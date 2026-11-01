package learn

import (
	"regexp"
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
		"GOOGLE *FI LzNRQ8":     "GOOGLE FI",
		"Seven-Eleven 1234 CNX": "SEVEN ELEVEN",
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
	for payee, want := range map[string]bool{"GOOGLE *FI LzNRQ8": true, "Google Fi": true, "GOOGLE *Fitbod Workout": false} {
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

func TestProposalIDNamesKeyAndCategory(t *testing.T) {
	if got, want := (Proposal{Key: "CAFE ONE", Category: 5}).ID(), "propose CAFE ONE 5"; got != want {
		t.Errorf("ID = %q, want %q", got, want)
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
