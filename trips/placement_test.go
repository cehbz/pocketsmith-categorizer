package trips

import (
	"slices"
	"testing"

	"github.com/cehbz/pocketsmith-categorizer/categorize"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

var (
	settings = Settings{Kinds: kinds, Home: Home{Places: []string{"HOME TOWN", "HOMETOWN"}}, Everywhere: []string{"GRAB"}, Start: date("2026-10-26"), CashAccount: "Checking"}
	abroad   = Trip{Name: "trip-abroad", From: date("2026-10-05"), To: date("2026-10-30")}
	past     = Trip{Name: "trip-past", From: date("2026-03-01"), To: date("2026-03-10")}
)

// txn is a card transaction paid in person to merchant with code, in state s as the other
// layers leave it.
func txn(id int64, d string, amount float64, merchant, code string, s ledger.State) Transaction {
	return Transaction{Transaction: ledger.Transaction{ID: id, Date: date(d), Amount: amount, Payee: merchant, State: s}, Details: Details{Merchant: merchant, Code: code, InStore: true}, Home: s}
}

// online is a card transaction paid online to merchant with code, in state s.
func online(id int64, d string, amount float64, merchant, code string, s ledger.State) Transaction {
	t := txn(id, d, amount, merchant, code, s)
	t.Details.InStore = false
	return t
}

// homeCafe is a café settled at home: charged in three months outside trips, on trip-past
// and, as ID 3, on trip-abroad.
func homeCafe() []Transaction {
	return []Transaction{txn(20, "2026-07-02", -4, "HOMECAFE", "5812", ledger.State{Category: 5}), txn(21, "2026-08-02", -5, "HOMECAFE", "5812", ledger.State{Category: 5}),
		txn(22, "2026-09-02", -6, "HOMECAFE", "5812", ledger.State{Category: 5}), txn(23, "2026-03-05", -7, "HOMECAFE", "5812", ledger.State{Category: 5}),
		txn(3, "2026-10-11", -8, "HOMECAFE", "5812", ledger.State{Category: 5})}
}

func seen(keys ...string) func(string) bool {
	return func(k string) bool { return slices.Contains(keys, k) }
}

func placer(list ...Trip) Placer { return Placer{Settings: settings, List: list, Seen: seen()} }

// placed is the change placing made to id.
func placed(p Placing, id int64) (categorize.Change, bool) {
	i := slices.IndexFunc(p.Changes, func(c categorize.Change) bool { return c.Charge == id })
	if i < 0 {
		return categorize.Change{}, false
	}
	return p.Changes[i], true
}

// notPlaced fails when placing made any change to id.
func notPlaced(t *testing.T, p Placing, id int64) {
	t.Helper()
	if c, ok := placed(p, id); ok {
		t.Errorf("transaction %d: change %+v; want none", id, c)
	}
}

func wantState(t *testing.T, p Placing, id int64, after ledger.State) {
	t.Helper()
	if c, ok := placed(p, id); !ok || !c.After.Equal(after) {
		t.Errorf("transaction %d: change %+v, %v; want After %+v", id, c, ok, after)
	}
}

func unchanged(t *testing.T, p Placing, id int64) {
	t.Helper()
	if c, ok := placed(p, id); ok && !c.After.Equal(c.Before) {
		t.Errorf("transaction %d changed: %+v", id, c)
	}
}

func TestAChargeAtANewPlaceOnATripGoesToItsKindsBigSubcategory(t *testing.T) {
	p := placer(abroad).Place([]Transaction{txn(1, "2026-10-10", -100, "HOTEL", "7011", ledger.State{Category: 7, NeedsReview: true})})
	wantState(t, p, 1, ledger.State{Category: 102, NeedsReview: true, Labels: []string{Label, "trip-abroad"}})
	if c, _ := placed(p, 1); c.Source != "trips: trip-abroad" || c.Reason != (categorize.Reason{Why: categorize.OnTrip, Trip: "trip-abroad"}) {
		t.Errorf("source = %q, reason %+v; want trips: trip-abroad, on the trip", c.Source, c.Reason)
	}
	if !slices.Equal(p.Reasons[1], []string{Placed}) || p.Keys[1] != "placed 1 trip-abroad 2026-10-05..2026-10-30" {
		t.Errorf("reasons %v, key %q", p.Reasons[1], p.Keys[1])
	}
	if len(p.Changes) != 1 {
		t.Errorf("changes = %+v, want the one placement", p.Changes)
	}
}

func TestAVisaRunAddsItsLabel(t *testing.T) {
	run := abroad
	run.VisaRun = true
	p := placer(run).Place([]Transaction{txn(1, "2026-10-10", -100, "HOTEL", "7011", ledger.State{Category: 7})})
	wantState(t, p, 1, ledger.State{Category: 102, Labels: []string{Label, "trip-abroad", VisaRun}})
}

func TestAChargeAtAPlaceNamingHomeARecurringChargeAndATransferStay(t *testing.T) {
	transfer := txn(6, "2026-10-10", -500, "WISE", "", ledger.State{Category: 8})
	transfer.Transfer = true
	txs := []Transaction{
		txn(1, "2026-10-10", -5, "CAFE HOME TOWN", "5812", ledger.State{Category: 5}),
		txn(2, "2026-08-31", -9.99, "STREAM", "5815", ledger.State{Category: 9}),
		txn(3, "2026-09-30", -9.99, "STREAM", "5815", ledger.State{Category: 9}),
		txn(4, "2026-10-30", -9.99, "STREAM", "5815", ledger.State{Category: 104, Labels: []string{Label, "trip-abroad"}}), // placed by an earlier run
		txn(5, "2026-03-05", -20, "STREAM", "5815", ledger.State{Category: 9}),
		transfer,
		txn(7, "2026-10-12", -5, "CAFE HOME TOWN", "5812", ledger.State{Category: 103, Labels: []string{Label, "trip-abroad"}}), // so is this
	}
	pl := placer(abroad, past)
	pl.Origins = map[int64]ledger.State{4: {Category: 9}, 7: {Category: 5}}
	p := pl.Place(txs)
	notPlaced(t, p, 1)
	notPlaced(t, p, 6)
	wantState(t, p, 4, ledger.State{Category: 9})
	wantState(t, p, 7, ledger.State{Category: 5})
	if len(p.Reasons) != 0 {
		t.Errorf("reasons = %v, want none", p.Reasons)
	}
}

func TestAnOnlineChargeNamingHomeStaysAndDoesntMakeTheTripHomeActive(t *testing.T) {
	p := placer(abroad).Place([]Transaction{
		online(1, "2026-10-10", -30, "HOMETOWN WATER", "4900", ledger.State{Category: 8}),
		online(2, "2026-10-10", -3, "MART ONLINE", "5499", ledger.State{Category: 7}),
	})
	notPlaced(t, p, 1)
	wantState(t, p, 2, ledger.State{Category: 103, Labels: []string{Label, "trip-abroad"}})
	if len(p.Reasons[1]) != 0 {
		t.Errorf("reasons for the home charge = %v, want none", p.Reasons[1])
	}
}

// stayHome places with 30 (Utilities) and 31 (Doctor) as categories that stay home.
func stayHome(list ...Trip) Placer {
	pl := placer(list...)
	pl.Settings.StayHome = []ledger.CategoryID{30, 31}
	return pl
}

func TestAChargeInAStayHomeCategoryOnATripStaysAndOnePlacedEarlierReturnsToItsOrigin(t *testing.T) {
	pl := stayHome(abroad)
	pl.Origins = map[int64]ledger.State{3: {Category: 31}}
	p := pl.Place([]Transaction{
		online(1, "2026-10-10", -40, "TELCO", "4814", ledger.State{Category: 30}),
		online(2, "2026-10-10", -3, "MART ONLINE", "5499", ledger.State{Category: 7}),
		online(3, "2026-10-12", -80, "DENTIST", "8021", ledger.State{Category: 104, Labels: []string{Label, "trip-abroad"}}), // placed by an earlier run
	})
	notPlaced(t, p, 1)
	wantState(t, p, 2, ledger.State{Category: 103, Labels: []string{Label, "trip-abroad"}})
	wantState(t, p, 3, ledger.State{Category: 31})
	if len(p.Reasons[1]) != 0 || len(p.Reasons[3]) != 0 {
		t.Errorf("reasons = %v, want none for the stay-home charges", p.Reasons)
	}
}

func TestAStayHomePayeesChargeOnATripStaysAndOnePlacedEarlierReturnsToItsOrigin(t *testing.T) {
	pl := placer(abroad)
	pl.Settings.HomePayees = []string{"LAZADA PAY"}
	pl.Origins = map[int64]ledger.State{2: {Category: 40, Labels: []string{"auto-lazada"}}}
	p := pl.Place([]Transaction{
		online(1, "2026-10-10", -13, "LAZADA PAY", "", ledger.State{Category: 40, Labels: []string{"auto-lazada"}}),
		online(2, "2026-10-12", -20, "LAZADA PAY", "", ledger.State{Category: 104, Labels: []string{Label, "trip-abroad"}}), // placed by an earlier run
		online(3, "2026-10-14", 13, "LAZADA PAY", "", ledger.State{Category: 40, Labels: []string{"auto-lazada"}}),          // the first's refund
	})
	notPlaced(t, p, 1)
	wantState(t, p, 2, ledger.State{Category: 40, Labels: []string{"auto-lazada"}})
	notPlaced(t, p, 3)
	if len(p.Reasons) != 0 {
		t.Errorf("reasons = %v, want none", p.Reasons)
	}
}

func TestAStayHomeChargeDoesntMakeTheTripHomeActive(t *testing.T) {
	p := stayHome(abroad).Place([]Transaction{
		online(1, "2026-10-10", -40, "TELCO", "4814", ledger.State{Category: 30}),
		txn(2, "2026-10-10", -2, "GRAB", "4121", ledger.State{Category: 6}),
	})
	notPlaced(t, p, 1)
	wantState(t, p, 2, ledger.State{Category: 101, Labels: []string{Label, "trip-abroad"}})
	if len(p.Reasons[2]) != 1 || p.Reasons[2][0] != Placed {
		t.Errorf("reasons for Grab = %v, want placed", p.Reasons[2])
	}
}

func TestARecurringChargeOnATripNobodyWasHomeForStays(t *testing.T) {
	p := placer(abroad).Place([]Transaction{
		txn(1, "2026-08-31", -9.99, "STREAM", "5815", ledger.State{Category: 9}),
		txn(2, "2026-09-30", -9.99, "STREAM", "5815", ledger.State{Category: 9}),
		txn(3, "2026-10-30", -9.99, "STREAM", "5815", ledger.State{Category: 9}),
	})
	notPlaced(t, p, 3)
	if len(p.Reasons) != 0 {
		t.Errorf("reasons = %v, want none", p.Reasons)
	}
}

func TestAChargeAtASettledPlaceOnATripStays(t *testing.T) {
	placedEarlier := txn(4, "2026-10-12", -9, "HOMECAFE", "5812", ledger.State{Category: 103, Labels: []string{Label, "trip-abroad"}})
	pl := placer(abroad, past)
	pl.Origins = map[int64]ledger.State{4: {Category: 5}}
	p := pl.Place(append(homeCafe(), placedEarlier))
	notPlaced(t, p, 3)
	wantState(t, p, 4, ledger.State{Category: 5})
	if len(p.Reasons) != 0 {
		t.Errorf("reasons = %v, want none", p.Reasons)
	}
}

func TestAPlaceSettledOnlyByAListedTripsDaysIsntSettledAndItsChargeOnATripIsATripCharge(t *testing.T) {
	// Charged in four months, one of them only on trip-past: two months outside the list's
	// trips don't settle it.
	p := placer(abroad, past).Place([]Transaction{
		txn(1, "2026-08-02", -4, "CAFE", "5812", ledger.State{Category: 5}), txn(2, "2026-09-02", -5, "CAFE", "5812", ledger.State{Category: 5}),
		txn(3, "2026-03-05", -6, "CAFE", "5812", ledger.State{Category: 5}), txn(4, "2026-10-11", -7, "CAFE", "5812", ledger.State{Category: 5}),
	})
	wantState(t, p, 4, ledger.State{Category: 103, Labels: []string{Label, "trip-abroad"}})
}

// everywhereCharges are an online shop's and Grab's charges at home and, as IDs 1 and 2,
// abroad.
func everywhereCharges() []Transaction {
	return []Transaction{
		online(10, "2026-09-01", -3, "MART ONLINE", "5499", ledger.State{Category: 7}), online(1, "2026-10-10", -3, "MART ONLINE", "5499", ledger.State{Category: 7}),
		txn(12, "2026-09-01", -2, "GRAB", "4121", ledger.State{Category: 6}), txn(2, "2026-10-10", -2, "GRAB", "4121", ledger.State{Category: 6}),
	}
}

func TestEverywhereChargesAreTripChargesWhenNoOneWasHome(t *testing.T) {
	p := placer(abroad).Place(everywhereCharges())
	wantState(t, p, 1, ledger.State{Category: 103, Labels: []string{Label, "trip-abroad"}})
	wantState(t, p, 2, ledger.State{Category: 101, Labels: []string{Label, "trip-abroad"}})
}

func TestEverywhereChargesAreInDoubtWhenSomeoneWasHome(t *testing.T) {
	p := placer(abroad, past).Place(append(everywhereCharges(), homeCafe()...))
	for _, id := range []int64{1, 2, 3} {
		unchanged(t, p, id)
	}
	if !slices.Equal(p.Reasons[1], []string{Unsure}) || !slices.Equal(p.Reasons[2], []string{Unsure}) || len(p.Reasons[3]) > 0 {
		t.Errorf("reasons = %v; want the shop's and Grab's unsure, the home cafe's none", p.Reasons)
	}
}

func TestAUserCategorizedChargeIsRemappedOncePerVersionAndACorrectionStands(t *testing.T) {
	p := placer(abroad).Place([]Transaction{txn(1, "2026-10-10", -8, "RAMEN", "", ledger.State{Category: 5})})
	wantState(t, p, 1, ledger.State{Category: 103, Labels: []string{Label, "trip-abroad"}})

	corrected := txn(1, "2026-10-10", -8, "RAMEN", "", ledger.State{Category: 104, Labels: []string{"trip-abroad"}})
	key := PlacedPrefix(1) + abroad.Version()
	pl := placer(abroad)
	pl.Seen = seen(key)
	unchanged(t, pl.Place([]Transaction{corrected}), 1)

	longer := abroad
	longer.To = date("2026-10-31")
	pl = placer(longer)
	pl.Seen = seen(key)
	wantState(t, pl.Place([]Transaction{corrected}), 1, ledger.State{Category: 104, Labels: []string{Label, "trip-abroad"}})
}

var later = Trip{Name: "trip-later", From: date("2026-11-15"), To: date("2026-11-25")}

func TestAChargeOnATripTheUserLabelledWithAnotherTripStays(t *testing.T) {
	tickets := txn(1, "2026-10-10", -900, "AIRLINE", "3008", ledger.State{Category: 7, Labels: []string{"trip-later"}}) // bought on trip-abroad
	p := placer(abroad, later).Place([]Transaction{tickets})
	wantState(t, p, 1, tickets.State)
	if len(p.Reasons[1]) != 0 || p.Keys[1] != "" {
		t.Errorf("reasons %v, key %q; want none", p.Reasons[1], p.Keys[1])
	}
}

func TestAChargeThePlacerPutOnItsTripAndTheUserMovedToAnotherStaysMoved(t *testing.T) {
	onAbroad := ledger.State{Category: 101, Labels: []string{Label, "trip-abroad"}}
	for _, labels := range [][]string{{Label, "trip-later"}, {"trip-later"}} { // the user kept or removed Label
		tickets := txn(1, "2026-10-10", -900, "AIRLINE", "3008", ledger.State{Category: 101, Labels: labels})
		tickets.Home = ledger.State{Category: 7}
		pl := placer(abroad, later)
		pl.Seen = seen(PlacedPrefix(1) + abroad.Version())
		pl.Origins = map[int64]ledger.State{1: {Category: 7}}
		pl.Last = map[int64]categorize.Change{1: {Charge: 1, Before: ledger.State{Category: 7}, After: onAbroad, Source: "trips: trip-abroad"}}
		p := pl.Place([]Transaction{tickets})
		wantState(t, p, 1, tickets.State)
		if len(p.Reasons[1]) != 0 {
			t.Errorf("labels %v: reasons %v; want none", labels, p.Reasons[1])
		}
	}
}

func TestAChargeThePlacerPutOnATripRedatedAwayFromItMovesToTheTripItIsIn(t *testing.T) {
	onLater := ledger.State{Category: 102, Labels: []string{Label, "trip-later"}}
	hotel := txn(1, "2026-10-10", -100, "HOTEL", "7011", onLater) // trip-later began on 2026-10-08 when it was placed
	hotel.Home = ledger.State{Category: 7}
	pl := placer(abroad, later)
	pl.Origins = map[int64]ledger.State{1: {Category: 7}}
	pl.Last = map[int64]categorize.Change{1: {Charge: 1, Before: ledger.State{Category: 7}, After: onLater, Source: "trips: trip-later"}}
	p := pl.Place([]Transaction{hotel})
	wantState(t, p, 1, ledger.State{Category: 102, Labels: []string{Label, "trip-abroad"}})
}

func TestABookingTheUserClaimedForATripStaysClaimed(t *testing.T) {
	claimed := ledger.State{Category: 101, Labels: []string{Label, Booking, "trip-later"}}
	booking := txn(1, "2026-11-01", -500, "UNITED", "3000", claimed)
	booking.Home = ledger.State{Category: 7}
	pl := placer(abroad, later)
	pl.Seen = seen(PlacedPrefix(1) + Booking)
	pl.Last = map[int64]categorize.Change{1: {Charge: 1, Before: ledger.State{Category: 7}, After: ledger.State{Category: 101, Labels: []string{Label, Booking}}, Source: "trips: trip-booking"}}
	wantState(t, pl.Place([]Transaction{booking}), 1, claimed)
}

func TestAChargeLeavingEveryTripReturnsToItsOrigin(t *testing.T) {
	shrunk := txn(1, "2026-10-31", -8, "RAMEN", "5812", ledger.State{Category: 103, Labels: []string{Label, "trip-abroad"}})
	gone := txn(2, "2026-06-05", -8, "UDON", "5812", ledger.State{Category: 103, Labels: []string{Label, "trip-gone", "x"}})
	never := txn(3, "2026-11-20", -8, "SOBA", "5812", ledger.State{Category: 5})
	pl := placer(abroad)
	pl.Written = []Trip{{Name: "trip-gone", From: date("2026-06-01"), To: date("2026-06-10")}}
	pl.Origins = map[int64]ledger.State{1: {Category: 5, Labels: []string{"x"}}}
	p := pl.Place([]Transaction{shrunk, gone, never})
	wantState(t, p, 1, ledger.State{Category: 5, Labels: []string{"x"}})
	wantState(t, p, 2, ledger.State{Category: 103, Labels: []string{"x"}})
	notPlaced(t, p, 3)
	if c, _ := placed(p, 1); c.Source != "revert trips" || IsSource(c.Source) || c.Reason != (categorize.Reason{Why: categorize.OffTrip}) {
		t.Errorf("revert source = %q, reason %+v", c.Source, c.Reason)
	}
}

func TestARefundFollowsItsTripChargeAfterTheTripEnds(t *testing.T) {
	later := Trip{Name: "trip-later", From: date("2026-11-15"), To: date("2026-11-25")}
	p := placer(abroad, later).Place([]Transaction{
		txn(1, "2026-10-10", -100, "HOTEL", "7011", ledger.State{Category: 7}),
		txn(2, "2026-11-05", 100, "HOTEL", "7011", ledger.State{Category: 7}),
		txn(3, "2026-11-20", -100, "HOTEL", "7011", ledger.State{Category: 7}), // dated after the refund
	})
	wantState(t, p, 2, ledger.State{Category: 102, Labels: []string{Label, "trip-abroad"}})
	if c, _ := placed(p, 2); c.Source != "trips: trip-abroad refund of 1" || !slices.Equal(p.Reasons[2], []string{Placed}) ||
		c.Reason != (categorize.Reason{Why: categorize.Refund, Trip: "trip-abroad"}) {
		t.Errorf("refund source %q, reason %+v, reasons %v", c.Source, c.Reason, p.Reasons[2])
	}
	if p.Keys[2] != "placed 2 refund 1" {
		t.Errorf("refund key = %q, want placed 2 refund 1", p.Keys[2])
	}
}

func TestARefundFollowsTheChargeItIsCloseTo(t *testing.T) {
	p := placer(abroad).Place([]Transaction{
		txn(1, "2026-09-01", -600.00, "AGENCY", "4722", ledger.State{Category: 7}),
		txn(2, "2026-09-20", -50, "AGENCY", "4722", ledger.State{Category: 7}),
		txn(3, "2026-09-28", 594.00, "AGENCY", "4722", ledger.State{Category: 7}),
	})
	wantState(t, p, 3, ledger.State{Category: 102, Labels: []string{Label, Booking}})
	if c, _ := placed(p, 3); c.Source != "trips: trip-booking refund of 1" || len(p.Reasons[3]) > 0 {
		t.Errorf("refund source %q, reasons %v; want the first booking's, unflagged", c.Source, p.Reasons[3])
	}
}

func TestARefundFollowsTheChargeThatPaidWhatItRefunds(t *testing.T) {
	txs := []Transaction{
		txn(1, "2026-10-10", -12, "NOODLES", "5812", ledger.State{Category: 5}), // on the trip
		txn(2, "2026-11-01", -12, "NOODLES", "5812", ledger.State{Category: 5}), // after it
		txn(3, "2026-11-05", 12, "NOODLES", "5812", ledger.State{Category: 5}),  // the first's refund
	}
	notPlaced(t, placer(abroad).Place(txs), 3) // by amount, it follows the later charge
	pl := placer(abroad)
	pl.Paid = map[int64]int64{3: 1}
	p := pl.Place(txs)
	wantState(t, p, 3, ledger.State{Category: 103, Labels: []string{Label, "trip-abroad"}})
	if c, _ := placed(p, 3); c.Source != "trips: trip-abroad refund of 1" {
		t.Errorf("refund source %q; want trips: trip-abroad refund of 1", c.Source)
	}
}

func TestARefundFarFromItsChargesAmountIsUnsureBeforeTheStartToo(t *testing.T) {
	p := placer(past).Place([]Transaction{
		txn(1, "2026-03-05", -3000.00, "AIRLINE", "3008", ledger.State{Category: 7}),
		txn(2, "2026-03-20", 1000.00, "AIRLINE", "3008", ledger.State{Category: 7}),
	})
	wantState(t, p, 2, ledger.State{Category: 101, Labels: []string{Label, "trip-past"}})
	if !slices.Equal(p.Reasons[2], []string{Unsure}) {
		t.Errorf("reasons = %v, want unsure", p.Reasons[2])
	}
}

func TestARefundFollowsAClaimedBookingAndACorrectedCharge(t *testing.T) {
	claimed := ledger.State{Category: 101, Labels: []string{Label, Booking, "trip-abroad"}}
	corrected := ledger.State{Category: 9, Labels: []string{"trip-abroad"}} // not the category placement would choose
	pl := placer(abroad)
	pl.Corrected = map[int64]bool{3: true}
	booking := txn(1, "2026-09-02", -300, "UNITED", "3000", claimed)
	booking.Home = ledger.State{Category: 7} // what the other layers would leave it
	ramen := txn(3, "2026-10-12", -8, "RAMEN", "5812", corrected)
	ramen.Home = ledger.State{Category: 9, Labels: []string{"trip-abroad"}} // the user's category, as the other layers decide it
	ramen.State.Category = 10                                               // a category the refund must not take
	p := pl.Place([]Transaction{
		booking,
		txn(2, "2026-09-10", 300, "UNITED", "3000", ledger.State{Category: 7}),
		ramen,
		txn(4, "2026-10-20", 8, "RAMEN", "5812", ledger.State{Category: 7}),
	})
	wantState(t, p, 2, ledger.State{Category: 101, Labels: []string{Label, Booking, "trip-abroad"}})
	wantState(t, p, 4, ledger.State{Category: 9, Labels: []string{Label, "trip-abroad"}})
}

func TestCreditsThatArentRefundsStay(t *testing.T) {
	p := placer(abroad).Place([]Transaction{
		txn(1, "2026-09-01", -20, "SHOP", "5999", ledger.State{Category: 7}),
		txn(2, "2026-09-05", 20, "SHOP", "5999", ledger.State{Category: 7}),
		txn(3, "2026-10-10", 5, "NOBODY", "5999", ledger.State{Category: 7}),
		txn(4, "2026-10-12", -15, "TEMPLE", "5999", ledger.State{Category: 7}),
	})
	unchanged(t, p, 2)
	unchanged(t, p, 3)
	wantState(t, p, 4, ledger.State{Category: 104, Labels: []string{Label, "trip-abroad"}})
	if c, _ := placed(p, 4); c.Source != "trips: trip-abroad" || p.Keys[4] != PlacedPrefix(4)+abroad.Version() || len(p.Changes) != 1 {
		t.Errorf("charge placed as %q under key %q among %d changes; want a trip charge, not a refund", c.Source, p.Keys[4], len(p.Changes))
	}
}

// paidVia is an online transaction through a payment gateway whose payee doesn't name the
// merchant to the payee key; its record names merchant, or nothing.
func paidVia(id int64, d string, amount float64, payee, merchant, code string) Transaction {
	t := online(id, d, amount, merchant, code, ledger.State{Category: 7})
	t.Payee = payee
	return t
}

func TestARefundWithoutAMerchantNameFollowsTheChargesItsPayeeNames(t *testing.T) {
	p := placer(abroad).Place([]Transaction{
		paidVia(1, "2026-09-01", -2, "WWW.PAYCO.COM5550001", "", "4511"), // an unnamed booking through the gateway
		paidVia(2, "2026-09-02", -300, "WWW.PAYCO.COM*AIRCO", "AirCo", "3008"),
		paidVia(3, "2026-09-10", -20, "WWW.PAYCO.COM*SHOPCO PAY", "ShopCo Pay", "5262"),
		paidVia(4, "2026-09-12", 20, "WWW.PAYCO.COM*SHOPCO PAY", "", ""),
		paidVia(5, "2026-09-15", 300, "WWW.PAYCO.COM*AIRCO", "", ""),
	})
	unchanged(t, p, 4)
	wantState(t, p, 5, ledger.State{Category: 101, Labels: []string{Label, Booking}})
	if c, _ := placed(p, 5); c.Source != "trips: trip-booking refund of 2" {
		t.Errorf("refund source = %q, want trips: trip-booking refund of 2", c.Source)
	}
}

func TestARefundWhosePayeeCarriesTwoMerchantNamesKeepsItsPayeeKey(t *testing.T) {
	p := placer(abroad).Place([]Transaction{
		paidVia(1, "2026-09-01", -300, "WWW.PAYCO.COM", "AirCo", "3008"),
		paidVia(2, "2026-09-05", -20, "WWW.PAYCO.COM", "ShopCo Pay", "5262"),
		paidVia(3, "2026-09-02", -50, "WWW PAYCO 77", "", "4511"),
		paidVia(4, "2026-09-12", 300, "WWW.PAYCO.COM", "", ""),
	})
	if c, _ := placed(p, 4); c.Source != "trips: trip-booking refund of 3" {
		t.Errorf("refund source = %q, want trips: trip-booking refund of 3, the payee key's charge", c.Source)
	}
}

func TestATravelCodeOutsideEveryTripIsABooking(t *testing.T) {
	p := placer(abroad).Place([]Transaction{
		txn(1, "2026-11-01", -500, "UNITED", "3000", ledger.State{Category: 7}),
		txn(2, "2026-09-02", -300, "THAI", "3000", ledger.State{Category: 101, Labels: []string{Label, Booking, "trip-abroad"}}),
		txn(3, "2026-11-01", -2, "GRAB", "4121", ledger.State{Category: 6}),
		txn(4, "2026-11-01", -90, "RESORT HOME TOWN", "7011", ledger.State{Category: 5}),
	})
	wantState(t, p, 1, ledger.State{Category: 101, Labels: []string{Label, Booking}})
	if c, _ := placed(p, 1); c.Source != "trips: trip-booking" || !slices.Equal(p.Reasons[1], []string{Placed}) ||
		c.Reason != (categorize.Reason{Why: categorize.OnTrip, Trip: Booking}) {
		t.Errorf("booking source %q, reason %+v, reasons %v", c.Source, c.Reason, p.Reasons[1])
	}
	for _, id := range []int64{2, 3, 4} {
		unchanged(t, p, id)
	}
}

func TestABackFilledTripAddsNoFlagsAndKeepsTheReviewFlag(t *testing.T) {
	ruled := txn(3, "2026-03-07", -10, "TEMPLE", "5999", ledger.State{Category: 7, NeedsReview: true})
	ruled.Home = ledger.State{Category: 9, Labels: []string{"auto-rule"}}
	// Grab, charged on the trip while a café settled at home was too, is in doubt: placed once,
	// it is reverted without a reason.
	grab := txn(4, "2026-03-07", -2, "GRAB", "4121", ledger.State{Category: 101, Labels: []string{Label, "trip-past"}})
	p := placer(past).Place([]Transaction{
		txn(1, "2026-03-05", -80, "RYOKAN", "7011", ledger.State{Category: 7, NeedsReview: true}),
		txn(2, "2026-03-06", -10, "TEMPLE", "5999", ledger.State{Category: 7}),
		ruled, grab,
		txn(5, "2026-02-01", -2, "GRAB", "4121", ledger.State{Category: 6}),
		txn(6, "2025-12-02", -4, "HOMECAFE", "5812", ledger.State{Category: 5}), txn(8, "2026-01-02", -5, "HOMECAFE", "5812", ledger.State{Category: 5}),
		txn(9, "2026-02-02", -6, "HOMECAFE", "5812", ledger.State{Category: 5}), txn(7, "2026-03-08", -7, "HOMECAFE", "5812", ledger.State{Category: 5}),
	})
	wantState(t, p, 1, ledger.State{Category: 102, NeedsReview: true, Labels: []string{Label, "trip-past"}})
	wantState(t, p, 2, ledger.State{Category: 104, Labels: []string{Label, "trip-past"}})
	wantState(t, p, 3, ledger.State{Category: 104, NeedsReview: true, Labels: []string{Label, "trip-past"}})
	wantState(t, p, 4, ledger.State{Category: 101})
	if len(p.Reasons) != 0 {
		t.Errorf("reasons = %v, want none for a back-filled trip", p.Reasons)
	}
}

func TestTripsNotPlacedUnderTheirVersionAreReported(t *testing.T) {
	pl := placer(abroad, past)
	pl.Seen = seen(abroad.VersionKey())
	if p := pl.Place(nil); len(p.Versions) != 1 || p.Versions[0].Name != "trip-past" {
		t.Errorf("Versions = %v, want trip-past", p.Versions)
	}
}

// cash is a transaction from payee on the cash account, in the cash category.
func cash(id int64, d string, amount float64, payee string) Transaction {
	s := ledger.State{Category: 8}
	return Transaction{Transaction: ledger.Transaction{ID: id, Date: date(d), Amount: amount, Payee: payee, Account: ledger.Account{Name: "Checking"}, State: s}, Home: s}
}

func TestATripWithdrawalGoesToBigCashAndItsRebateFollowsIt(t *testing.T) {
	p := placer(abroad).Place([]Transaction{
		cash(5, "2026-10-06", -200, "xx20000Q9C7777 ALPHA PLAZA HOMETOWN"),
		cash(1, "2026-10-09", -300.50, "FAR BANK FARCITY"),
		cash(2, "2026-10-31", 1.50, "ATM Fee Rebate"),
		cash(3, "2026-09-09", -300, "xx20000Q9C7777 ALPHA PLAZA HOMETOWN"),
		cash(4, "2026-09-30", 2.10, "ATM Fee Rebate"),
	})
	wantState(t, p, 1, ledger.State{Category: 105, Labels: []string{Label, "trip-abroad"}})
	wantState(t, p, 2, ledger.State{Category: 105, Labels: []string{Label, "trip-abroad"}})
	if !slices.Equal(p.Reasons[2], []string{Placed}) {
		t.Errorf("rebate reasons = %v, want placed and never unsure", p.Reasons[2])
	}
	for _, id := range []int64{3, 4, 5} {
		unchanged(t, p, id)
	}
}

func TestAPlacedChargeThatBecomesInDoubtIsRevertedWithTheUnsureReasonAndKeepsItsReviewFlag(t *testing.T) {
	placedState := ledger.State{Category: 103, NeedsReview: true, Labels: []string{Label, "trip-abroad", Placed}}
	mart := online(1, "2026-10-10", -3, "MART ONLINE", "5499", placedState)
	mart.Home = ledger.State{Category: 7}
	txs := append(homeCafe(), mart)
	pl := placer(abroad, past)
	pl.Seen = seen(PlacedPrefix(1) + abroad.Version())
	pl.Origins = map[int64]ledger.State{1: {Category: 7}}
	p := pl.Place(txs)
	wantState(t, p, 1, ledger.State{Category: 7, NeedsReview: true})
	if c, _ := placed(p, 1); c.Source != "revert trips" || !slices.Equal(p.Reasons[1], []string{Unsure}) {
		t.Errorf("source %q, reasons %v; want a revert with the unsure reason", c.Source, p.Reasons[1])
	}
}

func TestARevertKeepsTheReviewFlagAndLabelsTheUserAddedSincePlacement(t *testing.T) {
	shrunk := txn(1, "2026-10-31", -8, "RAMEN", "5812", ledger.State{Category: 103, NeedsReview: true, Labels: []string{Label, "trip-abroad", Placed, "mine"}})
	pl := placer(abroad)
	pl.Origins = map[int64]ledger.State{1: {Category: 5, Labels: []string{"x"}}}
	wantState(t, pl.Place([]Transaction{shrunk}), 1, ledger.State{Category: 5, NeedsReview: true, Labels: []string{"x", "mine"}})
}

func TestPlacingItsOwnOutputAgainChangesNothing(t *testing.T) {
	var txs []Transaction
	txs = append(txs, txn(1, "2026-10-10", -100, "HOTEL", "7011", ledger.State{Category: 7, NeedsReview: true}),
		txn(2, "2026-11-05", 100, "HOTEL", "7011", ledger.State{Category: 7}),
		txn(3, "2026-11-01", -500, "UNITED", "3000", ledger.State{Category: 7}),
		txn(4, "2026-11-10", 500, "UNITED", "3000", ledger.State{Category: 7}))
	first := placer(abroad).Place(txs)
	keys := []string{abroad.VersionKey()}
	for _, k := range first.Keys {
		keys = append(keys, k)
	}
	var again []Transaction
	for _, tx := range txs {
		if c, ok := placed(first, tx.ID); ok {
			tx.State = c.After
			if r := first.Reasons[tx.ID]; len(r) > 0 {
				tx.State.NeedsReview = true
				tx.State.Labels = append(slices.Clone(c.After.Labels), r...)
			}
			tx.Home = tx.State
		}
		again = append(again, tx)
	}
	pl := placer(abroad)
	pl.Seen = seen(keys...)
	p := pl.Place(again)
	for _, c := range p.Changes {
		if !c.After.Equal(c.Before) {
			t.Errorf("second run changed %+v", c)
		}
	}
	if len(p.Reasons) != 0 || len(p.Versions) != 0 {
		t.Errorf("second run: changes %+v, reasons %v, versions %v; want none", p.Changes, p.Reasons, p.Versions)
	}
}

func TestAPlacedChargeAndAClaimedBookingEmitAChangeEvenWhenNothingChangesSoNoOtherLayersChangeOverwritesThem(t *testing.T) {
	home := ledger.State{Category: 7, Labels: []string{"trip-abroad", "auto-rule"}}
	hotel := txn(1, "2026-10-10", -100, "HOTEL", "7011", ledger.State{Category: 102, Labels: []string{Label, "trip-abroad"}})
	hotel.Home = home
	claimed := txn(2, "2026-09-02", -300, "UNITED", "3000", ledger.State{Category: 101, Labels: []string{Label, Booking, "trip-abroad"}})
	claimed.Home = home
	pl := placer(abroad)
	pl.Seen = seen(PlacedPrefix(1) + abroad.Version())
	p := pl.Place([]Transaction{hotel, claimed})
	for _, tx := range []Transaction{hotel, claimed} {
		if c, ok := placed(p, tx.ID); !ok || !c.After.Equal(tx.State) {
			t.Errorf("transaction %d: change %+v, %v; want one leaving it as it is", tx.ID, c, ok)
		}
	}
}

func TestAChargeBeforeThePlacementStartIsNeitherPlacedNorReverted(t *testing.T) {
	onAbroad := ledger.State{Category: 103, Labels: []string{Label, "trip-abroad"}}
	txs := []Transaction{
		txn(1, "2026-10-10", -100, "HOTEL", "7011", ledger.State{Category: 7}), // in the trip, never placed
		txn(2, "2026-10-02", -20, "CAFE", "5812", onAbroad),                    // placed, its trip's dates since edited
		txn(3, "2026-10-03", 15, "BAR", "5813", onAbroad),                      // placed, with no charge before it
	}
	pl := placer(abroad)
	pl.From = date("2026-10-15")
	p := pl.Place(txs)
	notPlaced(t, p, 1)
	wantState(t, p, 2, onAbroad) // held: a change leaving it as it is
	wantState(t, p, 3, onAbroad)
	for _, id := range []int64{1, 2, 3} {
		if k, ok := p.Keys[id]; ok {
			t.Errorf("transaction %d: key %q; want none", id, k)
		}
	}
}

func TestARefundWhoseChargeIsBeforeThePlacementStartButInTheReadIsLeftAsItIs(t *testing.T) {
	onAbroad := ledger.State{Category: 102, Labels: []string{Label, "trip-abroad"}}
	charge := txn(1, "2026-10-02", -100, "HOTEL", "7011", onAbroad) // placed, its trip's dates since edited
	refund := txn(2, "2026-10-20", 100, "HOTEL", "7011", onAbroad)
	pl := placer(abroad)
	pl.From = date("2026-10-15")
	pl.Seen = seen(PlacedPrefix(2) + "refund 1")
	p := pl.Place([]Transaction{charge, refund})
	unchanged(t, p, 1)
	unchanged(t, p, 2)
	if p.Keys[2] != PlacedPrefix(2)+"refund 1" {
		t.Errorf("refund key = %q, want %q", p.Keys[2], PlacedPrefix(2)+"refund 1")
	}
}

func TestAPlacedTransactionBeforeThePlacementStartIsHeldAgainstOtherLayers(t *testing.T) {
	// Category 104 is not where a fresh placement of the hotel would put it.
	hotel := txn(1, "2026-10-10", -100, "HOTEL", "7011", ledger.State{Category: 104, NeedsReview: true, Labels: []string{Label, "trip-abroad"}})
	hotel.Home = ledger.State{Category: 7, Labels: []string{"auto-rule"}}
	pl := placer(abroad)
	pl.From = date("2026-10-15")
	p := pl.Place([]Transaction{hotel})
	wantState(t, p, 1, hotel.State)
}

func TestOnlyACashCategoryTransactionOnTheCashAccountIsCash(t *testing.T) {
	onAccount := func(id int64, d string, amount float64, payee, account string, category ledger.CategoryID) Transaction {
		s := ledger.State{Category: category}
		return Transaction{Transaction: ledger.Transaction{ID: id, Date: date(d), Amount: amount, Payee: payee, Account: ledger.Account{Name: account}, State: s}, Details: Details{Merchant: payee}, Home: s}
	}
	// A rebate on another account doesn't follow that account's cash-category withdrawal.
	p := placer(abroad).Place([]Transaction{
		onAccount(1, "2026-10-06", -100, "SAVINGS BANK", "Savings", 8),
		onAccount(2, "2026-10-31", 1.50, "ATM Fee Rebate", "Savings", 8),
	})
	notPlaced(t, p, 2)

	// With no cash account, nothing is cash.
	pl := placer(abroad)
	pl.Settings.CashAccount = ""
	p = pl.Place([]Transaction{
		onAccount(3, "2026-10-06", -100, "SOME BANK", "", 8),
		onAccount(4, "2026-10-31", 1.50, "ATM Fee Rebate", "", 8),
	})
	notPlaced(t, p, 4)

	// A credit outside the cash category follows its merchant's charge, not the account's latest charge.
	p = placer(abroad).Place([]Transaction{
		onAccount(5, "2026-10-10", -100, "HOTEL", "Checking", 5),
		onAccount(6, "2026-10-12", -20, "TEMPLE", "Checking", 6),
		onAccount(7, "2026-10-20", 100, "HOTEL", "Checking", 7),
	})
	wantState(t, p, 7, ledger.State{Category: 103, Labels: []string{Label, "trip-abroad"}})
}
