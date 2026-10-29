package categorize

import (
	"slices"
	"testing"

	"github.com/cehbz/pocketsmith-categorizer/grab"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

var policy = Policy{Ride: 7, Delivery: 9, Label: "grab-auto"}

func TestCategoryForFollowsTheServiceKind(t *testing.T) {
	if got := policy.CategoryFor(grab.Payment{Service: "Standard"}); got != 7 {
		t.Errorf("ride category = %d, want 7", got)
	}
	if got := policy.CategoryFor(grab.Payment{Service: "GrabMart"}); got != 9 {
		t.Errorf("delivery category = %d, want 9", got)
	}
}

func TestTargetSetsCategoryClearsReviewAddsLabel(t *testing.T) {
	labels := make([]string, 1, 4)
	labels[0] = "x"
	current := ledger.State{Category: 5, NeedsReview: true, Labels: labels}
	got := policy.Target(grab.Payment{Service: "Standard"}, current)
	want := ledger.State{Category: 7, NeedsReview: false, Labels: []string{"x", "grab-auto"}}
	if !got.Equal(want) {
		t.Errorf("Target = %+v, want %+v", got, want)
	}
	if labels[:2][1] == "grab-auto" {
		t.Error("Target appended into the current state's label array")
	}
}

func TestTargetKeepsAnExistingLabelOnce(t *testing.T) {
	got := policy.Target(grab.Payment{Service: "GrabFood"}, ledger.State{Category: 9, Labels: []string{"grab-auto"}})
	if !slices.Equal(got.Labels, []string{"grab-auto"}) {
		t.Errorf("labels = %v, want [grab-auto]", got.Labels)
	}
}

func TestChangesProposesTheTargetState(t *testing.T) {
	pairs := []Pair{{
		Payment: grab.Payment{Service: "Standard"},
		Charge:  ledger.Charge{ID: 11, State: ledger.State{Category: 9, NeedsReview: true}},
	}}
	changes, correct := policy.Changes(pairs)
	if correct != 0 || len(changes) != 1 {
		t.Fatalf("changes %v, correct %d", changes, correct)
	}
	c := changes[0]
	if c.Charge != 11 || c.Before.Category != 9 || !c.Before.NeedsReview {
		t.Errorf("before = %+v", c)
	}
	if !c.After.Equal(ledger.State{Category: 7, Labels: []string{"grab-auto"}}) {
		t.Errorf("after = %+v", c.After)
	}
}

func TestChangesSkipsChargesAlreadyAtTarget(t *testing.T) {
	pairs := []Pair{{
		Payment: grab.Payment{Service: "Standard"},
		Charge:  ledger.Charge{ID: 11, State: ledger.State{Category: 7, Labels: []string{"grab-auto"}}},
	}}
	changes, correct := policy.Changes(pairs)
	if len(changes) != 0 || correct != 1 {
		t.Errorf("changes %v, correct %d; want none and 1", changes, correct)
	}
}

func TestInverseSwapsBeforeAndAfter(t *testing.T) {
	c := Change{Charge: 3, Before: ledger.State{Category: 1}, After: ledger.State{Category: 2}}
	inv := c.Inverse()
	if inv.Charge != 3 || inv.Before.Category != 2 || inv.After.Category != 1 {
		t.Errorf("Inverse = %+v", inv)
	}
}
