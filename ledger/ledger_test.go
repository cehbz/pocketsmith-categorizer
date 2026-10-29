package ledger

import "testing"

func TestStateEqualIgnoresLabelOrder(t *testing.T) {
	a := State{Category: 1, NeedsReview: false, Labels: []string{"x", "grab-auto"}}
	b := State{Category: 1, NeedsReview: false, Labels: []string{"grab-auto", "x"}}
	if !a.Equal(b) {
		t.Error("states differing only in label order are not Equal")
	}
}

func TestStateEqualDetectsDifferences(t *testing.T) {
	base := State{Category: 1, NeedsReview: true, Labels: []string{"x"}}
	for name, other := range map[string]State{
		"category": {Category: 2, NeedsReview: true, Labels: []string{"x"}},
		"review":   {Category: 1, NeedsReview: false, Labels: []string{"x"}},
		"labels":   {Category: 1, NeedsReview: true, Labels: []string{"x", "y"}},
	} {
		if base.Equal(other) {
			t.Errorf("states differing in %s are Equal", name)
		}
	}
	if !(State{Category: 1}).Equal(State{Category: 1, Labels: []string{}}) {
		t.Error("nil and empty labels are not Equal")
	}
}

func TestStateHasLabel(t *testing.T) {
	s := State{Labels: []string{"x", "grab-auto"}}
	if !s.HasLabel("grab-auto") || s.HasLabel("y") {
		t.Errorf("HasLabel wrong for %v", s.Labels)
	}
}
