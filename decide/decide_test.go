package decide

import (
	"slices"
	"testing"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/categorize"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

type layerFunc func(ledger.Transaction) (Decision, bool)

func (f layerFunc) Decide(t ledger.Transaction) (Decision, bool) { return f(t) }

// to9 decides category 9 for every transaction.
var to9 = layerFunc(func(ledger.Transaction) (Decision, bool) {
	return Decision{Category: 9, Label: "auto-rule", Source: "r9", Reason: r9}, true
})

var r9 = categorize.Reason{Why: categorize.ByRule, Rule: "r9"}

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
	if c.Charge != 1 || !c.After.Equal(want) || c.Source != "r9" || c.Reason != r9 {
		t.Errorf("change = %+v, want after %+v, source r9 and the rule's reason", c, want)
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

func TestFlaggedDecisionStaysInReview(t *testing.T) {
	s := ledger.State{Category: 3, NeedsReview: true, Labels: []string{"attention-large"}}
	p := Make([]ledger.Transaction{tx(1, "2026-02-01", s)}, []Layer{to9}, nil, since)
	if len(p.Changes) != 1 || !p.Changes[0].After.NeedsReview || !p.Changes[0].After.HasLabel("attention-large") || !p.Changes[0].After.HasLabel("auto-rule") {
		t.Errorf("changes = %+v", p.Changes)
	}
}

func TestADecisionDropsTheLabelItMakesUntrue(t *testing.T) {
	explained := layerFunc(func(ledger.Transaction) (Decision, bool) {
		return Decision{Category: 9, Label: "auto-x", Source: "x 1", Clears: "attention-no-x-receipt"}, true
	})
	for _, c := range []struct {
		labels []string
		want   ledger.State
	}{
		{[]string{"attention-no-x-receipt"}, ledger.State{Category: 9, Labels: []string{"auto-x"}}},
		{[]string{"attention-no-x-receipt", "attention-large", "auto-x"}, ledger.State{Category: 9, NeedsReview: true, Labels: []string{"attention-large", "auto-x"}}},
	} {
		s := ledger.State{Category: 9, NeedsReview: true, Labels: c.labels}
		p := Make([]ledger.Transaction{tx(1, "2026-02-01", s)}, []Layer{explained}, nil, since)
		if len(p.Changes) != 1 || !p.Changes[0].After.Equal(c.want) {
			t.Errorf("labels %v: changes = %+v; want one to %+v", c.labels, p.Changes, c.want)
		}
	}
}

func TestClearedFlaggedDecisionStaysCleared(t *testing.T) {
	s := ledger.State{Category: 9, Labels: []string{"auto-rule", "attention-large"}}
	p := Make([]ledger.Transaction{tx(1, "2026-02-01", s)}, []Layer{to9}, nil, since)
	if len(p.Changes) != 0 {
		t.Errorf("changes = %+v, want none", p.Changes)
	}
}

func TestFlagJoinsAPlannedDecision(t *testing.T) {
	t1 := tx(1, "2026-02-01", ledger.State{Category: 3, NeedsReview: true})
	p := Make([]ledger.Transaction{t1}, []Layer{to9}, nil, since)
	p.Flag(map[int64]ledger.Transaction{1: t1}, map[int64][]string{1: {"attention-new-payee"}})
	a := p.Changes[0].After
	if a.Category != 9 || !a.NeedsReview || !a.HasLabel("auto-rule") || !a.HasLabel("attention-new-payee") || len(p.Flags) != 0 {
		t.Errorf("after = %+v, flags = %+v", a, p.Flags)
	}
}

func TestFlagAddsAChangeWithoutADecision(t *testing.T) {
	t1 := tx(1, "2026-02-01", ledger.State{Category: 4, Labels: []string{"x"}})
	var p Plan
	p.Flag(map[int64]ledger.Transaction{1: t1}, map[int64][]string{1: {"attention-large", "attention-duplicate"}})
	if len(p.Flags) != 1 {
		t.Fatalf("flags = %+v", p.Flags)
	}
	f := p.Flags[0]
	if f.Charge != 1 || !f.Before.Equal(t1.State) || f.After.Category != 4 || !f.After.NeedsReview || !slices.Equal(f.After.Labels, []string{"x", "attention-large", "attention-duplicate"}) {
		t.Errorf("flag = %+v", f)
	}
}

func TestFlagSkipsAnAlreadyFlaggedState(t *testing.T) {
	t1 := tx(1, "2026-02-01", ledger.State{Category: 4, NeedsReview: true, Labels: []string{"attention-large"}})
	var p Plan
	p.Flag(map[int64]ledger.Transaction{1: t1}, map[int64][]string{1: {"attention-large"}})
	if len(p.Flags) != 0 {
		t.Errorf("flags = %+v, want none", p.Flags)
	}
}

func TestFlagLeavesCorrectionsAlone(t *testing.T) {
	t1 := tx(1, "2026-02-01", ledger.State{Category: 4})
	p := Plan{Corrections: []categorize.Change{{Charge: 1}}}
	p.Flag(map[int64]ledger.Transaction{1: t1}, map[int64][]string{1: {"attention-large"}})
	if len(p.Flags) != 0 {
		t.Errorf("flags = %+v, want none", p.Flags)
	}
}

func TestFlagsAreInIDOrder(t *testing.T) {
	txs := map[int64]ledger.Transaction{}
	reasons := map[int64][]string{}
	for _, id := range []int64{5, 2, 9} {
		txs[id] = tx(id, "2026-02-01", ledger.State{})
		reasons[id] = []string{"attention-large"}
	}
	var p Plan
	p.Flag(txs, reasons)
	if len(p.Flags) != 3 || p.Flags[0].Charge != 2 || p.Flags[1].Charge != 5 || p.Flags[2].Charge != 9 {
		t.Errorf("flags = %+v", p.Flags)
	}
}

func TestAPlacementOverridesThePlannedDecision(t *testing.T) {
	p := Plan{Changes: []categorize.Change{{Charge: 1, Before: ledger.State{Category: 3}, After: ledger.State{Category: 9, Labels: []string{"auto-rule"}}, Source: "r9", Reason: r9}}}
	onT := categorize.Reason{Why: categorize.OnTrip, Trip: "t"}
	p.Place([]categorize.Change{{Charge: 1, Before: ledger.State{Category: 3}, After: ledger.State{Category: 7, Labels: []string{"auto-trip"}}, Source: "trips: t", Reason: onT}})
	if len(p.Changes) != 1 || p.Changes[0].After.Category != 7 || p.Changes[0].Source != "trips: t" || p.Changes[0].Reason != onT {
		t.Errorf("changes = %+v, want the placement", p.Changes)
	}
}

func TestAPlacementThatLeavesATransactionAsItIsDropsItsChange(t *testing.T) {
	placed := ledger.State{Category: 7, Labels: []string{"auto-trip"}}
	p := Plan{Changes: []categorize.Change{{Charge: 1, Before: placed, After: ledger.State{Category: 9, Labels: []string{"auto-rule"}}, Source: "r9"}}}
	p.Place([]categorize.Change{{Charge: 1, Before: placed, After: placed, Source: "trips: t"}})
	if len(p.Changes) != 0 {
		t.Errorf("changes = %+v, want none", p.Changes)
	}
}

func TestAnotherPlacementIsAChangeOfItsOwn(t *testing.T) {
	var p Plan
	p.Place([]categorize.Change{{Charge: 2, Before: ledger.State{Category: 3}, After: ledger.State{Category: 7, Labels: []string{"auto-trip"}}, Source: "trips: t"}})
	if len(p.Changes) != 1 || p.Changes[0].Charge != 2 {
		t.Errorf("changes = %+v, want the placement", p.Changes)
	}
}

func TestAPlacedTransactionIsNoLongerUndecided(t *testing.T) {
	p := Plan{Undecided: []ledger.Transaction{{ID: 1}, {ID: 2}}}
	p.Place([]categorize.Change{{Charge: 1, Before: ledger.State{NeedsReview: true}, After: ledger.State{Category: 7, NeedsReview: true, Labels: []string{"auto-trip"}}}})
	if len(p.Undecided) != 1 || p.Undecided[0].ID != 2 {
		t.Errorf("undecided = %+v, want only 2", p.Undecided)
	}
}
