package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/categorize"
	"github.com/cehbz/pocketsmith-categorizer/decide"
	"github.com/cehbz/pocketsmith-categorizer/fx"
	"github.com/cehbz/pocketsmith-categorizer/gmail"
	"github.com/cehbz/pocketsmith-categorizer/internal/day"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
	"github.com/cehbz/pocketsmith-categorizer/report"
)

// fakeMailbox is a mailbox holding the messages each query finds. It records each search
// and the ids it read in full, and fails every search with err when it isn't nil.
type fakeMailbox struct {
	messages map[string][]gmail.Message // by query
	err      error
	searches []search
	read     []string
}

// search is one query over a window of days.
type search struct {
	query    string
	from, to time.Time
}

func (m *fakeMailbox) Messages(_ context.Context, query string, from, to time.Time, skip func(id string) bool) ([]gmail.Message, error) {
	m.searches = append(m.searches, search{query, from, to})
	if m.err != nil {
		return nil, m.err
	}
	var out []gmail.Message
	for _, msg := range m.messages[query] {
		if skip != nil && skip(msg.ID) {
			continue
		}
		m.read = append(m.read, msg.ID)
		out = append(out, msg)
	}
	return out, nil
}

// fakeRates is a rate source at a fixed THB per USD that counts its fetches.
type fakeRates struct {
	src     fx.Frankfurter
	fetches int
}

// rates is a rate source at thb per USD on every day from 2026-01-01.
func rates(t *testing.T, thb string) *fakeRates {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"amount":1.0,"base":"USD","rates":{"2026-01-01":{"THB":` + thb + `}}}`))
	}))
	t.Cleanup(srv.Close)
	return &fakeRates{src: fx.Frankfurter{BaseURL: srv.URL, HTTP: srv.Client()}}
}

func (r *fakeRates) Fetch(ctx context.Context, first, last time.Time) (fx.Table, error) {
	r.fetches++
	return r.src.Fetch(ctx, first, last)
}

var grabPolicy = categorize.Policy{Ride: 21, Delivery: 22, Label: "grab-auto"}

const (
	rideReceipt = `Grab E-Receipt | Standard | | Hope you enjoyed your ride! Booking ID: A-TESTRIDE0001 | | Total Paid | ฿70 | Grab Thailand`
	foodReceipt = `| ทานอาหารให้อร่อย! | | รวม ฿ 350 | ใบเสร็จรับเงิน | GrabFood | รหัสการจอง A-TESTFOOD0002 | รวม | ฿ 350`
	noTotal     = `Standard Hope you enjoyed your ride! Booking ID: A-TESTRIDE0003`
)

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestTheGrabLayerDecidesTheChargesReceiptsMatchByTheirService(t *testing.T) {
	box := &fakeMailbox{messages: map[string][]gmail.Message{receiptQuery: {
		{ID: "m1", Received: at("2026-11-01T05:00:00Z"), To: "a@example.com", Text: rideReceipt},
		{ID: "m2", Received: at("2026-10-30T12:00:00Z"), To: "a@example.com", Text: foodReceipt},
		{ID: "m3", Received: at("2026-10-29T08:00:00Z"), To: "a@example.com", Text: noTotal},
	}}}
	src := rates(t, "35")
	s := &session{today: mustParse("2026-11-03"), receipts: box, rates: src, grabPayees: []string{"GRAB"}, grabPolicy: grabPolicy, loc: time.UTC}
	card := ledger.Account{Name: "Card"}
	ride := ledger.Transaction{ID: 1, Date: mustParse("2026-11-02"), Amount: -2, Payee: "GRAB", Account: card, State: ledger.State{NeedsReview: true}}
	food := ledger.Transaction{ID: 2, Date: mustParse("2026-10-31"), Amount: -10, Payee: "GRAB", Account: card, State: ledger.State{NeedsReview: true}}
	other := ledger.Transaction{ID: 3, Date: mustParse("2026-11-01"), Amount: -5, Payee: "GRAB", Account: card, State: ledger.State{NeedsReview: true}}
	cafe := ledger.Transaction{ID: 4, Date: mustParse("2026-11-02"), Amount: -2, Payee: "CAFE", Account: card, State: ledger.State{NeedsReview: true}}
	from := mustParse("2026-10-01")
	layer, receipts, err := readGrab(context.Background(), s, []ledger.Transaction{cafe, ride, food, other}, from)
	if err != nil {
		t.Fatal(err)
	}
	if want := []search{{receiptQuery, from, s.today}}; !reflect.DeepEqual(box.searches, want) {
		t.Errorf("mailbox searches %v; want %v", box.searches, want)
	}
	for _, c := range []struct {
		tx   ledger.Transaction
		want decide.Decision
	}{
		{ride, decide.Decision{Category: 21, Label: "grab-auto", Source: "grab A-TESTRIDE0001", Reason: categorize.Reason{Why: categorize.ByReceipt, Merchant: "grab", Receipt: "A-TESTRIDE0001"}}},
		{food, decide.Decision{Category: 22, Label: "grab-auto", Source: "grab A-TESTFOOD0002", Reason: categorize.Reason{Why: categorize.ByReceipt, Merchant: "grab", Receipt: "A-TESTFOOD0002"}}},
	} {
		if got, ok := layer.Decide(c.tx); !ok || got != c.want {
			t.Errorf("Decide(%d) = %+v, %t; want %+v", c.tx.ID, got, ok, c.want)
		}
	}
	for _, tx := range []ledger.Transaction{other, cafe} {
		if got, ok := layer.Decide(tx); ok {
			t.Errorf("Decide(%d) = %+v; want an abstention", tx.ID, got)
		}
	}
	want := []report.Unreadable{{ID: "m3", Merchant: "grab", Received: at("2026-10-29T08:00:00Z"), Why: "no total"}}
	if !reflect.DeepEqual(receipts.Unreadable, want) || len(receipts.Unmatched) != 0 {
		t.Errorf("receipts = %+v; want %+v unreadable and none unmatched", receipts, want)
	}
}

func TestAReceiptNoChargeMatchesIsReported(t *testing.T) {
	box := &fakeMailbox{messages: map[string][]gmail.Message{receiptQuery: {{ID: "m1", Received: at("2026-11-01T05:00:00Z"), To: "a@example.com", Text: rideReceipt}}}}
	s := &session{today: mustParse("2026-11-03"), receipts: box, rates: rates(t, "35"), grabPayees: []string{"GRAB"}, grabPolicy: grabPolicy, loc: time.UTC}
	_, receipts, err := readGrab(context.Background(), s, nil, mustParse("2026-10-01"))
	if err != nil {
		t.Fatal(err)
	}
	if len(receipts.Unmatched) != 1 || receipts.Unmatched[0].Ref() != "A-TESTRIDE0001" || receipts.Unmatched[0].Merchant != "grab" {
		t.Errorf("unmatched = %+v; want the ride", receipts.Unmatched)
	}
}

func TestReceiptsAreReportedInTheLedgersTimeZone(t *testing.T) {
	bangkok, err := time.LoadLocation("Asia/Bangkok")
	if err != nil {
		t.Fatal(err)
	}
	box := &fakeMailbox{messages: map[string][]gmail.Message{receiptQuery: {
		{ID: "m1", Received: at("2026-11-01T20:00:00Z"), To: "a@example.com", Text: rideReceipt},
		{ID: "m2", Received: at("2026-11-01T20:00:00Z"), To: "a@example.com", Text: noTotal},
	}}}
	s := &session{today: mustParse("2026-11-03"), receipts: box, rates: rates(t, "35"), grabPayees: []string{"GRAB"}, grabPolicy: grabPolicy, loc: bangkok}
	_, receipts, err := readGrab(context.Background(), s, nil, mustParse("2026-10-01"))
	if err != nil {
		t.Fatal(err)
	}
	if len(receipts.Unmatched) != 1 || receipts.Unmatched[0].At.Location() != bangkok || receipts.Unmatched[0].At.Format("2006-01-02 15:04") != "2026-11-02 03:00" {
		t.Errorf("unmatched = %+v; want the ride paid at 2026-11-02 03:00 Bangkok time", receipts.Unmatched)
	}
	if len(receipts.Unreadable) != 1 || day.Format(receipts.Unreadable[0].Received) != "2026-11-02" {
		t.Errorf("unreadable = %+v; want the receipt without a total, received 2026-11-02", receipts.Unreadable)
	}
}

func TestWithoutReceiptsTheRatesArentFetched(t *testing.T) {
	src := rates(t, "35")
	s := &session{today: mustParse("2026-11-03"), receipts: &fakeMailbox{}, rates: src, grabPayees: []string{"GRAB"}, grabPolicy: grabPolicy, loc: time.UTC}
	tx := ledger.Transaction{ID: 1, Date: mustParse("2026-11-02"), Amount: -2, Payee: "GRAB"}
	layer, _, err := readGrab(context.Background(), s, []ledger.Transaction{tx}, mustParse("2026-10-01"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := layer.Decide(tx); ok || src.fetches != 0 {
		t.Errorf("decided %t after %d fetches; want an abstention and no fetch", ok, src.fetches)
	}
}

func TestReceiptsMatchedOnKeptRatesSaySo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rates.json")
	if err := os.WriteFile(path, []byte(`{"2026-10-01": 35}`), 0o600); err != nil {
		t.Fatal(err)
	}
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "origin down", http.StatusBadGateway)
	}))
	defer down.Close()
	box := &fakeMailbox{messages: map[string][]gmail.Message{receiptQuery: {{ID: "m1", Received: at("2026-11-01T05:00:00Z"), To: "a@example.com", Text: rideReceipt}}}}
	s := &session{today: mustParse("2026-11-03"), receipts: box, rates: fx.Cache{Path: path, Source: fx.Frankfurter{BaseURL: down.URL, HTTP: down.Client()}}, grabPayees: []string{"GRAB"}, grabPolicy: grabPolicy, loc: time.UTC}
	tx := ledger.Transaction{ID: 1, Date: mustParse("2026-11-02"), Amount: -2, Payee: "GRAB"}
	layer, receipts, err := readGrab(context.Background(), s, []ledger.Transaction{tx}, mustParse("2026-10-01"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := layer.Decide(tx); !ok || receipts.StaleRates == nil {
		t.Errorf("decided %t, stale rates %v; want the charge decided on the kept rate, and the receipts saying so", ok, receipts.StaleRates)
	}
}
