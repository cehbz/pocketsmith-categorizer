package learn

import (
	"regexp"
	"slices"
	"testing"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

func date(s string) time.Time {
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return d
}

func user(payee, d string, cat ledger.CategoryID) ledger.Transaction {
	return ledger.Transaction{Payee: payee, Date: date(d), Amount: -1, State: ledger.State{Category: cat}}
}

func never(ledger.Transaction) bool { return false }

func TestPayeeKeyIsTheFirstTwoWordsOfLetters(t *testing.T) {
	for payee, want := range map[string]string{
		"GOOGLE *FI AbCDE1":     "GOOGLE FI",
		"Seven-Eleven 1234 HMT": "SEVEN ELEVEN",
		"cafe":                  "CAFE",
	} {
		if got := PayeeKey(payee); got != want {
			t.Errorf("PayeeKey(%q) = %q, want %q", payee, got, want)
		}
	}
}

func TestPayeeKeyOfNoLettersIsEmpty(t *testing.T) {
	if got := PayeeKey("12345-678"); got != "" {
		t.Errorf("PayeeKey = %q, want empty", got)
	}
}

func TestPatternMatchesTheKeysWordsOnly(t *testing.T) {
	re := regexp.MustCompile(Pattern("GOOGLE FI"))
	for payee, want := range map[string]bool{"GOOGLE *FI AbCDE1": true, "Google Fi": true, "GOOGLE *Fitapp Workout": false} {
		if got := re.MatchString(payee); got != want {
			t.Errorf("match %q = %v, want %v", payee, got, want)
		}
	}
}

func TestProposeNeedsUnanimousEvidence(t *testing.T) {
	txs := []ledger.Transaction{
		user("CAFE ONE 1", "2026-01-01", 5), user("CAFE ONE 2", "2026-01-02", 5), user("CAFE ONE 3", "2026-01-03", 5),
		user("MIXED SHOP", "2026-01-01", 5), user("MIXED SHOP", "2026-01-02", 6), user("MIXED SHOP", "2026-01-03", 5),
		user("RARE PLACE", "2026-01-01", 7),
		{Payee: "PENDING CO", State: ledger.State{Category: 8, NeedsReview: true}},
		{Payee: "PENDING CO", State: ledger.State{Category: 8, NeedsReview: true}},
	}
	got := Propose(txs, 2, never)
	if len(got) != 1 || got[0].Key != "CAFE ONE" || got[0].Category != 5 || got[0].Count != 3 || got[0].Pattern != Pattern("CAFE ONE") {
		t.Errorf("Propose = %+v", got)
	}
}

func TestProposeSkipsCoveredPayees(t *testing.T) {
	txs := []ledger.Transaction{user("CAFE ONE", "2026-01-01", 5), user("CAFE ONE", "2026-01-02", 5)}
	if got := Propose(txs, 1, func(ledger.Transaction) bool { return true }); len(got) != 0 {
		t.Errorf("Propose = %+v", got)
	}
}

func TestProposeSkipsEmptyKeys(t *testing.T) {
	txs := []ledger.Transaction{user("1234", "2026-01-01", 5), user("1234", "2026-01-02", 5)}
	if got := Propose(txs, 1, never); len(got) != 0 {
		t.Errorf("Propose = %+v", got)
	}
}

func TestBacktestFindsTheThreshold(t *testing.T) {
	txs := []ledger.Transaction{
		// steady: always 5, twice before the cutoff, twice after
		user("STEADY CO", "2025-01-01", 5), user("STEADY CO", "2025-02-01", 5),
		user("STEADY CO", "2026-03-01", 5), user("STEADY CO", "2026-04-01", 5),
		// fickle: once before as 5, twice after as 6
		user("FICKLE CO", "2025-01-01", 5),
		user("FICKLE CO", "2026-03-01", 6), user("FICKLE CO", "2026-04-01", 6),
	}
	trials := Backtest(txs, date("2026-01-01"), 3)
	want := []Trial{{Min: 1, Evaluated: 4, Correct: 2}, {Min: 2, Evaluated: 2, Correct: 2}, {Min: 3, Evaluated: 0, Correct: 0}}
	if len(trials) != 3 || trials[0] != want[0] || trials[1] != want[1] || trials[2] != want[2] {
		t.Fatalf("Backtest = %+v, want %+v", trials, want)
	}
	if n, ok := Threshold(trials, 0.95, 1); !ok || n != 2 {
		t.Errorf("Threshold = %d, %v, want 2", n, ok)
	}
	if _, ok := Threshold(trials, 0.95, 5); ok {
		t.Error("Threshold found a minimum with too few evaluated")
	}
}

func TestProposeLengthensKeysUntilHistoryAgrees(t *testing.T) {
	txs := []ledger.Transaction{
		user("ZELLE TO BOB SAMPLE", "2026-01-01", 5), user("ZELLE TO BOB SAMPLE", "2026-02-01", 5),
		user("ZELLE TO ALICE EXAMPLE", "2026-01-01", 6), user("ZELLE TO ALICE EXAMPLE", "2026-02-01", 6),
		user("ZELLE TO", "2026-01-01", 7), user("ZELLE TO", "2026-02-01", 7),
		user("ZELLE TO ONCE ONLY", "2026-01-01", 8),
	}
	got := Propose(txs, 2, never)
	want := []Proposal{
		{Key: "ZELLE TO ALICE", Category: 6, Count: 2, Pattern: Pattern("ZELLE TO ALICE")},
		{Key: "ZELLE TO BOB", Category: 5, Count: 2, Pattern: Pattern("ZELLE TO BOB")},
	}
	if !slices.Equal(got, want) {
		t.Errorf("Propose = %+v, want %+v", got, want)
	}
}

func TestProposeSkipsCoveredLengthenedKeys(t *testing.T) {
	txs := []ledger.Transaction{
		user("ZELLE TO BOB", "2026-01-01", 5), user("ZELLE TO BOB", "2026-02-01", 5),
		user("ZELLE TO ALICE", "2026-01-01", 6), user("ZELLE TO ALICE", "2026-02-01", 6),
	}
	got := Propose(txs, 2, func(t ledger.Transaction) bool { return t.Payee == "ZELLE TO BOB" })
	if len(got) != 1 || got[0].Key != "ZELLE TO ALICE" {
		t.Errorf("Propose = %+v", got)
	}
}

func TestBacktestScoresLengthenedKeys(t *testing.T) {
	txs := []ledger.Transaction{
		user("ZELLE TO BOB", "2025-01-01", 5), user("ZELLE TO BOB", "2025-02-01", 5),
		user("ZELLE TO ALICE", "2025-01-01", 6), user("ZELLE TO ALICE", "2025-02-01", 6),
		user("ZELLE TO BOB", "2026-03-01", 5), user("ZELLE TO ALICE", "2026-03-01", 9),
		user("ZELLE TO NEWCOMER", "2026-03-01", 7),
	}
	if got, want := Backtest(txs, date("2026-01-01"), 2)[1], (Trial{Min: 2, Evaluated: 2, Correct: 1}); got != want {
		t.Errorf("Backtest min 2 = %+v, want %+v", got, want)
	}
}

func TestProposeDropsShortKeys(t *testing.T) {
	txs := []ledger.Transaction{user("AB C 1", "2026-01-01", 5), user("AB C 2", "2026-01-02", 5), user("TEAS 1", "2026-01-01", 6), user("TEAS 2", "2026-01-02", 6)}
	got := Propose(txs, 1, never)
	if len(got) != 1 || got[0].Key != "TEAS" {
		t.Errorf("Propose = %+v; want TEAS alone: AB C has 3 letters", got)
	}
}

func TestProposeDropsAPatternMatchingAnotherCategory(t *testing.T) {
	owned := ledger.Transaction{Payee: "TEA HOUSE ANNEX", State: ledger.State{Category: 7, Labels: []string{"auto-rule"}}}
	inReview := ledger.Transaction{Payee: "TEA HOUSE KIOSK", State: ledger.State{Category: 8, NeedsReview: true}}
	uncategorized := ledger.Transaction{Payee: "TEA HOUSE", State: ledger.State{NeedsReview: true}}
	base := []ledger.Transaction{user("TEA HOUSE 1", "2026-01-01", 5), user("TEA HOUSE 2", "2026-01-02", 5)}
	if got := Propose(append(slices.Clone(base), uncategorized), 1, never); len(got) != 1 {
		t.Errorf("Propose with an uncategorized match = %+v; want TEA HOUSE", got)
	}
	for _, other := range []ledger.Transaction{owned, inReview} {
		if got := Propose(append(slices.Clone(base), other), 1, never); len(got) != 0 {
			t.Errorf("Propose with %q in category %d = %+v; want none", other.Payee, other.State.Category, got)
		}
	}
}
