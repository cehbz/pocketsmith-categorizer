package decide

import (
	"testing"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/categorize"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

type layerFunc func(ledger.Transaction) (Decision, bool)

func (f layerFunc) Decide(t ledger.Transaction) (Decision, bool) { return f(t) }

// to9 decides category 9 for every transaction.
var to9 = layerFunc(func(ledger.Transaction) (Decision, bool) {
	return Decision{Category: 9, Label: "auto-rule", Source: "r9"}, true
})

var abstain = layerFunc(func(ledger.Transaction) (Decision, bool) { return Decision{}, false })

func date(s string) time.Time {
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return d
}

var since = date("2026-01-01")

func tx(id int64, d string, s ledger.State) ledger.Transaction {
	return ledger.Transaction{ID: id, Date: date(d), Amount: -1, Payee: "P", State: s}
}

func TestNeedsReviewDecidedIsChanged(t *testing.T) {
	p := Make([]ledger.Transaction{tx(1, "2020-05-01", ledger.State{Category: 3, NeedsReview: true, Labels: []string{"x"}})}, []Layer{to9}, nil, since)
	if len(p.Changes) != 1 {
		t.Fatalf("changes = %+v", p.Changes)
	}
	c := p.Changes[0]
	want := ledger.State{Category: 9, Labels: []string{"x", "auto-rule"}}
	if c.Charge != 1 || !c.After.Equal(want) || c.Source != "r9" {
		t.Errorf("change = %+v, want after %+v and source r9", c, want)
	}
}

func TestNeedsReviewUndecidedIsListed(t *testing.T) {
	p := Make([]ledger.Transaction{tx(1, "2026-02-01", ledger.State{NeedsReview: true})}, []Layer{abstain}, nil, since)
	if len(p.Changes) != 0 || len(p.Undecided) != 1 {
		t.Errorf("plan = %+v", p)
	}
}

func TestOwnedInLookbackIsRedecided(t *testing.T) {
	p := Make([]ledger.Transaction{tx(1, "2026-02-01", ledger.State{Category: 3, Labels: []string{"grab-auto"}})}, []Layer{to9}, nil, since)
	if len(p.Changes) != 1 || !p.Changes[0].After.Equal(ledger.State{Category: 9, Labels: []string{"auto-rule"}}) {
		t.Errorf("changes = %+v", p.Changes)
	}
}

func TestOwnedBeforeTheLookbackIsLeftAlone(t *testing.T) {
	p := Make([]ledger.Transaction{tx(1, "2025-12-31", ledger.State{Category: 3, Labels: []string{"auto-rule"}})}, []Layer{to9}, nil, since)
	if len(p.Changes)+len(p.Suggestions)+len(p.Corrections) != 0 {
		t.Errorf("plan = %+v", p)
	}
}

func TestAbstainingNeverReverts(t *testing.T) {
	p := Make([]ledger.Transaction{tx(1, "2026-02-01", ledger.State{Category: 3, Labels: []string{"auto-rule"}})}, []Layer{abstain}, nil, since)
	if len(p.Changes)+len(p.Undecided) != 0 {
		t.Errorf("plan = %+v", p)
	}
}

func TestAlreadyDecidedNeedsNoChange(t *testing.T) {
	p := Make([]ledger.Transaction{tx(1, "2026-02-01", ledger.State{Category: 9, Labels: []string{"auto-rule"}})}, []Layer{to9}, map[int64]categorize.Change{1: rec(9)}, since)
	if len(p.Changes)+len(p.Corrections) != 0 {
		t.Errorf("plan = %+v", p)
	}
}

func TestCorrectionWinsOverADecision(t *testing.T) {
	p := Make([]ledger.Transaction{tx(1, "2026-02-01", ledger.State{Category: 5, Labels: []string{"auto-rule"}})}, []Layer{to9}, map[int64]categorize.Change{1: rec(9)}, since)
	if len(p.Changes) != 0 || len(p.Corrections) != 1 {
		t.Fatalf("plan = %+v", p)
	}
	if !p.Corrections[0].After.Equal(ledger.State{Category: 5}) {
		t.Errorf("correction = %+v", p.Corrections[0])
	}
}

func TestCorrectionRemovesOnlyCategorizerLabels(t *testing.T) {
	p := Make([]ledger.Transaction{tx(1, "2026-02-01", ledger.State{Category: 5, Labels: []string{"x", "auto-rule"}})}, nil, map[int64]categorize.Change{1: rec(9)}, since)
	if len(p.Corrections) != 1 || !p.Corrections[0].After.Equal(ledger.State{Category: 5, Labels: []string{"x"}}) {
		t.Errorf("corrections = %+v", p.Corrections)
	}
}

func TestOwnedWithoutAJournalRecordIsNoCorrection(t *testing.T) {
	p := Make([]ledger.Transaction{tx(1, "2026-02-01", ledger.State{Category: 5, Labels: []string{"grab-auto"}})}, []Layer{abstain}, nil, since)
	if len(p.Corrections) != 0 {
		t.Errorf("corrections = %+v", p.Corrections)
	}
}

func TestUsersTransactionGetsASuggestionOnlyWhenALayerDisagrees(t *testing.T) {
	txs := []ledger.Transaction{
		tx(1, "2026-02-01", ledger.State{Category: 3}),
		tx(2, "2026-02-01", ledger.State{Category: 9}),
	}
	p := Make(txs, []Layer{to9}, nil, since)
	if len(p.Changes) != 0 || len(p.Suggestions) != 1 || p.Suggestions[0].Transaction.ID != 1 {
		t.Errorf("plan = %+v", p)
	}
}

func TestFirstLayerWins(t *testing.T) {
	to4 := layerFunc(func(ledger.Transaction) (Decision, bool) {
		return Decision{Category: 4, Label: "auto-x", Source: "r4"}, true
	})
	p := Make([]ledger.Transaction{tx(1, "2026-02-01", ledger.State{NeedsReview: true})}, []Layer{abstain, to4, to9}, nil, since)
	if len(p.Changes) != 1 || p.Changes[0].After.Category != 4 {
		t.Errorf("changes = %+v", p.Changes)
	}
}

func TestMergeKeepsOneCopyPerID(t *testing.T) {
	a := []ledger.Transaction{tx(1, "2026-02-01", ledger.State{}), tx(2, "2026-02-01", ledger.State{})}
	b := []ledger.Transaction{tx(2, "2026-02-01", ledger.State{}), tx(3, "2026-02-01", ledger.State{})}
	got := Merge(a, b)
	if len(got) != 3 || got[0].ID != 1 || got[1].ID != 2 || got[2].ID != 3 {
		t.Errorf("Merge = %+v", got)
	}
}

func TestSuggestionKeyNamesTransactionCategoryAndSource(t *testing.T) {
	s := Suggestion{Transaction: tx(7, "2026-02-01", ledger.State{}), Decision: Decision{Category: 9, Source: "r9"}}
	if got, want := s.Key(), "suggest 7 9 r9"; got != want {
		t.Errorf("Key = %q, want %q", got, want)
	}
}

// rec is a journal record of a run that set category c with the auto-rule label.
func rec(c ledger.CategoryID) categorize.Change {
	return categorize.Change{
		Charge: 1,
		Before: ledger.State{Category: 3, NeedsReview: true},
		After:  ledger.State{Category: c, Labels: []string{"auto-rule"}},
	}
}

func TestStateStillAtBeforeIsNotACorrection(t *testing.T) {
	cur := ledger.State{Category: 3, NeedsReview: true, Labels: []string{"auto-rule"}}
	r := rec(9)
	r.Before = cur
	p := Make([]ledger.Transaction{tx(1, "2026-02-01", cur)}, []Layer{to9}, map[int64]categorize.Change{1: r}, since)
	if len(p.Corrections) != 0 || len(p.Changes) != 1 {
		t.Errorf("plan = %+v", p)
	}
}

func TestUserRecategorizingAfterARecordedRunIsACorrection(t *testing.T) {
	p := Make([]ledger.Transaction{tx(1, "2026-02-01", ledger.State{Category: 5, Labels: []string{"auto-rule"}})}, []Layer{to9}, map[int64]categorize.Change{1: rec(9)}, since)
	if len(p.Changes) != 0 || len(p.Corrections) != 1 || p.Corrections[0].After.Category != 5 {
		t.Errorf("plan = %+v", p)
	}
}

func TestCorrectionIsExemptFromTheLookback(t *testing.T) {
	p := Make([]ledger.Transaction{tx(1, "2020-02-01", ledger.State{Category: 5, Labels: []string{"auto-rule"}})}, []Layer{to9}, map[int64]categorize.Change{1: rec(9)}, since)
	if len(p.Corrections) != 1 {
		t.Errorf("plan = %+v", p)
	}
}
