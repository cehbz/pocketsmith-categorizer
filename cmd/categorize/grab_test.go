package main

import (
	"context"
	"net/http"
	"net/http/httptest"
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

// fakeMailbox is a mailbox that records the query and window it was asked for.
type fakeMailbox struct {
	messages []gmail.Message
	err      error
	query    string
	from, to time.Time
}

func (m *fakeMailbox) Messages(_ context.Context, query string, from, to time.Time) ([]gmail.Message, error) {
	m.query, m.from, m.to = query, from, to
	return m.messages, m.err
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
	box := &fakeMailbox{messages: []gmail.Message{
		{ID: "m1", Received: at("2026-11-01T05:00:00Z"), To: "a@example.com", Text: rideReceipt},
		{ID: "m2", Received: at("2026-10-30T12:00:00Z"), To: "a@example.com", Text: foodReceipt},
		{ID: "m3", Received: at("2026-10-29T08:00:00Z"), To: "a@example.com", Text: noTotal},
	}}
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
	if box.query != receiptQuery || !box.from.Equal(from) || !box.to.Equal(s.today) {
		t.Errorf("mailbox asked %q %v..%v; want %q %v..%v", box.query, box.from, box.to, receiptQuery, from, s.today)
	}
	for _, c := range []struct {
		tx   ledger.Transaction
		want decide.Decision
	}{
		{ride, decide.Decision{Category: 21, Label: "grab-auto", Source: "grab A-TESTRIDE0001"}},
		{food, decide.Decision{Category: 22, Label: "grab-auto", Source: "grab A-TESTFOOD0002"}},
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
	want := []report.Unreadable{{ID: "m3", Received: at("2026-10-29T08:00:00Z"), Why: "no total"}}
	if !reflect.DeepEqual(receipts.Unreadable, want) || len(receipts.Unmatched) != 0 {
		t.Errorf("receipts = %+v; want %+v unreadable and none unmatched", receipts, want)
	}
}

func TestAReceiptNoChargeMatchesIsReported(t *testing.T) {
	box := &fakeMailbox{messages: []gmail.Message{{ID: "m1", Received: at("2026-11-01T05:00:00Z"), To: "a@example.com", Text: rideReceipt}}}
	s := &session{today: mustParse("2026-11-03"), receipts: box, rates: rates(t, "35"), grabPayees: []string{"GRAB"}, grabPolicy: grabPolicy, loc: time.UTC}
	_, receipts, err := readGrab(context.Background(), s, nil, mustParse("2026-10-01"))
	if err != nil {
		t.Fatal(err)
	}
	if len(receipts.Unmatched) != 1 || receipts.Unmatched[0].Booking != "A-TESTRIDE0001" {
		t.Errorf("unmatched = %+v; want the ride", receipts.Unmatched)
	}
}

func TestReceiptsAreReportedInTheLedgersTimeZone(t *testing.T) {
	bangkok, err := time.LoadLocation("Asia/Bangkok")
	if err != nil {
		t.Fatal(err)
	}
	box := &fakeMailbox{messages: []gmail.Message{
		{ID: "m1", Received: at("2026-11-01T20:00:00Z"), To: "a@example.com", Text: rideReceipt},
		{ID: "m2", Received: at("2026-11-01T20:00:00Z"), To: "a@example.com", Text: noTotal},
	}}
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
