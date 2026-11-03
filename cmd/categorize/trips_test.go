package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/categorize"
	"github.com/cehbz/pocketsmith-categorizer/decide"
	"github.com/cehbz/pocketsmith-categorizer/journal"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
	"github.com/cehbz/pocketsmith-categorizer/trips"
)

func TestTripTransactionsKeepTheCardsAndTheWithdrawalsWithTheirPlannedStates(t *testing.T) {
	card := ledger.Account{Name: "Card"}
	txs := []ledger.Transaction{
		{ID: 1, Date: mustParse("2026-03-10"), Amount: -12.5, Payee: "CAFE", Account: card, State: ledger.State{Category: 3}},
		{ID: 2, Date: mustParse("2026-03-10"), Amount: -4, Payee: "SHOP", Account: card, State: ledger.State{Category: 3}},
		{ID: 3, Date: mustParse("2026-03-10"), Amount: -7, Payee: "BAR", Account: card, State: ledger.State{Category: 4}},
		{ID: 4, Date: mustParse("2026-03-10"), Amount: -9, Payee: "RENT", Account: ledger.Account{Name: "Checking"}},
		{ID: 5, Date: mustParse("2026-03-10"), Amount: -300, Payee: "BANK ATM", Account: ledger.Account{Name: "Checking"}, State: ledger.State{Category: 8}},
	}
	plan := decide.Plan{
		Changes:     []categorize.Change{{Charge: 1, After: ledger.State{Category: 9}}},
		Corrections: []categorize.Change{{Charge: 2, After: ledger.State{Category: 5}}},
	}
	recs := []trips.Record{{Account: "Card", Date: mustParse("2026-03-11"), Amount: 12.5, Name: "CAFE", Merchant: "Cafe", Code: "5812"}}
	cash := trips.Settings{Kinds: trips.Kinds{Home: map[ledger.CategoryID]trips.Kind{8: trips.Cash}}, CashAccount: "Checking"}
	got := tripTransactions(txs, recs, plan, []string{"Card"}, cash)
	if len(got) != 4 || got[3].ID != 5 {
		t.Fatalf("got %d transactions, want the card's 3 and the withdrawal", len(got))
	}
	if got[0].Home.Category != 9 || got[1].Home.Category != 5 || got[2].Home.Category != 4 {
		t.Errorf("home categories = %d, %d, %d; want 9, 5, 4", got[0].Home.Category, got[1].Home.Category, got[2].Home.Category)
	}
	if got[0].Details.Merchant != "Cafe" || got[1].Details != (trips.Details{}) {
		t.Errorf("details = %+v, %+v", got[0].Details, got[1].Details)
	}
}

func TestRememberPlacementsSkipsUnwrittenAndHoldsVersionsUntilEveryWriteLands(t *testing.T) {
	trip := trips.Trip{Name: "trip-a", From: mustParse("2026-03-01"), To: mustParse("2026-03-05")}
	pl := trips.Placing{Keys: map[int64]string{1: "placed 1 x", 2: "placed 2 x"}, Versions: []trips.Trip{trip}}
	placed := emptyMemo(t)
	rememberPlacements(placed, pl, []categorize.Change{{Charge: 2}})
	if !placed.Seen("placed 1 x") || placed.Seen("placed 2 x") || placed.Seen(trip.VersionKey()) {
		t.Error("remembered an unwritten placement, or a version before every write landed")
	}
	rememberPlacements(placed, pl, nil)
	if !placed.Seen("placed 2 x") || !placed.Seen(trip.VersionKey()) {
		t.Error("didn't remember the placements and versions of a run whose writes all landed")
	}
}

type fakeTrips struct {
	list    trips.List
	written []trips.Trip
}

func (f *fakeTrips) Load() (trips.List, []trips.Trip, error) { return f.list, f.written, nil }

func (f *fakeTrips) Save(list trips.List, written []trips.Trip) error {
	f.list, f.written = list, written
	return nil
}

func TestUndoListingRemovesTheAddedTripsAndRestoresTheExtendedOnes(t *testing.T) {
	a, b := trips.Trip{Name: "a"}, trips.Trip{Name: "b", From: mustParse("2026-11-01"), To: mustParse("2026-11-03")}
	longer := b
	longer.To = mustParse("2026-11-05")
	f := &fakeTrips{list: trips.List{a, longer}, written: []trips.Trip{a, longer}}
	if err := undoListing(f, trips.Listing{Added: []trips.Trip{a}, Extended: []trips.Extension{{Before: b, After: longer}}}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.list, trips.List{b}) || !reflect.DeepEqual(f.written, []trips.Trip{b}) {
		t.Errorf("after undoListing: list %v, written %v; want b as before in each", f.list, f.written)
	}
}

func TestUndoForgetsTheRevertedPlacements(t *testing.T) {
	placed := emptyMemo(t)
	for _, k := range []string{"placed 1 trip-a 2026-03-01..2026-03-05", "placed 1 trip-booking", "placed 12 trip-booking"} {
		placed.Add(k)
	}
	forgetPlacements(placed, []ledger.Charge{{ID: 1}})
	if placed.Seen("placed 1 trip-a 2026-03-01..2026-03-05") || placed.Seen("placed 1 trip-booking") || !placed.Seen("placed 12 trip-booking") {
		t.Error("forgot the wrong placements")
	}
}

// fakeCard is a card issuer that records the window it was asked for.
type fakeCard struct {
	recs     []trips.Record
	from, to time.Time
}

func (c *fakeCard) Records(_ context.Context, from, to time.Time) ([]trips.Record, error) {
	c.from, c.to = from, to
	return c.recs, nil
}

func TestInferTripsReadsTheCardFromJustBeforeTheLedgerRead(t *testing.T) {
	card := &fakeCard{recs: []trips.Record{{Account: "Card", Date: mustParse("2026-03-11"), Amount: 12.5, Name: "CAFE", Merchant: "Cafe", Code: "5812"}}}
	s := &session{today: mustParse("2026-11-03"), card: card, accounts: []string{"Card"}}
	txs := []ledger.Transaction{{ID: 1, Date: mustParse("2026-03-10"), Amount: -12.5, Payee: "CAFE", Account: ledger.Account{Name: "Card"}}}
	got, _, err := inferTrips(context.Background(), s, txs, decide.Plan{}, mustParse("2026-01-01"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !card.from.Equal(mustParse("2025-12-30")) || !card.to.Equal(s.today) {
		t.Errorf("records read %v..%v; want 2025-12-30..2026-11-03", card.from, card.to)
	}
	if len(got) != 1 || got[0].Details.Merchant != "Cafe" {
		t.Errorf("trip transactions = %+v; want the card's charge with its details", got)
	}
}

func TestAFailedRunAddsNoTrips(t *testing.T) {
	f := &fakeTrips{}
	j := journal.Store{Dir: t.TempDir()}
	added := []trips.Trip{{Name: "trip-a"}}
	listed, err := listTrips(errors.New("write failed"), f, j, "run-1", trips.List(added), nil, trips.Listing{Added: added})
	if err == nil || !listed.Empty() || f.list != nil || f.written != nil {
		t.Errorf("listed %v, err %v, list %v, written %v; want the run's error and no trips", listed, err, f.list, f.written)
	}
	if c, _ := j.Trips("run-1"); !c.Empty() {
		t.Errorf("journal trips %+v; want none", c)
	}
}

func TestTripsUnrecordedInTheJournalAreNotListed(t *testing.T) {
	f := &fakeTrips{}
	notDir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(notDir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	added := []trips.Trip{{Name: "trip-a"}}
	listed, err := listTrips(nil, f, journal.Store{Dir: notDir}, "run-1", trips.List(added), nil, trips.Listing{Added: added})
	if err == nil || !listed.Empty() || f.list != nil {
		t.Errorf("listed %v, err %v, list %v; want an error and the list unwritten, so undo can remove every trip listed", listed, err, f.list)
	}
}

func TestASuccessfulRunListsAndJournalsItsTrips(t *testing.T) {
	old := trips.Trip{Name: "trip-old", From: mustParse("2026-11-01"), To: mustParse("2026-11-03")}
	longer := old
	longer.To = mustParse("2026-11-05")
	added := []trips.Trip{{Name: "trip-a"}}
	c := trips.Listing{Added: added, Extended: []trips.Extension{{Before: old, After: longer}}}
	f := &fakeTrips{}
	j := journal.Store{Dir: t.TempDir()}
	listed, err := listTrips(nil, f, j, "run-1", trips.List{longer, added[0]}, []trips.Trip{old}, c)
	if err != nil || !reflect.DeepEqual(listed, c) {
		t.Fatalf("listed %+v, err %v; want trip-a added and trip-old extended", listed, err)
	}
	if !reflect.DeepEqual(f.list, trips.List{longer, added[0]}) || !reflect.DeepEqual(f.written, []trips.Trip{longer, added[0]}) {
		t.Errorf("list %v, written %v", f.list, f.written)
	}
	if got, _ := j.Trips("run-1"); !reflect.DeepEqual(got, c) {
		t.Errorf("journal trips %+v; want %+v", got, c)
	}
}
