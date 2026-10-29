package categorize

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/grab"
	"github.com/cehbz/pocketsmith-categorizer/internal/day"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

var (
	bkk = mustLoad("Asia/Bangkok")
	ny  = mustLoad("America/New_York")
)

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// flat is a rate of thbPerUSD on every date.
func flat(thbPerUSD float64) Rates {
	return func(time.Time) (float64, error) { return thbPerUSD, nil }
}

// pay is a Standard payment at a Bangkok time; 2026-10-12 13:59 has ledger date 2026-10-12.
func pay(booking string, thb float64, at string) grab.Payment {
	t, err := time.ParseInLocation("2006-01-02 15:04", at, bkk)
	if err != nil {
		panic(err)
	}
	return grab.Payment{Booking: booking, Service: "Standard", THB: thb, At: t}
}

func charge(id int64, usd float64, date string) ledger.Charge {
	d, err := day.Parse(date)
	if err != nil {
		panic(err)
	}
	return ledger.Charge{ID: id, USD: usd, Date: d}
}

// pairs maps each paired booking to its charge ID.
func pairs(m Matching) map[string]int64 {
	out := map[string]int64{}
	for _, p := range m.Pairs {
		out[p.Payment.Booking] = p.Charge.ID
	}
	return out
}

func bookings(ps []grab.Payment) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Booking)
	}
	slices.Sort(out)
	return out
}

func chargeIDs(cs []ledger.Charge) []int64 {
	var out []int64
	for _, c := range cs {
		out = append(out, c.ID)
	}
	slices.Sort(out)
	return out
}

func match(t *testing.T, ps []grab.Payment, cs []ledger.Charge) Matching {
	t.Helper()
	m, err := Match(ps, cs, flat(33), ny)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestMatchPairsExactAmount(t *testing.T) {
	m := match(t, []grab.Payment{pay("A", 330, "2026-10-12 13:59")}, []ledger.Charge{charge(1, 10.00, "2026-10-12")})
	if got := pairs(m); got["A"] != 1 || len(got) != 1 {
		t.Fatalf("pairs = %v, want A->1", got)
	}
}

func TestMatchToleranceEdge(t *testing.T) {
	tests := []struct {
		usd  float64
		pair bool
	}{
		{10.19, true},  // 1.86% of the charge
		{10.21, false}, // 2.06%
		{9.81, true},   // 1.94%
		{9.79, false},  // 2.15%
	}
	for _, tt := range tests {
		m := match(t, []grab.Payment{pay("A", 330, "2026-10-12 13:59")}, []ledger.Charge{charge(1, tt.usd, "2026-10-12")})
		if got := len(m.Pairs) == 1; got != tt.pair {
			t.Errorf("charge $%.2f for $10.00 expected: paired %v, want %v", tt.usd, got, tt.pair)
		}
	}
}

func TestMatchPostingWindowEdge(t *testing.T) {
	tests := []struct {
		date string
		pair bool
	}{
		{"2026-10-11", false},
		{"2026-10-12", true},
		{"2026-10-15", true},
		{"2026-10-16", false},
	}
	for _, tt := range tests {
		m := match(t, []grab.Payment{pay("A", 330, "2026-10-12 13:59")}, []ledger.Charge{charge(1, 10.00, tt.date)})
		if got := len(m.Pairs) == 1; got != tt.pair {
			t.Errorf("charge dated %s: paired %v, want %v", tt.date, got, tt.pair)
		}
	}
}

func TestMatchInterchangeablePairs(t *testing.T) {
	m := match(t,
		[]grab.Payment{pay("A", 330, "2026-10-12 13:59"), pay("B", 330, "2026-10-12 15:46")},
		[]ledger.Charge{charge(1, 10.00, "2026-10-12"), charge(2, 10.00, "2026-10-12")})
	got := pairs(m)
	if len(got) != 2 || got["A"] == got["B"] {
		t.Fatalf("pairs = %v, want A and B on distinct charges", got)
	}
	if len(m.Unmatched) != 0 || len(m.Unclaimed) != 0 {
		t.Errorf("leftovers: unmatched %v, unclaimed %v", bookings(m.Unmatched), chargeIDs(m.Unclaimed))
	}
}

func TestMatchReportsLeftoversOnBothSides(t *testing.T) {
	m := match(t,
		[]grab.Payment{pay("A", 330, "2026-10-12 13:59"), pay("B", 990, "2026-10-12 15:00")},
		[]ledger.Charge{charge(1, 10.00, "2026-10-12"), charge(2, 50.00, "2026-10-12")})
	if got := pairs(m); got["A"] != 1 || len(got) != 1 {
		t.Errorf("pairs = %v, want A->1", got)
	}
	if got := bookings(m.Unmatched); !slices.Equal(got, []string{"B"}) {
		t.Errorf("unmatched = %v, want [B]", got)
	}
	if got := chargeIDs(m.Unclaimed); !slices.Equal(got, []int64{2}) {
		t.Errorf("unclaimed = %v, want [2]", got)
	}
}

func TestMatchCompetingPaymentsCheaperWins(t *testing.T) {
	m := match(t,
		[]grab.Payment{pay("A", 330, "2026-10-12 13:59"), pay("B", 333.3, "2026-10-12 14:30")},
		[]ledger.Charge{charge(1, 10.10, "2026-10-12")})
	if got := pairs(m); got["B"] != 1 || len(got) != 1 {
		t.Errorf("pairs = %v, want B->1 (exact) over A (0.99%% off)", got)
	}
	if got := bookings(m.Unmatched); !slices.Equal(got, []string{"A"}) {
		t.Errorf("unmatched = %v, want [A]", got)
	}
}

func TestMatchNeverPairsCredit(t *testing.T) {
	m := match(t, []grab.Payment{pay("A", 330, "2026-10-12 13:59")}, []ledger.Charge{charge(1, -10.00, "2026-10-12")})
	if len(m.Pairs) != 0 {
		t.Fatalf("pairs = %v, want none for a credit", pairs(m))
	}
	if len(m.Unmatched) != 1 || len(m.Unclaimed) != 1 {
		t.Errorf("unmatched %v, unclaimed %v, want one each", bookings(m.Unmatched), chargeIDs(m.Unclaimed))
	}
}

func TestMatchWithNoChargesLeavesEveryPaymentUnmatched(t *testing.T) {
	m := match(t, []grab.Payment{pay("A", 330, "2026-10-12 13:59")}, nil)
	if len(m.Unmatched) != 1 || len(m.Pairs) != 0 {
		t.Errorf("unmatched %v, pairs %v", bookings(m.Unmatched), pairs(m))
	}
}

func TestMatchPropagatesRateError(t *testing.T) {
	failing := func(time.Time) (float64, error) { return 0, errors.New("no rate") }
	_, err := Match([]grab.Payment{pay("A", 330, "2026-10-12 13:59")}, []ledger.Charge{charge(1, 10, "2026-10-12")}, failing, ny)
	if err == nil {
		t.Fatal("Match succeeded without a rate")
	}
}
