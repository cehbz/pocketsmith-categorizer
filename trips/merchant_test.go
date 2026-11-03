package trips

import (
	"testing"

	"github.com/cehbz/pocketsmith-categorizer/internal/day"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

// charge is a card charge whose record names merchant.
func charge(id int64, d string, amount float64, merchant string) Transaction {
	return Transaction{Transaction: ledger.Transaction{ID: id, Date: date(d), Amount: amount, Payee: merchant + " 1234"}, Evidence: Evidence{Merchant: merchant}}
}

func TestTheMerchantIsTheRecordsNameElseThePayeeKey(t *testing.T) {
	ticket := Transaction{Transaction: ledger.Transaction{Payee: "AIRLINE 7Z9XYZ"}, Evidence: Evidence{Merchant: "Airline"}}
	refund := Transaction{Transaction: ledger.Transaction{Payee: "AIRLINE"}, Evidence: Evidence{Merchant: "Airline"}}
	if ticket.Merchant() != "AIRLINE" || refund.Merchant() != "AIRLINE" {
		t.Errorf("merchants = %q, %q, want AIRLINE", ticket.Merchant(), refund.Merchant())
	}
	if got := (Transaction{Transaction: ledger.Transaction{Payee: "GOOGLE *FI AbCDE1"}}).Merchant(); got != "GOOGLE FI" {
		t.Errorf("unmatched merchant = %q, want GOOGLE FI", got)
	}
	if got := (Transaction{Transaction: ledger.Transaction{Payee: "12345"}}).Merchant(); got != "12345" {
		t.Errorf("merchant without letters = %q, want 12345", got)
	}
}

var (
	tripA = Trip{Name: "trip-a", From: date("2026-03-01"), To: date("2026-03-10")}
	tripB = Trip{Name: "trip-b", From: date("2026-06-01"), To: date("2026-06-10")}
)

func TestMerchantClasses(t *testing.T) {
	local, chain, ryokan, named, bakery := charge(2, "2026-03-05", -5, "LOCAL"), charge(4, "2026-03-05", -5, "CHAIN"),
		charge(6, "2026-03-05", -5, "RYOKAN"), charge(7, "2026-03-05", -5, "CAFE HOME TOWN"), charge(9, "2026-02-01", -5, "BAKERY")
	txs := []Transaction{
		charge(1, "2026-02-01", -5, "LOCAL"), local,
		charge(3, "2026-02-01", -5, "CHAIN"), chain, charge(5, "2026-06-05", -5, "CHAIN"),
		ryokan, named, charge(8, "2026-05-05", -5, "CAFE HOME TOWN"), bakery,
	}
	m := NewMerchants(txs, List{tripA, tripB}, Settings{Places: []string{"home town"}})
	for _, c := range []struct {
		t    Transaction
		want Class
	}{{local, HomeMerchant}, {chain, EverywhereMerchant}, {ryokan, TripMerchant}, {named, HomeMerchant}, {bakery, HomeMerchant}} {
		if got := m.Class(c.t); got != c.want {
			t.Errorf("Class(%s) = %v, want %v", c.t.Merchant(), got, c.want)
		}
	}
}

func TestAMonthlyRunOfThreeWithin3PercentIsRecurring(t *testing.T) {
	monthly := []Transaction{charge(1, "2026-01-15", -9.99, "STREAM"), charge(2, "2026-02-14", -9.99, "STREAM"), charge(3, "2026-03-16", -9.99, "STREAM")}
	twice := []Transaction{charge(4, "2026-01-15", -5, "TWICE"), charge(5, "2026-02-14", -5, "TWICE")}
	drifting := []Transaction{charge(9, "2026-01-15", -209.00, "HOSTING"), charge(10, "2026-02-14", -213.50, "HOSTING"), charge(11, "2026-03-16", -218.00, "HOSTING")}
	changing := []Transaction{charge(6, "2026-01-15", -5, "CHANGING"), charge(7, "2026-02-14", -6, "CHANGING"), charge(8, "2026-03-16", -7, "CHANGING")}
	var daily []Transaction
	for i := range 70 {
		daily = append(daily, charge(int64(100+i), day0.AddDate(0, 0, i).Format("2006-01-02"), -1.85, "RIDES"))
	}
	m := NewMerchants(append(append(append(append(monthly, drifting...), twice...), changing...), daily...), nil, Settings{})
	for _, c := range append(monthly, drifting...) {
		if !m.Recurring(c) {
			t.Errorf("charge %d of a monthly run isn't recurring", c.ID)
		}
	}
	for _, c := range append(append(twice, changing...), daily...) {
		if m.Recurring(c) {
			t.Errorf("charge %d (%s) is recurring", c.ID, c.Merchant())
		}
	}
}

var day0 = date("2026-01-01")

func TestHomeActivityIsAHomeMerchantsChargeThatIsntRecurring(t *testing.T) {
	local := charge(2, "2026-03-05", -5, "LOCAL")
	stream := charge(5, "2026-03-05", -9.99, "STREAM")
	credit := charge(6, "2026-03-06", 5, "LOCAL")
	txs := []Transaction{charge(1, "2026-02-01", -5, "LOCAL"), local,
		charge(3, "2026-01-04", -9.99, "STREAM"), charge(4, "2026-02-03", -9.99, "STREAM"), stream, credit}
	m := NewMerchants(txs, List{tripA}, Settings{})
	if !m.HomeActivity(local) || m.HomeActivity(stream) || m.HomeActivity(credit) {
		t.Errorf("home activity: local %v, recurring %v, credit %v; want true, false, false", m.HomeActivity(local), m.HomeActivity(stream), m.HomeActivity(credit))
	}
}

func TestThePayeeKeySkipsTokensWithThreeOrMoreDigits(t *testing.T) {
	for payee, want := range map[string]string{
		"xx20000Q9C7777 ALPHA PLAZA HOMETOWN": "ALPHA PLAZA",
		"xx20000Q9C8888R BETA MART HOMETOWN":  "BETA MART",
		"xx20000Q9C9999 GAMMA BANK FARCITY":   "GAMMA BANK",
		"AIRLINE 0123456789":                  "AIRLINE",
		"7-ELEVEN 54321 FARCITY":              "ELEVEN FARCITY",
		"12GO FARCITY":                        "GO FARCITY",
		"98765":                               "98765",
	} {
		if got := (Transaction{Transaction: ledger.Transaction{Payee: payee}}).Merchant(); got != want {
			t.Errorf("merchant of %q = %q, want %q", payee, got, want)
		}
	}
}

// hometown names home in the two-pass tests; GRAB is always an everywhere merchant.
var hometown = Settings{Places: []string{"HOMETOWN"}, Everywhere: []string{"GRAB"}}

func TestACafeChargedOnlyOnTripsSomeoneWasHomeForStaysHome(t *testing.T) {
	onA, onB := charge(2, "2026-03-05", -4, "CAFE"), charge(3, "2026-06-05", -4, "CAFE")
	txs := []Transaction{charge(1, "2026-02-01", -4, "CAFE"), onA, onB,
		charge(4, "2026-03-06", -2, "MART HOMETOWN"), charge(5, "2026-06-06", -2, "MART HOMETOWN")}
	m := NewMerchants(txs, List{tripA, tripB}, hometown)
	for _, c := range []Transaction{onA, onB} {
		if m.Class(c) != HomeMerchant || !m.HomeActivity(c) {
			t.Errorf("cafe on %s: class %v, home activity %v; want home merchant, home activity", day.Format(c.Date), m.Class(c), m.HomeActivity(c))
		}
	}
}

func TestAChainChargedOnTripsNobodyWasHomeForIsEverywhere(t *testing.T) {
	home, onA, onB := charge(1, "2026-02-01", -3, "CHAIN"), charge(2, "2026-03-05", -3, "CHAIN"), charge(3, "2026-06-05", -3, "CHAIN")
	m := NewMerchants([]Transaction{home, onA, onB}, List{tripA, tripB}, hometown)
	for _, c := range []Transaction{home, onA, onB} {
		if got := m.Class(c); got != EverywhereMerchant {
			t.Errorf("chain on %s: class %v, want everywhere", day.Format(c.Date), got)
		}
	}
}

func TestGrabIsAnEverywhereMerchantWhateverItsHistory(t *testing.T) {
	grab, grabber := charge(2, "2026-02-05", -2, "GRAB TAXI"), charge(4, "2026-02-05", -2, "GRABBER")
	m := NewMerchants([]Transaction{charge(1, "2026-02-01", -2, "GRAB TAXI"), grab, charge(3, "2026-02-01", -2, "GRABBER"), grabber}, List{tripA}, hometown)
	if m.Class(grab) != EverywhereMerchant || m.HomeActivity(grab) {
		t.Errorf("grab at home: class %v, home activity %v; want everywhere, none", m.Class(grab), m.HomeActivity(grab))
	}
	if got := m.Class(grabber); got != HomeMerchant {
		t.Errorf("GRABBER: class %v, want home: GRAB is a word prefix", got)
	}
}

func TestACafeUsedAtHomeAndOnTwoTripsSomeoneWasHomeForIsHomeWithoutAHomeNamedMerchant(t *testing.T) {
	onA, onB := charge(2, "2026-03-05", -4, "CAFE"), charge(3, "2026-06-05", -4, "CAFE")
	txs := []Transaction{charge(1, "2026-02-01", -4, "CAFE"), onA, onB,
		charge(4, "2026-02-02", -6, "BAKERY"), charge(5, "2026-03-06", -6, "BAKERY"), // at home and on trip A only
		charge(6, "2026-02-03", -8, "PHARMACY"), charge(7, "2026-06-06", -8, "PHARMACY")} // at home and on trip B only
	m := NewMerchants(txs, List{tripA, tripB}, hometown)
	for _, c := range []Transaction{onA, onB} {
		if m.Class(c) != HomeMerchant || !m.HomeActivity(c) {
			t.Errorf("cafe on %s: class %v, home activity %v; want home merchant, home activity", day.Format(c.Date), m.Class(c), m.HomeActivity(c))
		}
	}
}

func TestAHomeNamedGrabChargeDoesntShowAnyoneHome(t *testing.T) {
	onA := charge(2, "2026-03-05", -4, "CAFE")
	txs := []Transaction{charge(1, "2026-02-01", -4, "CAFE"), onA, charge(3, "2026-06-05", -4, "CAFE"),
		charge(4, "2026-03-06", -2, "GRAB HOMETOWN"), charge(5, "2026-06-06", -2, "MART HOMETOWN")}
	m := NewMerchants(txs, List{tripA, tripB}, hometown)
	if got := m.Class(onA); got != EverywhereMerchant {
		t.Errorf("cafe: class %v, want everywhere: Grab on trip A shows nobody home", got)
	}
}
