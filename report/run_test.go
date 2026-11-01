package report

import (
	"strings"
	"testing"

	"github.com/cehbz/pocketsmith-categorizer/categorize"
	"github.com/cehbz/pocketsmith-categorizer/decide"
	"github.com/cehbz/pocketsmith-categorizer/internal/day"
	"github.com/cehbz/pocketsmith-categorizer/learn"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
	"github.com/cehbz/pocketsmith-categorizer/rules"
)

var titles = map[ledger.CategoryID]string{3: "Phone", 5: "Food", 9: "Cash"}

func rtx(id int64, payee string, cat ledger.CategoryID) ledger.Transaction {
	d, _ := day.Parse("2026-02-01")
	return ledger.Transaction{ID: id, Date: d, Amount: -12.5, Payee: payee, Account: ledger.Account{Name: "Card"}, State: ledger.State{Category: cat}}
}

func TestWriteRunListsEverySection(t *testing.T) {
	r := Run{
		RunID:       "RUN1",
		Changes:     []categorize.Change{{Charge: 1, Before: ledger.State{Category: 3, NeedsReview: true}, After: ledger.State{Category: 5, Labels: []string{"auto-rule"}}, Source: "rule-a"}},
		Applied:     1,
		Corrections: []categorize.Change{{Charge: 2, Before: ledger.State{Category: 9, Labels: []string{"auto-rule"}}, After: ledger.State{Category: 9}}},
		Amendments:  []categorize.Change{{Charge: 2, Before: ledger.State{Category: 5}, After: ledger.State{Category: 9}, Source: "rule-b"}},
		Undecided:   []ledger.Transaction{rtx(3, "NEW PLACE", 0)},
		Suggestions: []decide.Suggestion{{Transaction: rtx(4, "OLD PLACE", 3), Decision: decide.Decision{Category: 5, Source: "rule-c"}}},
		Proposals:   []learn.Proposal{{Key: "CAFE ONE", Category: 5, Count: 4, Pattern: learn.Pattern("CAFE ONE")}},
		Threshold:   3, ThresholdFound: true,
		Trials:       []learn.Trial{{Min: 3, Evaluated: 40, Correct: 39}},
		Titles:       titles,
		Transactions: map[int64]ledger.Transaction{1: rtx(1, "SHOP", 3), 2: rtx(2, "ATM", 9)},
	}
	var b strings.Builder
	if err := WriteRun(&b, r); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		"run RUN1: applied 1 of 1 changes",
		"2026-02-01 Card -12.50 SHOP: Phone -> Food (rule-a)",
		"2026-02-01 Card -12.50 ATM: you set Cash; categorizer label removed",
		"rule-b decided Food; you chose Cash for 2026-02-01 Card -12.50 ATM",
		"2026-02-01 Card -12.50 NEW PLACE (uncategorized)",
		"2026-02-01 Card -12.50 OLD PLACE: Phone -> Food (rule-c)",
		"proposed rules (1), from 3 or more matching categorizations (39 of 40 right in backtest):",
		`payee = "(?i)\\bCAFE[^A-Za-z]*ONE\\b"`,
		`category = "Food" # 4 categorizations of CAFE ONE`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q:\n%s", want, out)
		}
	}
}

func TestWriteRunWithoutAThreshold(t *testing.T) {
	var b strings.Builder
	if err := WriteRun(&b, Run{RunID: "(none)", Titles: titles}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "no proposals: the backtest found no evidence count reaching 95%") {
		t.Errorf("report:\n%s", b.String())
	}
}

func TestWriteReplay(t *testing.T) {
	st := []rules.Stats{{Rule: rules.Rule{Payee: "(?i)acme", Category: "Phone"}, Matches: 2, Agree: 1, Disagree: []ledger.Transaction{rtx(1, "ACME 2", 5)}}}
	from, _ := day.Parse("2025-01-01")
	to, _ := day.Parse("2026-01-01")
	var b strings.Builder
	if err := WriteReplay(&b, Replay{From: from, To: to, Users: 10, Stats: st, Titles: titles}); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		"replay 2025-01-01..2026-01-01: 10 of your categorizations, 2 matched a rule, 1 agree",
		`payee="(?i)acme"`,
		"2 matches, 1 agree",
		"2026-02-01 Card -12.50 ACME 2: you chose Food, rule says Phone",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q:\n%s", want, out)
		}
	}
}
