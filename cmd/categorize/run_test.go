package main

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/attention"
	"github.com/cehbz/pocketsmith-categorizer/categorize"
	"github.com/cehbz/pocketsmith-categorizer/decide"
	"github.com/cehbz/pocketsmith-categorizer/evidence"
	"github.com/cehbz/pocketsmith-categorizer/gmail"
	"github.com/cehbz/pocketsmith-categorizer/grab"
	"github.com/cehbz/pocketsmith-categorizer/journal"
	"github.com/cehbz/pocketsmith-categorizer/lazada"
	"github.com/cehbz/pocketsmith-categorizer/learn"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
	"github.com/cehbz/pocketsmith-categorizer/memo"
	"github.com/cehbz/pocketsmith-categorizer/rules"
	"github.com/cehbz/pocketsmith-categorizer/trips"
)

func TestFlagsKeepTheRulesSource(t *testing.T) {
	owned := ledger.State{Category: 5, Labels: []string{"auto-rule"}}
	user := ledger.State{Category: 6}
	flags := []categorize.Change{
		{Charge: 1, Before: owned, After: ledger.State{Category: 5, NeedsReview: true, Labels: []string{"auto-rule", attention.Large}}},
		{Charge: 2, Before: user, After: ledger.State{Category: 6, NeedsReview: true, Labels: []string{attention.Large}}},
		{Charge: 3, Before: owned, After: ledger.State{Category: 5, NeedsReview: true, Labels: []string{"auto-rule", attention.Large}}},
	}
	last := map[int64]categorize.Change{
		1: {Charge: 1, After: owned, Source: "rule coffee"},
		2: {Charge: 2, After: owned, Source: "rule other"},
	}
	inheritSources(flags, last)
	if flags[0].Source != "rule coffee" || flags[1].Source != "" || flags[2].Source != "" {
		t.Errorf("sources = %q, %q, %q", flags[0].Source, flags[1].Source, flags[2].Source)
	}
}

// fakeLedger is a ledger in memory that counts its updates and records whom they updated.
type fakeLedger struct {
	txs     map[int64]ledger.Transaction
	updates int
	updated []int64
}

func (l *fakeLedger) Transactions(_ context.Context, first, last time.Time, needsReview bool) ([]ledger.Transaction, error) {
	var out []ledger.Transaction
	for _, id := range slices.Sorted(maps.Keys(l.txs)) {
		t := l.txs[id]
		if !t.Date.Before(first) && !t.Date.After(last) && (!needsReview || t.State.NeedsReview) {
			out = append(out, t)
		}
	}
	return out, nil
}

func (l *fakeLedger) Charge(_ context.Context, id int64) (ledger.Charge, error) {
	t := l.txs[id]
	return ledger.Charge{ID: id, USD: -t.Amount, Date: t.Date, Payee: t.Payee, State: t.State}, nil
}

func (l *fakeLedger) Update(_ context.Context, id int64, s ledger.State) error {
	t := l.txs[id]
	t.State = s
	l.txs[id] = t
	l.updates++
	l.updated = append(l.updated, id)
	return nil
}

// countingTrips is a trip store that counts its saves.
type countingTrips struct {
	fakeTrips
	saves int
}

func (c *countingTrips) Save(list trips.List, written []trips.Trip) error {
	c.saves++
	return c.fakeTrips.Save(list, written)
}

// tripRun is a session over a card with a three-day trip from 2026-10-05: two in-store hotel
// nights and a new restaurant, a hotel refund after it, a flight booked before it and a
// grocer at home around it. Two new cafés on consecutive days at the start of the history
// are inside the inference warm-up, so they make no trip.
func tripRun(t *testing.T) (*session, *fakeLedger) {
	t.Helper()
	card := ledger.Account{Name: "Card"}
	tx := func(id int64, d string, amount float64, payee string) ledger.Transaction {
		return ledger.Transaction{ID: id, Date: mustParse(d), Amount: amount, Payee: payee, Account: card, State: ledger.State{Category: 5}}
	}
	l := &fakeLedger{txs: map[int64]ledger.Transaction{}}
	for _, x := range []ledger.Transaction{
		tx(1, "2026-08-01", -5, "GROCER"), tx(2, "2026-09-01", -5.5, "GROCER"), tx(3, "2026-10-20", -6, "GROCER"),
		tx(4, "2026-10-05", -120, "HOTEL FARCITY"), tx(5, "2026-10-06", -12, "NOODLES FARCITY"), tx(6, "2026-10-07", -125, "HOTEL FARCITY"),
		tx(7, "2026-10-15", 125, "HOTEL FARCITY"), tx(8, "2026-09-01", -400, "AIRLINE"),
		tx(9, "2025-12-01", -10, "CAFE ALPHA FARCITY"), tx(10, "2025-12-02", -10, "CAFE BETA FARCITY"),
	} {
		l.txs[x.ID] = x
	}
	rec := func(d string, amount float64, name, merchant, code string, inStore bool) trips.Record {
		return trips.Record{Account: "Card", Date: mustParse(d), Amount: amount, Name: name, Merchant: merchant, Code: code, InStore: inStore}
	}
	c := &fakeCard{recs: []trips.Record{
		rec("2026-10-05", 120, "HOTEL FARCITY", "Hotel Farcity", "7011", true), rec("2026-10-06", 12, "NOODLES FARCITY", "Noodles", "5812", true),
		rec("2026-10-07", 125, "HOTEL FARCITY", "Hotel Farcity", "7011", true), rec("2026-10-15", -125, "HOTEL FARCITY", "Hotel Farcity", "7011", false),
		rec("2026-09-01", 400, "AIRLINE", "Airline", "4511", false),
		rec("2025-12-01", 10, "CAFE ALPHA FARCITY", "Cafe Alpha", "5812", true), rec("2025-12-02", 10, "CAFE BETA FARCITY", "Cafe Beta", "5812", true),
	}}
	start := mustParse("2026-10-01")
	big := map[trips.Kind]ledger.CategoryID{trips.Transport: 101, trips.Lodging: 102, trips.Food: 103, trips.Discretionary: 104, trips.Cash: 105}
	s := &session{ledger: l, titles: map[ledger.CategoryID]string{}, today: mustParse("2026-11-03"), card: c, accounts: []string{"Card"}, calendar: &fakeCalendar{},
		receipts: &fakeMailbox{}, merchants: []evidence.Merchant{testGrab()}, loc: time.UTC,
		attention: attention.Settings{Start: start, Size: attention.Size{Absolute: 1e9, SD: 1e3}},
		trips:     trips.Settings{Kinds: trips.Kinds{Codes: trips.DefaultCodes(), Big: big}, Home: trips.Home{Places: []string{"HOMETOWN"}}, Start: start}}
	return s, l
}

// runners makes runners sharing a journal, memos and a trip store in a temp dir.
func runners(t *testing.T) (func() runner, *countingTrips) {
	t.Helper()
	dir := t.TempDir()
	store := &countingTrips{}
	return func() runner {
		r := runner{journal: journal.Store{Dir: filepath.Join(dir, "journal")}, trips: store, lookback: 365, history: 365}
		var err error
		if r.evidence, err = evidence.Load(filepath.Join(dir, "evidence.json")); err != nil {
			t.Fatal(err)
		}
		for _, m := range []struct {
			dst  **memo.Memo
			name string
		}{{&r.shown, "shown.json"}, {&r.judged, "judged.json"}, {&r.placed, "placed.json"}, {&r.payees, "payees.json"}} {
			var err error
			if *m.dst, err = memo.Load(filepath.Join(dir, m.name)); err != nil {
				t.Fatal(err)
			}
		}
		return r
	}, store
}

func TestARunRemembersThePayeesItRead(t *testing.T) {
	s, _ := tripRun(t)
	newRunner, _ := runners(t)
	r := newRunner()
	var out strings.Builder
	if err := r.run(context.Background(), s, &out); err != nil {
		t.Fatal(err)
	}
	if !r.payees.Seen("HOTEL FARCITY") || !r.payees.Seen("GROCER") {
		t.Errorf("payees = %v, want the run's payees", r.payees.Keys())
	}
}

func TestASecondRunOverTheFirstRunsOutputAppliesNothing(t *testing.T) {
	newRunner, store := runners(t)
	s, l := tripRun(t)
	var out strings.Builder
	if err := newRunner().run(context.Background(), s, &out); err != nil {
		t.Fatal(err)
	}
	trip := trips.Trip{Name: "trip-2026-10-05", From: mustParse("2026-10-05"), To: mustParse("2026-10-07")}
	if !reflect.DeepEqual(store.list, trips.List{trip}) || l.txs[4].State.Category != 102 || !l.txs[4].State.HasLabel(trip.Name) ||
		l.txs[7].State.Category != 102 || l.txs[8].State.Category != 101 || l.txs[2].State.Category != 5 {
		t.Fatalf("first run: list %v, hotel %+v, refund %+v, booking %+v, grocer %+v\n%s", store.list, l.txs[4].State, l.txs[7].State, l.txs[8].State, l.txs[2].State, out.String())
	}
	if got := out.String(); !strings.Contains(got, "\nTrips Added\n  trip-2026-10-05 ·") || strings.Contains(got, "Trips Renamed or Redated") ||
		!l.txs[4].State.NeedsReview || !l.txs[4].State.HasLabel(trips.Placed) {
		t.Errorf("first run: hotel %+v; want it flagged placed, one trip added and none with a new version\n%s", l.txs[4].State, got)
	}
	l.updates, store.saves = 0, 0
	out.Reset()
	if err := newRunner().run(context.Background(), s, &out); err != nil {
		t.Fatal(err)
	}
	if l.updates != 0 || store.saves != 0 {
		t.Errorf("second run: %d updates, %d trip list saves; want none\n%s", l.updates, store.saves, out.String())
	}
}

func TestARunExtendsATripInProgressAndTheNextAppliesNothing(t *testing.T) {
	newRunner, store := runners(t)
	s, l := tripRun(t)
	today := s.today
	s.today = mustParse("2026-10-06")
	var out strings.Builder
	if err := newRunner().run(context.Background(), s, &out); err != nil {
		t.Fatal(err)
	}
	early := trips.Trip{Name: "trip-2026-10-05", From: mustParse("2026-10-05"), To: mustParse("2026-10-06")}
	if !reflect.DeepEqual(store.list, trips.List{early}) {
		t.Fatalf("run on day 2: list %v, want %v\n%s", store.list, early, out.String())
	}
	s.today = today
	out.Reset()
	if err := newRunner().run(context.Background(), s, &out); err != nil {
		t.Fatal(err)
	}
	longer := early
	longer.To = mustParse("2026-10-07")
	if !reflect.DeepEqual(store.list, trips.List{longer}) || !reflect.DeepEqual(store.written, []trips.Trip{longer}) ||
		l.txs[6].State.Category != 102 || !l.txs[6].State.HasLabel(longer.Name) {
		t.Fatalf("later run: list %v, written %v, last night %+v; want the trip extended and its last night placed\n%s", store.list, store.written, l.txs[6].State, out.String())
	}
	if want := "\nTrips Renamed or Redated\n  trip-2026-10-05 · Oct 5 – Oct 7\n"; !strings.Contains(out.String(), want) {
		t.Errorf("report lacks %q:\n%s", want, out.String())
	}
	l.updates, store.saves = 0, 0
	out.Reset()
	if err := newRunner().run(context.Background(), s, &out); err != nil {
		t.Fatal(err)
	}
	if l.updates != 0 || store.saves != 0 {
		t.Errorf("next run: %d updates, %d trip list saves; want none\n%s", l.updates, store.saves, out.String())
	}
}

// first runs the first run of tripRun and returns its trip.
func first(t *testing.T, newRunner func() runner, s *session) trips.Trip {
	t.Helper()
	var out strings.Builder
	if err := newRunner().run(context.Background(), s, &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	return trips.Trip{Name: "trip-2026-10-05", From: mustParse("2026-10-05"), To: mustParse("2026-10-07")}
}

func TestAReviewedPlacementIsntFlaggedAgain(t *testing.T) {
	newRunner, _ := runners(t)
	s, l := tripRun(t)
	first(t, newRunner, s)
	for id, tx := range l.txs {
		tx.State.NeedsReview = false // the user reviewed everything
		l.txs[id] = tx
	}
	l.updates = 0
	var out strings.Builder
	if err := newRunner().run(context.Background(), s, &out); err != nil {
		t.Fatal(err)
	}
	if l.updates != 0 {
		t.Errorf("run after the review: %d updates, hotel %+v; want none: the placements were flagged once\n%s", l.updates, l.txs[4].State, out.String())
	}
}

func TestAChargeTheUserRecategorizedIsntPlacedAgain(t *testing.T) {
	newRunner, _ := runners(t)
	s, l := tripRun(t)
	first(t, newRunner, s)
	hotel := l.txs[4]
	hotel.State.Category = 9 // the user's correction, to a category placement would not choose
	l.txs[4] = hotel
	var out strings.Builder
	if err := newRunner().run(context.Background(), s, &out); err != nil {
		t.Fatal(err)
	}
	if got := l.txs[4].State; got.Category != 9 || got.HasLabel(trips.Label) ||
		!strings.Contains(out.String(), "0 of 0 changes applied") || !strings.Contains(out.String(), "\nCorrected Trip Categories\n") {
		t.Errorf("hotel after the correction: %+v; want the user's category, no longer the categorizer's, and no placement\n%s", got, out.String())
	}
}

func TestATripTheUserDeletedReturnsItsChargesToWhereTheyWere(t *testing.T) {
	newRunner, store := runners(t)
	s, l := tripRun(t)
	trip := first(t, newRunner, s)
	store.list = nil // deleted from the list; the record of trips written keeps it
	var out strings.Builder
	if err := newRunner().run(context.Background(), s, &out); err != nil {
		t.Fatal(err)
	}
	// The restaurant returns to where it was; the lodging, outside every trip, is a booking.
	if got := l.txs[5].State; got.Category != 5 || got.HasLabel(trips.Label) || got.HasLabel(trip.Name) {
		t.Errorf("restaurant after the trip was deleted: %+v; want category 5 without the trip's labels\n%s", got, out.String())
	}
	for _, id := range []int64{4, 6} {
		if got := l.txs[id].State; !got.HasLabel(trips.Booking) || got.HasLabel(trip.Name) {
			t.Errorf("transaction %d after the trip was deleted: %+v; want a booking without the trip's name\n%s", id, got, out.String())
		}
	}
	if len(store.list) != 0 {
		t.Errorf("list = %v; the deleted trip was written before and isn't listed again", store.list)
	}
}

func TestAListedTripChangesNothingInTheLedgerAndIsStillRecordedUnderARun(t *testing.T) {
	newRunner, store := runners(t)
	s, l := tripRun(t)
	trip := first(t, newRunner, s)
	store.list, store.written = nil, nil // the user wiped both files
	l.updates = 0
	r := newRunner()
	before, err := r.journal.Latest()
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := r.run(context.Background(), s, &out); err != nil {
		t.Fatal(err)
	}
	if latest, err := r.journal.Latest(); !reflect.DeepEqual(store.list, trips.List{trip}) || l.updates != 0 || err != nil || latest == before {
		t.Errorf("list %v, %d updates; want %v listed under a run of its own\n%s", store.list, l.updates, trip, out.String())
	}
}

func TestATripTheUserRenamedIsReportedAndItsChargesPlacedUnderTheNewName(t *testing.T) {
	newRunner, store := runners(t)
	s, l := tripRun(t)
	first(t, newRunner, s)
	store.list = trips.List{{Name: "trip-farcity", From: mustParse("2026-10-05"), To: mustParse("2026-10-07")}}
	var out strings.Builder
	if err := newRunner().run(context.Background(), s, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "\nTrips Renamed or Redated\n  Farcity trip ·") || !l.txs[4].State.HasLabel("trip-farcity") {
		t.Errorf("hotel %+v; want it under the new name and the report to list the trip\n%s", l.txs[4].State, out.String())
	}
}

func TestARunLeavesChargesDatedBeforeItsLookbackAsTheyAre(t *testing.T) {
	newRunner, _ := runners(t)
	s, l := tripRun(t)
	r := newRunner()
	r.lookback = 30
	var out strings.Builder
	if err := r.run(context.Background(), s, &out); err != nil {
		t.Fatal(err)
	}
	if l.txs[4].State.Category != 102 || l.txs[8].State.Category != 5 {
		t.Errorf("hotel %+v, flight booked before the lookback %+v; want the hotel placed and the flight left\n%s", l.txs[4].State, l.txs[8].State, out.String())
	}
}

func TestARunReportsTheTripsItAddsWithTheirEvidence(t *testing.T) {
	newRunner, _ := runners(t)
	s, _ := tripRun(t)
	var out strings.Builder
	if err := newRunner().run(context.Background(), s, &out); err != nil {
		t.Fatal(err)
	}
	want := "\nTrips Added\n  trip-2026-10-05 · Oct 5 – Oct 7\n" +
		"    card  lodging    HOTEL FARCITY    Oct 5 – Oct 7\n" +
		"    card  new place  HOTEL FARCITY    Oct 5\n" +
		"    card  new place  NOODLES FARCITY  Oct 6\n"
	if !strings.Contains(out.String(), want) {
		t.Errorf("report lacks %q:\n%s", want, out.String())
	}
}

func TestATripInferredBeyondOneTheUserEditedIsAHintEveryRunAndTheListStays(t *testing.T) {
	newRunner, store := runners(t)
	s, _ := tripRun(t)
	first(t, newRunner, s)
	edited := trips.Trip{Name: "trip-farcity", From: mustParse("2026-10-06"), To: mustParse("2026-10-07")}
	store.list = trips.List{edited}
	store.saves = 0
	var out strings.Builder
	if err := newRunner().run(context.Background(), s, &out); err != nil {
		t.Fatal(err)
	}
	want := "\nTrip Hints\n  Farcity trip · Oct 6 – Oct 7 · inferred Oct 5 – Oct 7\n"
	if !strings.Contains(out.String(), want) || !reflect.DeepEqual(store.list, trips.List{edited}) || store.saves != 0 {
		t.Errorf("list %v after %d saves; want the hint and the list as the user left it\n%s", store.list, store.saves, out.String())
	}
	out.Reset()
	if err := newRunner().run(context.Background(), s, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), want) || !reflect.DeepEqual(store.list, trips.List{edited}) || store.saves != 0 {
		t.Errorf("next run: list %v after %d saves; want the hint again and the list as the user left it\n%s", store.list, store.saves, out.String())
	}
}

// grabRun is tripRun with a Grab ride's charge needing review, a rule that also matches it,
// and the ride's receipt in the mailbox.
func grabRun(t *testing.T) (*session, *fakeLedger) {
	t.Helper()
	s, l := tripRun(t)
	l.txs[11] = ledger.Transaction{ID: 11, Date: mustParse("2026-11-02"), Amount: -2, Payee: "GRAB", Account: ledger.Account{Name: "Card"}, State: ledger.State{NeedsReview: true}}
	path := filepath.Join(t.TempDir(), "rules.toml")
	if err := os.WriteFile(path, []byte("[[rule]]\npayee = \"GRAB\"\ncategory = \"Travel\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rs, err := rules.Load(path, []ledger.Category{{ID: 30, Title: "Travel"}})
	if err != nil {
		t.Fatal(err)
	}
	s.rules = rs
	s.receipts = &fakeMailbox{messages: map[string][]gmail.Message{grab.Query: {ride("m1", "2026-11-01T05:00:00Z")}}}
	s.rates = rates(t, "35")
	return s, l
}

func TestARunPutsAGrabChargeAReceiptMatchesInItsServicesCategoryAheadOfTheRules(t *testing.T) {
	newRunner, _ := runners(t)
	s, l := grabRun(t)
	var out strings.Builder
	if err := newRunner().run(context.Background(), s, &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if got := l.txs[11].State; got.Category != 21 || !got.HasLabel("grab-auto") {
		t.Errorf("Grab charge %+v; want the ride category under grab-auto\n%s", got, out.String())
	}
}

func TestAMailboxFailureFailsTheRunBeforeAnythingIsApplied(t *testing.T) {
	newRunner, store := runners(t)
	s, l := grabRun(t)
	s.receipts = &fakeMailbox{err: gmail.ErrAuth}
	var out strings.Builder
	if err := newRunner().run(context.Background(), s, &out); !errors.Is(err, gmail.ErrAuth) || l.updates != 0 || store.saves != 0 {
		t.Errorf("run: %v after %d updates and %d trip list saves; want the mailbox's error and nothing applied\n%s", err, l.updates, store.saves, out.String())
	}
}

func TestARunPutsALazadaOrdersChargeInTheOrderCategoryAndAChargeWithoutAReceiptInReview(t *testing.T) {
	newRunner, _ := runners(t)
	s, l := lazadaRun(t)
	var out strings.Builder
	if err := newRunner().run(context.Background(), s, &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if got := l.txs[11].State; got.Category != 40 || !got.HasLabel("auto-lazada") {
		t.Errorf("order's charge %+v; want the order category under auto-lazada\n%s", got, out.String())
	}
	if got := l.txs[12].State; !got.NeedsReview || !got.HasLabel(evidence.NoReceipt("lazada")) {
		t.Errorf("charge without a receipt %+v; want it in review for want of a Lazada receipt\n%s", got, out.String())
	}
	if _, reasons, _ := strings.Cut(out.String(), "\nReview Reasons\n"); !strings.Contains(reasons, "no Lazada receipt") {
		t.Errorf("Review Reasons lack the missing receipt\n%s", out.String())
	}
}

// lazadaRun is tripRun explaining Grab's and Lazada's charges, with a Lazada order's
// confirmation in the mailbox, its charge, and a Lazada charge no receipt explains, both
// needing review.
func lazadaRun(t *testing.T) (*session, *fakeLedger) {
	t.Helper()
	s, l := tripRun(t)
	s.merchants = []evidence.Merchant{testGrab(), testLazada()}
	s.rates = rates(t, "35")
	s.receipts = &fakeMailbox{messages: map[string][]gmail.Message{lazada.Queries[0]: {{ID: "m1", Received: at("2026-11-01T07:06:00Z"),
		Subject: "We have received your order No 900000000000001",
		Text:    "Your Order #900000000000001 placed on November 1, 2026 at 14:05 via Credit or Debit Card has been confirmed. Total (VAT included) ฿460.00"}}}}
	card := ledger.Account{Name: "Card"}
	l.txs[11] = ledger.Transaction{ID: 11, Date: mustParse("2026-11-01"), Amount: -13.18, Payee: "LAZADA PAY", Account: card, State: ledger.State{NeedsReview: true}}
	l.txs[12] = ledger.Transaction{ID: 12, Date: mustParse("2026-11-02"), Amount: -9, Payee: "LAZADA PAY", Account: card, State: ledger.State{NeedsReview: true}}
	return s, l
}

func TestAChargeWithoutAReceiptTheUserReviewedIsntPutBackInReview(t *testing.T) {
	newRunner, _ := runners(t)
	s, l := lazadaRun(t)
	var out strings.Builder
	if err := newRunner().run(context.Background(), s, &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if !l.txs[12].State.HasLabel(evidence.NoReceipt("lazada")) {
		t.Fatalf("first run: charge without a receipt %+v; want it in review for want of a Lazada receipt\n%s", l.txs[12].State, out.String())
	}
	reviewed := l.txs[12]
	reviewed.State.NeedsReview = false // the user reviewed it and left the label
	l.txs[12] = reviewed
	l.updated = nil
	out.Reset()
	if err := newRunner().run(context.Background(), s, &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if got := l.txs[12].State; got.NeedsReview || slices.Contains(l.updated, 12) {
		t.Errorf("next run: charge %+v after updates to %v; want it left as the user reviewed it\n%s", got, l.updated, out.String())
	}
}

func TestAnEvidenceStoreThatCantBeSavedFailsTheRunBeforeAnythingIsApplied(t *testing.T) {
	newRunner, store := runners(t)
	s, l := grabRun(t)
	r := newRunner()
	blocked := filepath.Join(t.TempDir(), "state")
	var err error
	if r.evidence, err = evidence.Load(filepath.Join(blocked, "evidence.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blocked, nil, 0o600); err != nil { // a file where the store's directory goes
		t.Fatal(err)
	}
	var out strings.Builder
	if err := r.run(context.Background(), s, &out); !errors.Is(err, syscall.ENOTDIR) || l.updates != 0 || store.saves != 0 {
		t.Errorf("run: %v after %d updates and %d trip list saves; want the store's error and nothing applied\n%s", err, l.updates, store.saves, out.String())
	}
}

func TestALazadaOrderOnATripIsntTripSpending(t *testing.T) {
	newRunner, _ := runners(t)
	s, l := tripRun(t)
	s.merchants = []evidence.Merchant{testGrab(), testLazada()}
	s.trips.HomePayees = []string{"LAZADA PAY"}
	s.rates = rates(t, "35")
	s.receipts = &fakeMailbox{messages: map[string][]gmail.Message{lazada.Queries[0]: {{ID: "m1", Received: at("2026-10-06T07:06:00Z"),
		Subject: "We have received your order No 900000000000001",
		Text:    "Your Order #900000000000001 placed on October 6, 2026 at 14:05 via Credit or Debit Card has been confirmed. Total (VAT included) ฿460.00"}}}}
	l.txs[11] = ledger.Transaction{ID: 11, Date: mustParse("2026-10-06"), Amount: -13.18, Payee: "LAZADA PAY", Account: ledger.Account{Name: "Card"}, State: ledger.State{NeedsReview: true}}
	var out strings.Builder
	if err := newRunner().run(context.Background(), s, &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if got := l.txs[11].State; got.Category != 40 || !got.HasLabel("auto-lazada") || got.HasLabel(trips.Label) {
		t.Errorf("order's charge on the trip %+v; want the order category, off the trip\n%s", got, out.String())
	}
}

func TestAGrabChargeTheUserCategorizedIsOnlySuggestedTheReceiptsCategory(t *testing.T) {
	newRunner, _ := runners(t)
	s, l := grabRun(t)
	s.titles = map[ledger.CategoryID]string{30: "Travel", 21: "Rides"}
	charge := l.txs[11]
	charge.State = ledger.State{Category: 30}
	l.txs[11] = charge
	l.txs[12] = ledger.Transaction{ID: 12, Date: mustParse("2026-09-10"), Amount: -3, Payee: "GRAB", Account: charge.Account, State: ledger.State{Category: 30}} // GRAB isn't a new payee
	var out strings.Builder
	if err := newRunner().run(context.Background(), s, &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if got := l.txs[11].State; !reflect.DeepEqual(got, charge.State) || slices.Contains(l.updated, 11) {
		t.Errorf("Grab charge %+v after updates to %v; want it left in Travel and not updated\n%s", got, l.updated, out.String())
	}
	_, suggestions, _ := strings.Cut(out.String(), "\nSuggestions\n")
	if !strings.Contains(suggestions, "Travel → Rides  receipt A-TESTRIDE0001") {
		t.Errorf("want the receipt's category suggested for the Grab charge\n%s", out.String())
	}
}

func TestARunWithSomethingNewInsertsANoticeAndASecondRunNone(t *testing.T) {
	newRunner, _ := runners(t)
	s, l := grabRun(t)
	l.txs[12] = ledger.Transaction{ID: 12, Date: mustParse("2026-11-02"), Amount: -9, Payee: "UNKNOWN SHOP", Account: ledger.Account{Name: "Card"}, State: ledger.State{NeedsReview: true}}
	b := &fakeInbox{}
	notify := &notifier{inbox: b, dir: filepath.Join(t.TempDir(), "reports"), start: time.Now()}
	var out strings.Builder
	r := newRunner()
	r.notify = notify
	if err := r.run(context.Background(), s, &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if len(b.notices) != 1 || !strings.Contains(b.notices[0].Subject, "to review") {
		t.Fatalf("first run's notices %+v; want one counting the transaction put in review\n%s", b.notices, out.String())
	}
	out.Reset()
	r = newRunner()
	r.notify = notify
	if err := r.run(context.Background(), s, &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if len(b.notices) != 1 {
		t.Errorf("second run inserted %+v; want nothing new\n%s", b.notices[1:], out.String())
	}
}

// noticed runs a runner with notices into b and returns its report.
func noticed(t *testing.T, newRunner func() runner, s *session, b *fakeInbox) string {
	t.Helper()
	var out strings.Builder
	r := newRunner()
	r.notify = &notifier{inbox: b, dir: filepath.Join(t.TempDir(), "reports"), start: time.Now()}
	if err := r.run(context.Background(), s, &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	return out.String()
}

func TestAnUnreadableReceiptMakesANoticeOnceAndStaysInTheReport(t *testing.T) {
	newRunner, _ := runners(t)
	s, _ := tripRun(t)
	unreadable := ride("m3", "2026-10-29T08:00:00Z")
	unreadable.Text = noTotal
	s.receipts = &fakeMailbox{messages: map[string][]gmail.Message{grab.Query: {unreadable}}}
	b := &fakeInbox{}
	out := noticed(t, newRunner, s, b)
	if len(b.notices) != 1 || !strings.Contains(b.notices[0].Subject, "1 unreadable receipt") {
		t.Fatalf("first run's notices %+v; want one counting the receipt not read\n%s", b.notices, out)
	}
	out = noticed(t, newRunner, s, b)
	if len(b.notices) != 1 {
		t.Errorf("second run inserted %+v; want nothing new\n%s", b.notices[1:], out)
	}
	if !strings.Contains(out, "\nUnreadable Receipts\n  Received      Merchant  Why\n  Oct 29 08:00  Grab      no total\n") {
		t.Errorf("second run's report lacks the receipt not read\n%s", out)
	}
}

func TestATripHintMakesANoticeOnceAndStaysInTheReport(t *testing.T) {
	newRunner, store := runners(t)
	s, _ := tripRun(t)
	first(t, newRunner, s)
	store.list = trips.List{{Name: "trip-farcity", From: mustParse("2026-10-06"), To: mustParse("2026-10-07")}}
	b := &fakeInbox{}
	out := noticed(t, newRunner, s, b)
	if len(b.notices) != 1 || !strings.Contains(b.notices[0].Subject, "1 trip hint") {
		t.Fatalf("first run's notices %+v; want one counting the trip hint\n%s", b.notices, out)
	}
	out = noticed(t, newRunner, s, b)
	if len(b.notices) != 1 {
		t.Errorf("second run inserted %+v; want nothing new\n%s", b.notices[1:], out)
	}
	if !strings.Contains(out, "\nTrip Hints\n  Farcity trip · Oct 6 – Oct 7 · inferred Oct 5 – Oct 7\n") {
		t.Errorf("second run's report lacks the trip hint\n%s", out)
	}
}

func TestTheReportIsHeadedByTheRunsStartInTheLedgersTimeZone(t *testing.T) {
	newRunner, _ := runners(t)
	s, _ := tripRun(t)
	s.loc = time.FixedZone("ICT", 7*60*60)
	before := time.Now().In(s.loc).Truncate(time.Minute)
	var out strings.Builder
	if err := newRunner().run(context.Background(), s, &out); err != nil {
		t.Fatal(err)
	}
	after := time.Now().In(s.loc)
	header, _, _ := strings.Cut(out.String(), " · ")
	at, err := time.ParseInLocation("Run 2006-01-02 15:04", header, s.loc)
	if err != nil || at.Before(before) || at.After(after) {
		t.Errorf("header %q, %v; want the run's start in the ledger's zone, %v..%v", header, err, before, after)
	}
}

func TestTheReportHeadsTheRunWithItsStartInLocalTime(t *testing.T) {
	local := time.Local
	time.Local = time.FixedZone("UTC+7", 7*3600)
	t.Cleanup(func() { time.Local = local })
	newRunner, _ := runners(t)
	s, _ := tripRun(t)
	s.loc = time.FixedZone("UTC-5", -5*3600)
	var out strings.Builder
	if err := newRunner().run(context.Background(), s, &out); err != nil {
		t.Fatal(err)
	}
	header, _, _ := strings.Cut(out.String(), " ·")
	at, err := time.ParseInLocation("Run 2006-01-02 15:04", header, time.Local)
	if err != nil {
		t.Fatalf("header %q: %v", header, err)
	}
	if d := time.Since(at); d < -time.Minute || d > 2*time.Minute {
		t.Errorf("header %q is %v from now in local time; want the run's start", header, d)
	}
}

// ruleRun is a session whose rules come from a file holding a rule, ACME MOBILE to Phone (3),
// over two ACME MOBILE charges in review and an earlier one the user filed in Phone, so the
// payee isn't new.
func ruleRun(t *testing.T, rule string) (*session, *fakeLedger, string) {
	t.Helper()
	cats := []ledger.Category{{ID: 3, Title: "Phone"}, {ID: 5, Title: "Food"}, {ID: 9, Title: "Cash"}}
	path := filepath.Join(t.TempDir(), "rules.toml")
	if err := os.WriteFile(path, []byte(rule), 0o644); err != nil {
		t.Fatal(err)
	}
	rs, err := rules.Load(path, cats)
	if err != nil {
		t.Fatal(err)
	}
	card := ledger.Account{Name: "Card"}
	l := &fakeLedger{txs: map[int64]ledger.Transaction{
		1: {ID: 1, Date: mustParse("2026-10-05"), Amount: -20, Payee: "ACME MOBILE", Account: card, State: ledger.State{NeedsReview: true}},
		2: {ID: 2, Date: mustParse("2026-10-20"), Amount: -20, Payee: "ACME MOBILE", Account: card, State: ledger.State{NeedsReview: true}},
		3: {ID: 3, Date: mustParse("2026-09-05"), Amount: -20, Payee: "ACME MOBILE", Account: card, State: ledger.State{Category: 3}},
	}}
	s := &session{ledger: l, titles: map[ledger.CategoryID]string{3: "Phone", 5: "Food", 9: "Cash"}, rules: rs,
		rulesFile: rules.File{Path: path, Categories: cats}, today: mustParse("2026-11-03"), card: &fakeCard{}, accounts: []string{"Card"},
		calendar: &fakeCalendar{}, receipts: &fakeMailbox{}, loc: time.UTC,
		attention: attention.Settings{Start: mustParse("2026-12-01"), Size: attention.Size{Absolute: 1e9, SD: 1e3}}}
	return s, l, path
}

const acmeRule = "[[rule]]\npayee = '(?i)^acme mobile'\ncategory = \"Phone\"\n"

func TestARunAmendsTheRuleTheUserOverrodeFromThatChargeOn(t *testing.T) {
	newRunner, _ := runners(t)
	s, l, path := ruleRun(t, acmeRule)
	first(t, newRunner, s)
	if l.txs[1].State.Category != 3 || l.txs[2].State.Category != 3 {
		t.Fatalf("first run: %+v, %+v; want both in Phone", l.txs[1].State, l.txs[2].State)
	}
	late := l.txs[2]
	late.State.Category = 9 // the user's override of the later charge
	l.txs[2] = late
	var out strings.Builder
	if err := newRunner().run(context.Background(), s, &out); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if want := acmeRule[:len(acmeRule)-len("category = \"Phone\"\n")] + "category = \"Cash\"\nstate = \"tentative\"\nfrom = 2026-10-20\n"; string(b) != want {
		t.Errorf("rules file =\n%s\nwant\n%s", b, want)
	}
	if l.txs[1].State.Category != 3 {
		t.Errorf("the earlier charge: %+v; want it left in Phone", l.txs[1].State)
	}
	if got := out.String(); !strings.Contains(got, "New Rules to Review") || !strings.Contains(got, "Amended ^acme mobile: Phone → Cash from Oct 20") || !strings.Contains(got, "apply-all") {
		t.Errorf("report:\n%s", got)
	}
	if got := out.String(); strings.Contains(got, "Overridden Decisions") {
		t.Errorf("report lists the rule's override for the user to apply:\n%s", got)
	}
}

func TestARunAmendsTheRuleForAllChargesWithApplyAll(t *testing.T) {
	newRunner, _ := runners(t)
	s, l, path := ruleRun(t, acmeRule)
	first(t, newRunner, s)
	late := l.txs[2]
	late.State.Category = 9
	late.State.Labels = append(late.State.Labels, decide.ApplyAll)
	l.txs[2] = late
	var out strings.Builder
	if err := newRunner().run(context.Background(), s, &out); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), "from =") || !strings.Contains(string(b), "category = \"Cash\"") {
		t.Errorf("rules file =\n%s\nwant Cash with no start date", b)
	}
	if got := l.txs[1].State; got.Category != 9 || got.NeedsReview {
		t.Errorf("the earlier charge: %+v; want it re-filed in Cash, out of review", got)
	}
	if l.txs[2].State.HasLabel(decide.ApplyAll) {
		t.Errorf("the overridden charge kept apply-all: %+v", l.txs[2].State)
	}
}

func TestARunConfirmsATentativeRuleTheUserAccepted(t *testing.T) {
	newRunner, _ := runners(t)
	s, l, path := ruleRun(t, acmeRule+"state = \"tentative\"\n")
	first(t, newRunner, s)
	if !l.txs[1].State.NeedsReview || !l.txs[2].State.NeedsReview {
		t.Fatalf("first run: %+v, %+v; want both left in review by the tentative rule", l.txs[1].State, l.txs[2].State)
	}
	accepted := l.txs[1]
	accepted.State.NeedsReview = false
	l.txs[1] = accepted
	first(t, newRunner, s)
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "state = \"confirmed\"") {
		t.Errorf("rules file =\n%s\nwant the rule confirmed", b)
	}
	if l.txs[2].State.NeedsReview {
		t.Errorf("the other charge: %+v; want it out of review", l.txs[2].State)
	}
}

func TestARunWhoseRulesFileCantBeRevisedAppliesNothing(t *testing.T) {
	newRunner, _ := runners(t)
	s, l, path := ruleRun(t, acmeRule)
	first(t, newRunner, s)
	late := l.txs[2]
	late.State.Category = 9
	l.txs[2] = late
	s.titles[9] = "Not A Category" // the amendment names a category the file can't load
	l.updates = 0
	var out strings.Builder
	if err := newRunner().run(context.Background(), s, &out); err == nil {
		t.Error("the run succeeded")
	}
	if b, _ := os.ReadFile(path); string(b) != acmeRule {
		t.Errorf("rules file =\n%s\nwant it as it was", b)
	}
	if l.updates != 0 {
		t.Errorf("%d updates; want none", l.updates)
	}
}

func TestALearnedRuleIsAddedTentativeUnderItsComment(t *testing.T) {
	p := learn.Proposal{Key: "TEA HOUSE", Category: 5, Count: 3, Pattern: learn.Pattern("TEA HOUSE")}
	got := additions([]learn.Proposal{p}, map[ledger.CategoryID]string{5: "Food"}, mustParse("2026-11-03"))
	want := []rules.Addition{{Comment: "added 2026-11-03 from 3 categorizations of TEA HOUSE", Rule: rules.Rule{Payee: p.Pattern, Category: "Food", State: rules.Tentative}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("additions = %+v, want %+v", got, want)
	}
}

func TestALearnedRuleFromOneCategorizationSaysSo(t *testing.T) {
	p := learn.Proposal{Key: "TEA HOUSE", Category: 5, Count: 1, Pattern: learn.Pattern("TEA HOUSE")}
	got := additions([]learn.Proposal{p}, map[ledger.CategoryID]string{5: "Food"}, mustParse("2026-11-03"))
	if want := "added 2026-11-03 from 1 categorization of TEA HOUSE"; len(got) != 1 || got[0].Comment != want {
		t.Errorf("additions = %+v, want one under %q", got, want)
	}
}

func TestAnOverrideOfARuleTheUserEditedSinceIsAnOverriddenDecision(t *testing.T) {
	newRunner, _ := runners(t)
	s, l, path := ruleRun(t, acmeRule)
	first(t, newRunner, s)
	edited := acmeRule + "max = 100\n" // the user's edit changes the rule's key
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	var err error
	if s.rules, err = rules.Load(path, s.rulesFile.(rules.File).Categories); err != nil {
		t.Fatal(err)
	}
	late := l.txs[2]
	late.State.Category = 9
	l.txs[2] = late
	var out strings.Builder
	if err := newRunner().run(context.Background(), s, &out); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != edited {
		t.Errorf("rules file =\n%s\nwant it as the user left it", b)
	}
	if got := out.String(); !strings.Contains(got, "Overridden Decisions") || strings.Contains(got, "New Rules to Review") {
		t.Errorf("report:\n%s\nwant the override under Overridden Decisions", got)
	}
}

func TestARunAfterAnAmendmentWithNothingNewLeavesTheRulesFileAlone(t *testing.T) {
	newRunner, _ := runners(t)
	s, l, path := ruleRun(t, acmeRule)
	first(t, newRunner, s)
	late := l.txs[2]
	late.State.Category = 9
	l.txs[2] = late
	first(t, newRunner, s)
	amended, _ := os.ReadFile(path)
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	l.updates = 0
	var out strings.Builder
	if err := newRunner().run(context.Background(), s, &out); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != string(amended) || !info.ModTime().Equal(old) {
		t.Errorf("rules file rewritten (modified %v) =\n%s\nwant\n%s", info.ModTime(), b, amended)
	}
	if l.updates != 0 || strings.Contains(out.String(), "New Rules to Review") {
		t.Errorf("%d updates; report:\n%s\nwant no updates and no New Rules to Review", l.updates, out.String())
	}
}

func TestALearnedRuleInACategoryWithoutATitleIsDropped(t *testing.T) {
	p := learn.Proposal{Key: "TEA HOUSE", Category: 7, Count: 3, Pattern: learn.Pattern("TEA HOUSE")}
	if got := additions([]learn.Proposal{p}, map[ledger.CategoryID]string{5: "Food"}, mustParse("2026-11-03")); len(got) != 0 {
		t.Errorf("additions = %+v, want none", got)
	}
}
