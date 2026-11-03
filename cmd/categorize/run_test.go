package main

import (
	"context"
	"maps"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/attention"
	"github.com/cehbz/pocketsmith-categorizer/categorize"
	"github.com/cehbz/pocketsmith-categorizer/journal"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
	"github.com/cehbz/pocketsmith-categorizer/memo"
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

// fakeLedger is a ledger in memory that counts its updates.
type fakeLedger struct {
	txs     map[int64]ledger.Transaction
	updates int
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
	s := &session{ledger: l, titles: map[ledger.CategoryID]string{}, today: mustParse("2026-11-03"), card: c, accounts: []string{"Card"},
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
		for _, m := range []struct {
			dst  **memo.Memo
			name string
		}{{&r.shown, "shown.json"}, {&r.judged, "judged.json"}, {&r.placed, "placed.json"}} {
			var err error
			if *m.dst, err = memo.Load(filepath.Join(dir, m.name)); err != nil {
				t.Fatal(err)
			}
		}
		return r
	}, store
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
	if got := out.String(); !strings.Contains(got, "trips added (1):") || !strings.Contains(got, "trips with new names or dates (0):") ||
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
	if want := "trips with new names or dates (1):"; !strings.Contains(out.String(), want) {
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
		!strings.Contains(out.String(), "applied 0 of 0 changes") || !strings.Contains(out.String(), "your corrections (1):") {
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
	var out strings.Builder
	if err := newRunner().run(context.Background(), s, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(store.list, trips.List{trip}) || l.updates != 0 || strings.Contains(out.String(), "run (none)") {
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
	if !strings.Contains(out.String(), "trips with new names or dates (1):\n  trip-farcity") || !l.txs[4].State.HasLabel("trip-farcity") {
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
