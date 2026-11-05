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
	"github.com/cehbz/pocketsmith-categorizer/evidence"
	"github.com/cehbz/pocketsmith-categorizer/fx"
	"github.com/cehbz/pocketsmith-categorizer/grab"
	"github.com/cehbz/pocketsmith-categorizer/internal/day"
	"github.com/cehbz/pocketsmith-categorizer/lazada"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
	"github.com/cehbz/pocketsmith-categorizer/report"
)

// fakeRates is a rate source at a fixed THB per USD that counts its fetches.
type fakeRates struct {
	src     fx.Frankfurter
	fetches int
	first   time.Time // the first day of the latest fetch
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
	r.first = first
	return r.src.Fetch(ctx, first, last)
}

// testGrab is Grab paid as GRAB, its receipts from 2026-10-26, rides in 21 and deliveries in 22.
func testGrab() evidence.Merchant {
	m := grab.Merchant()
	m.Payees, m.Start = []string{"GRAB"}, mustParse("2026-10-26")
	m.Categories = map[evidence.Kind]ledger.CategoryID{evidence.Ride: 21, evidence.Food: 22, evidence.Mart: 22, evidence.Express: 22}
	return m
}

// testLazada is Lazada paid as LAZADA PAY, its receipts from 2024-12-10, orders in 40 and
// bills in 41.
func testLazada() evidence.Merchant {
	m := lazada.Merchant()
	m.Payees, m.Start = []string{"LAZADA PAY"}, mustParse("2024-12-10")
	m.Categories = map[evidence.Kind]ledger.CategoryID{evidence.Order: 40, evidence.Electricity: 41, evidence.Water: 41, evidence.Bill: 41}
	return m
}

// receiptSession is a session on 2026-11-03 in UTC explaining Grab's and Lazada's charges at
// 35 THB per USD.
func receiptSession(t *testing.T) *session {
	return &session{today: mustParse("2026-11-03"), rates: rates(t, "35"), merchants: []evidence.Merchant{testGrab(), testLazada()}, loc: time.UTC}
}

func receipt(merchant, when, ref string, kind evidence.Kind, thb float64) evidence.Receipt {
	return evidence.Receipt{Merchant: merchant, At: at(when), Parts: []evidence.Part{{Ref: ref, Kind: kind, THB: thb}}}
}

// txn is a card transaction needing review.
func txn(id int64, date string, amount float64, payee string) ledger.Transaction {
	return ledger.Transaction{ID: id, Date: mustParse(date), Amount: amount, Payee: payee, Account: ledger.Account{Name: "Card"}, State: ledger.State{NeedsReview: true}}
}

func explainAll(t *testing.T, s *session, ev evidence.Evidence, txs ...ledger.Transaction) explained {
	t.Helper()
	ex, err := explain(context.Background(), s, ev, nil, txs, mustParse("2026-10-01"))
	if err != nil {
		t.Fatal(err)
	}
	return ex
}

func TestGrabChargesTakeTheirReceiptsKindsCategory(t *testing.T) {
	ev := evidence.Evidence{Receipts: []evidence.Receipt{
		receipt("grab", "2026-11-01T05:00:00Z", "A-TESTRIDE0001", evidence.Ride, 70),
		receipt("grab", "2026-10-30T12:00:00Z", "A-TESTFOOD0002", evidence.Food, 350),
	}}
	ride, food := txn(1, "2026-11-02", -2, "GRAB"), txn(2, "2026-10-31", -10, "GRAB")
	other, cafe := txn(3, "2026-11-01", -5, "GRAB"), txn(4, "2026-11-02", -2, "CAFE")
	ex := explainAll(t, receiptSession(t), ev, cafe, ride, food, other)
	for _, c := range []struct {
		tx   ledger.Transaction
		want decide.Decision
	}{
		{ride, decide.Decision{Category: 21, Label: "grab-auto", Source: "grab A-TESTRIDE0001", Reason: categorize.Reason{Why: categorize.ByReceipt, Merchant: "grab", Receipt: "A-TESTRIDE0001"}}},
		{food, decide.Decision{Category: 22, Label: "grab-auto", Source: "grab A-TESTFOOD0002", Reason: categorize.Reason{Why: categorize.ByReceipt, Merchant: "grab", Receipt: "A-TESTFOOD0002"}}},
	} {
		if got, ok := ex.layer.Decide(c.tx); !ok || got != c.want {
			t.Errorf("Decide(%d) = %+v, %t; want %+v", c.tx.ID, got, ok, c.want)
		}
	}
	for _, tx := range []ledger.Transaction{other, cafe} {
		if got, ok := ex.layer.Decide(tx); ok {
			t.Errorf("Decide(%d) = %+v; want an abstention", tx.ID, got)
		}
	}
	if want := map[int64][]string{3: {"attention-no-grab-receipt"}}; !reflect.DeepEqual(ex.reasons, want) {
		t.Errorf("reasons %v; want %v", ex.reasons, want)
	}
}

func TestALazadaCheckoutTakesTheOrderCategoryAndABillTheBillsCategory(t *testing.T) {
	a := receipt("lazada", "2026-10-20T07:05:00Z", "900000000000001", evidence.Order, 340)
	b := receipt("lazada", "2026-10-20T07:05:00Z", "900000000000002", evidence.Order, 120)
	a.Checkout, b.Checkout = "2026-10-20T07:05:00Z", "2026-10-20T07:05:00Z"
	bill := receipt("lazada", "2026-10-22T03:00:00Z", "EZBI0000000001", evidence.Water, 150)
	bill.Fee = lazada.Fee
	// 460 THB is 13.18 USD at 35 with the card's markup; 155.35 THB is 4.45 USD.
	order, water := txn(1, "2026-10-20", -13.18, "LAZADA PAY"), txn(2, "2026-10-22", -4.45, "LAZADA PAY")
	ex := explainAll(t, receiptSession(t), evidence.Evidence{Receipts: []evidence.Receipt{a, b, bill}}, order, water)
	for _, c := range []struct {
		tx   ledger.Transaction
		want decide.Decision
	}{
		{order, decide.Decision{Category: 40, Label: "auto-lazada", Source: "lazada 900000000000001+900000000000002",
			Reason: categorize.Reason{Why: categorize.ByReceipt, Merchant: "lazada", Receipt: "900000000000001+900000000000002"}}},
		{water, decide.Decision{Category: 41, Label: "auto-lazada", Source: "lazada EZBI0000000001",
			Reason: categorize.Reason{Why: categorize.ByReceipt, Merchant: "lazada", Receipt: "EZBI0000000001"}}},
	} {
		if got, ok := ex.layer.Decide(c.tx); !ok || got != c.want {
			t.Errorf("Decide(%d) = %+v, %t; want %+v", c.tx.ID, got, ok, c.want)
		}
	}
}

func TestATaxInvoiceMakesAnOtherwiseUnexplainedChargeABill(t *testing.T) {
	invoice := evidence.Receipt{Merchant: "lazada", At: at("2026-10-22T03:00:00Z"), Unpriced: true, Parts: []evidence.Part{{Ref: "tax invoice", Kind: evidence.Bill}}}
	bill := txn(1, "2026-10-23", -20, "LAZADA PAY")
	ex := explainAll(t, receiptSession(t), evidence.Evidence{Receipts: []evidence.Receipt{invoice}}, bill)
	if got, ok := ex.layer.Decide(bill); !ok || got.Category != 41 || len(ex.reasons) != 0 {
		t.Errorf("Decide = %+v, %t, reasons %v; want the bills' category and no reason", got, ok, ex.reasons)
	}
}

func TestACreditTakesTheCategoryOfTheChargeThatPaidItsPartAsItsRefund(t *testing.T) {
	ev := evidence.Evidence{
		Receipts:      []evidence.Receipt{receipt("lazada", "2026-10-20T07:05:00Z", "900000000000001", evidence.Order, 350)},
		Cancellations: []evidence.Cancellation{{Merchant: "lazada", Ref: "900000000000001", At: at("2026-10-25T07:00:00Z")}},
	}
	charge, refund := txn(1, "2026-10-20", -10.03, "LAZADA PAY"), txn(2, "2026-10-24", 10.03, "LAZADA PAY")
	for _, c := range []struct {
		name     string
		state    ledger.State
		category ledger.CategoryID
	}{
		{"charge in review", ledger.State{NeedsReview: true}, 40},
		{"charge the user categorized", ledger.State{Category: 55}, 55},
	} {
		charge.State = c.state
		ex := explainAll(t, receiptSession(t), ev, charge, refund)
		want := decide.Decision{Category: c.category, Label: "auto-lazada", Source: "lazada refund of 1",
			Reason: categorize.Reason{Why: categorize.Refund, Merchant: "lazada", Receipt: "900000000000001"}}
		if got, ok := ex.layer.Decide(refund); !ok || got != want || ex.paid[2] != 1 {
			t.Errorf("%s: Decide = %+v, %t, paid %v; want %+v, paid by charge 1", c.name, got, ok, ex.paid, want)
		}
	}
}

func TestAChargeOrCreditTheEvidenceCantExplainNeedsTheUser(t *testing.T) {
	s := receiptSession(t)
	s.merchants[1].Start = mustParse("2026-10-15")
	early := txn(1, "2026-10-10", -9, "LAZADA PAY") // before Lazada's receipts start: left to the rules
	charge, refund := txn(2, "2026-10-20", -9, "LAZADA PAY"), txn(3, "2026-10-21", 9, "LAZADA PAY")
	old := txn(4, "2026-09-20", -9, "LAZADA PAY") // before the lookback
	ex := explainAll(t, s, evidence.Evidence{}, early, charge, refund, old)
	if want := map[int64][]string{2: {"attention-no-lazada-receipt"}, 3: {"attention-no-lazada-purchase"}}; !reflect.DeepEqual(ex.reasons, want) {
		t.Errorf("reasons %v; want %v", ex.reasons, want)
	}
}

func TestReceiptsTheRunCouldntUseAreReportedInTheLedgersTimeZone(t *testing.T) {
	bangkok, err := time.LoadLocation("Asia/Bangkok")
	if err != nil {
		t.Fatal(err)
	}
	s := receiptSession(t)
	s.loc = bangkok
	invoice := evidence.Receipt{Merchant: "lazada", At: at("2026-10-22T03:00:00Z"), Unpriced: true, Parts: []evidence.Part{{Ref: "tax invoice", Kind: evidence.Bill}}}
	ev := evidence.Evidence{Receipts: []evidence.Receipt{receipt("grab", "2026-11-01T20:00:00Z", "A-TESTRIDE0001", evidence.Ride, 70), invoice}}
	unreadable := []evidence.Read{{ID: "m2", Merchant: "grab", Received: at("2026-11-01T20:00:00Z"), Unreadable: "no total"}}
	ex, err := explain(context.Background(), s, ev, unreadable, nil, mustParse("2026-10-01"))
	if err != nil {
		t.Fatal(err)
	}
	if u := ex.receipts.Unmatched; len(u) != 1 || u[0].Ref() != "A-TESTRIDE0001" || u[0].At.Location() != bangkok || u[0].At.Format("2006-01-02 15:04") != "2026-11-02 03:00" {
		t.Errorf("unmatched = %+v; want the ride alone, paid at 2026-11-02 03:00 Bangkok time", u)
	}
	want := []report.Unreadable{{ID: "m2", Merchant: "grab", Received: at("2026-11-01T20:00:00Z").In(bangkok), Why: "no total"}}
	if !reflect.DeepEqual(ex.receipts.Unreadable, want) || day.Format(ex.receipts.Unreadable[0].Received) != "2026-11-02" {
		t.Errorf("unreadable = %+v; want %+v", ex.receipts.Unreadable, want)
	}
}

func TestWithoutReceiptsNoRateIsFetched(t *testing.T) {
	s := receiptSession(t)
	src := s.rates.(*fakeRates)
	tx := txn(1, "2026-11-02", -2, "GRAB")
	ex := explainAll(t, s, evidence.Evidence{}, tx)
	if _, ok := ex.layer.Decide(tx); ok || src.fetches != 0 {
		t.Errorf("decided %t after %d fetches; want an abstention and no fetch", ok, src.fetches)
	}
}

func TestChargesExplainedOnKeptRatesSaySo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rates.json")
	if err := os.WriteFile(path, []byte(`{"2026-10-01": 35}`), 0o600); err != nil {
		t.Fatal(err)
	}
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "origin down", http.StatusBadGateway)
	}))
	defer down.Close()
	s := receiptSession(t)
	s.rates = fx.Cache{Path: path, Source: fx.Frankfurter{BaseURL: down.URL, HTTP: down.Client()}}
	tx := txn(1, "2026-11-02", -2, "GRAB")
	ex := explainAll(t, s, evidence.Evidence{Receipts: []evidence.Receipt{receipt("grab", "2026-11-01T05:00:00Z", "A-TESTRIDE0001", evidence.Ride, 70)}}, tx)
	if _, ok := ex.layer.Decide(tx); !ok || ex.receipts.StaleRates == nil {
		t.Errorf("decided %t, stale rates %v; want the charge decided on the kept rate, and the receipts saying so", ok, ex.receipts.StaleRates)
	}
}

func TestRatesCoverACreditDatedBeforeTheEarliestReceipt(t *testing.T) {
	s := receiptSession(t)
	src := s.rates.(*fakeRates)
	ev := evidence.Evidence{Receipts: []evidence.Receipt{receipt("lazada", "2026-10-25T07:05:00Z", "900000000000001", evidence.Order, 350)}}
	explainAll(t, s, ev, txn(1, "2026-10-05", 10.03, "LAZADA PAY"))
	if want := mustParse("2026-10-05"); src.first.After(want) {
		t.Errorf("rates fetched from %s; want on or before the credit's day %s", src.first.Format("2006-01-02"), want.Format("2006-01-02"))
	}
}

func TestARefundOfAPartWhoseChargeIsOutsideTheLookbackNamesThePart(t *testing.T) {
	ev := evidence.Evidence{Receipts: []evidence.Receipt{receipt("lazada", "2026-09-20T07:05:00Z", "900000000000001", evidence.Order, 350)}}
	old, refund := txn(1, "2026-09-20", -10.03, "LAZADA PAY"), txn(2, "2026-10-02", 10.03, "LAZADA PAY")
	ex := explainAll(t, receiptSession(t), ev, old, refund)
	want := decide.Decision{Category: 40, Label: "auto-lazada", Source: "lazada refund of 900000000000001",
		Reason: categorize.Reason{Why: categorize.Refund, Merchant: "lazada", Receipt: "900000000000001"}}
	if got, ok := ex.layer.Decide(refund); !ok || got != want || len(ex.paid) != 0 {
		t.Errorf("Decide = %+v, %t, paid %v; want %+v and no paid entry", got, ok, ex.paid, want)
	}
}
