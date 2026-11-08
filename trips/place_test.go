package trips

import (
	"testing"

	"github.com/cehbz/pocketsmith-categorizer/internal/day"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

// locals live in HOMETOWN from 2025-01-01, withdraw cash from Checking and take Grab.
var locals = Settings{Kinds: kinds, Home: Home{Places: []string{"HOMETOWN"}, Since: date("2025-01-01")}, Everywhere: []string{"GRAB"}, CashAccount: "Checking"}

// inStore is a charge paid in person to payee with code.
func inStore(id int64, d string, amount float64, payee, code string) Transaction {
	return Transaction{Transaction: ledger.Transaction{ID: id, Date: date(d), Amount: amount, Payee: payee}, Details: Details{Merchant: payee, Code: code, InStore: true}}
}

// visits are in-store food charges at payee from id first on, one per day, each a tenth more
// than the one before, so none is recurring.
func visits(first int64, payee string, days ...string) []Transaction {
	return coded(first, payee, "5812", days...)
}

// coded are in-store charges with code at payee from id first on, one per day, each a tenth
// more than the one before, so none is recurring.
func coded(first int64, payee, code string, days ...string) []Transaction {
	var out []Transaction
	amount := 10.0
	for i, d := range days {
		out = append(out, inStore(first+int64(i), d, -amount, payee, code))
		amount *= 1.1
	}
	return out
}

func TestAPlaceIsAnInStoreChargeWithAPlacesCodeOrAWithdrawal(t *testing.T) {
	food := inStore(1, "2026-03-10", -10, "NOODLES FARCITY", "5812")
	withdrawal := cash(2, "2026-03-10", -200, "FAR BANK FARCITY")
	digitalCode := inStore(3, "2026-03-10", -10, "APP STORE", "5818")
	noCode := inStore(4, "2026-03-10", -10, "KIOSK FARCITY", "")
	online := inStore(5, "2026-03-10", -10, "SHOP FARCITY", "5812")
	online.Details.InStore = false
	grab := inStore(6, "2026-03-10", -2, "GRAB FARCITY", "4121")
	unmatched := Transaction{Transaction: ledger.Transaction{ID: 7, Date: date("2026-03-10"), Amount: -10, Payee: "MARKET FARCITY"}}
	credit := inStore(8, "2026-03-11", 10, "NOODLES FARCITY", "5812")
	gym := []Transaction{inStore(9, "2026-01-15", -30, "GYM FARCITY", "7997"), inStore(10, "2026-02-14", -30, "GYM FARCITY", "7997"), inStore(11, "2026-03-16", -30, "GYM FARCITY", "7997")}
	p := NewPlaces(append([]Transaction{food, withdrawal, digitalCode, noCode, online, grab, unmatched, credit}, gym...), locals)
	for _, c := range []Transaction{food, withdrawal} {
		if _, ok := p.At(c); !ok {
			t.Errorf("%s isn't at a place", c.Payee)
		}
	}
	for _, c := range append([]Transaction{digitalCode, noCode, online, grab, unmatched, credit}, gym...) {
		if pl, ok := p.At(c); ok {
			t.Errorf("transaction %d (%s) is at place %q; want none", c.ID, c.Payee, pl)
		}
	}
}

func TestAChainsBranchesAreDifferentPlacesAndATerminalIDIsntPartOfOne(t *testing.T) {
	far, home, homeAgain := inStore(1, "2026-03-10", -10, "MART-FARCITY PLAZA", "5411"),
		inStore(2, "2026-03-10", -11, "MART-HOMETOWN", "5411"), inStore(3, "2026-03-12", -12, "xx20000Q9C7777 MART-HOMETOWN", "5411")
	p := NewPlaces([]Transaction{far, home, homeAgain}, locals)
	a, _ := p.At(far)
	b, _ := p.At(home)
	c, _ := p.At(homeAgain)
	if a != "MART FARCITY PLAZA" || b != "MART HOMETOWN" || c != b {
		t.Errorf("places = %q, %q, %q; want MART FARCITY PLAZA, MART HOMETOWN, MART HOMETOWN", a, b, c)
	}
}

func TestAPlaceKeepsTokensWithFewerThanThreeDigitsAndDropsTheRest(t *testing.T) {
	for payee, want := range map[string]Place{
		"12GO FARCITY":               "12GO FARCITY",
		"MART 05 FARCITY":            "MART 05 FARCITY",
		"MART 07 FARCITY":            "MART 07 FARCITY",
		"7-ELEVEN HOMETOWN":          "7 ELEVEN HOMETOWN",
		"xx20000Q9C7777 ALPHA PLAZA": "ALPHA PLAZA",
		"ร้านกาแฟ xx20000Q9C7777":    "ร้านกาแฟ", // raan gaafae, "coffee shop": no ASCII letters, and the terminal id stays out
	} {
		if got := placeOf(payee); got != want {
			t.Errorf("placeOf(%q) = %q, want %q", payee, got, want)
		}
	}
}

var (
	june = Trip{Name: "trip-june", From: date("2025-06-01"), To: date("2025-06-30")}
	sept = Trip{Name: "trip-sept", From: date("2025-09-01"), To: date("2025-09-30")}
)

func TestAPlaceChargedInThreeMonthsOutsideTripsFromTheHomeStartIsSettled(t *testing.T) {
	var txs []Transaction
	place := func(first int64, payee string, days ...string) Place {
		txs = append(txs, visits(first, payee, days...)...)
		return placeOf(payee)
	}
	three := place(1, "CAFE ONE", "2025-02-10", "2025-03-10", "2025-04-10")
	two := place(10, "CAFE TWO", "2025-02-11", "2025-03-11", "2025-06-11") // the third on a trip
	early := place(20, "CAFE EARLY", "2024-11-10", "2024-12-10", "2025-02-12")
	oneMonth := place(30, "CAFE MONTH", "2025-02-01", "2025-02-15", "2025-02-28")
	h := NewPlaces(txs, locals).Haunts(List{june})
	if !h.Settled(three) {
		t.Error("a place charged in 3 months outside trips isn't settled")
	}
	for _, pl := range []Place{two, early, oneMonth} {
		if h.Settled(pl) {
			t.Errorf("%s is settled", pl)
		}
	}
}

func TestAPlaceChargedOnTwoTripsWithFourFifthsOfItsChargesOnThemIsAFavouriteAndNotSettled(t *testing.T) {
	var txs []Transaction
	place := func(first int64, payee string, days ...string) Place {
		txs = append(txs, visits(first, payee, days...)...)
		return placeOf(payee)
	}
	// Three months outside trips and twelve charges on two trips: 80% inside.
	fav := place(1, "RAMEN FARCITY", "2025-02-10", "2025-03-10", "2025-04-10",
		"2025-06-01", "2025-06-02", "2025-06-03", "2025-06-04", "2025-06-05", "2025-06-06",
		"2025-09-01", "2025-09-02", "2025-09-03", "2025-09-04", "2025-09-05", "2025-09-06")
	oneTrip := place(30, "SUSHI FARCITY", "2025-02-11", "2025-06-01", "2025-06-02", "2025-06-03", "2025-06-04")
	mostlyHome := place(40, "UDON FARCITY", "2025-02-12", "2025-06-07", "2025-09-07")
	h := NewPlaces(txs, locals).Haunts(List{june, sept})
	if !h.Favourite(fav) || h.Settled(fav) {
		t.Errorf("ramen: favourite %v, settled %v; want a favourite, not settled", h.Favourite(fav), h.Settled(fav))
	}
	for _, pl := range []Place{oneTrip, mostlyHome} {
		if h.Favourite(pl) {
			t.Errorf("%s is a favourite", pl)
		}
	}
}

func TestHomeActivityIsAChargeAtASettledPlaceOrAtAPlaceNamingHome(t *testing.T) {
	settled := visits(1, "CAFE ONE", "2025-02-10", "2025-03-10", "2025-04-10", "2025-06-10")
	named := inStore(10, "2025-06-11", -5, "MART HOMETOWN", "5411")
	away := inStore(11, "2025-06-12", -9, "NOODLES FARCITY", "5812")
	online := inStore(12, "2025-06-13", -5, "SHOP HOMETOWN", "5999")
	online.Details.InStore = false
	h := NewPlaces(append(settled, named, away, online), locals).Haunts(List{june})
	if !h.Home(settled[3]) || !h.Home(named) || h.Home(away) || h.Home(online) {
		t.Errorf("home activity: settled place %v, place naming home %v, new place %v, online %v; want true, true, false, false",
			h.Home(settled[3]), h.Home(named), h.Home(away), h.Home(online))
	}
}

func TestAMonthlyRunOfThreeWithin3PercentIsRecurring(t *testing.T) {
	monthly := []Transaction{charge(1, "2026-01-15", -9.99, "STREAM"), charge(2, "2026-02-14", -9.99, "STREAM"), charge(3, "2026-03-16", -9.99, "STREAM")}
	twice := []Transaction{charge(4, "2026-01-15", -5, "TWICE"), charge(5, "2026-02-14", -5, "TWICE")}
	drifting := []Transaction{charge(9, "2026-01-15", -209.00, "HOSTING"), charge(10, "2026-02-14", -213.50, "HOSTING"), charge(11, "2026-03-16", -218.00, "HOSTING")}
	changing := []Transaction{charge(6, "2026-01-15", -5, "CHANGING"), charge(7, "2026-02-14", -6, "CHANGING"), charge(8, "2026-03-16", -7, "CHANGING")}
	var daily []Transaction
	for i := range 70 {
		daily = append(daily, charge(int64(100+i), day.Format(date("2026-01-01").AddDate(0, 0, i)), -1.85, "RIDES"))
	}
	p := NewPlaces(append(append(append(append(monthly, drifting...), twice...), changing...), daily...), Settings{})
	for _, c := range append(monthly, drifting...) {
		if !p.Recurring(c) {
			t.Errorf("charge %d of a monthly run isn't recurring", c.ID)
		}
	}
	for _, c := range append(append(twice, changing...), daily...) {
		if p.Recurring(c) {
			t.Errorf("charge %d (%s) is recurring", c.ID, c.Merchant())
		}
	}
}

func TestABookingIsAtNoPlaceSoIsNeverSettled(t *testing.T) {
	// An airline paid online through a gateway the issuer reports as in-store, at home in
	// three months.
	tickets := coded(1, "2C2P THA", "3077", "2025-02-10", "2025-03-10", "2025-04-10")
	p := NewPlaces(tickets, locals)
	for _, c := range tickets {
		if pl, ok := p.At(c); ok {
			t.Errorf("booking %d is at place %q; want none", c.ID, pl)
		}
	}
	if p.Haunts(List{june}).Settled(placeOf("2C2P THA")) {
		t.Error("a payee charged only with bookings is settled")
	}
}

func TestABookingOnATripAtAPayeeBookedFromHomeIsntAHomeCharge(t *testing.T) {
	tickets := coded(1, "2C2P THA", "3077", "2025-02-10", "2025-03-10", "2025-04-10", "2025-06-10")
	h := NewPlaces(tickets, locals).Haunts(List{june})
	if c := h.Class(tickets[3]); c == HomeCharge {
		t.Errorf("a booking on a trip is class %v; want not a home charge", c)
	}
}
