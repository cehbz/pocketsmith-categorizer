package ledger

import (
	"slices"
	"testing"
)

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

func TestCategorizerLabels(t *testing.T) {
	for label, want := range map[string]bool{"auto-rule": true, "auto-trip": true, "grab-auto": true, "x": false, "automatic": false} {
		if got := IsCategorizerLabel(label); got != want {
			t.Errorf("IsCategorizerLabel(%q) = %v, want %v", label, got, want)
		}
	}
}

func TestOwnedAndUserLabels(t *testing.T) {
	s := State{Labels: []string{"x", "auto-rule", "y", "grab-auto"}}
	if !s.Owned() {
		t.Error("state with auto-rule is not owned")
	}
	if got := s.UserLabels(); !slices.Equal(got, []string{"x", "y"}) {
		t.Errorf("UserLabels = %q", got)
	}
	if (State{Labels: []string{"x"}}).Owned() {
		t.Error("state with only a user label is owned")
	}
}

func TestUserDecided(t *testing.T) {
	cases := map[string]struct {
		s    State
		want bool
	}{
		"reviewed by the user":    {State{Category: 3}, true},
		"needs review":            {State{Category: 3, NeedsReview: true}, false},
		"categorizer-owned":       {State{Category: 3, Labels: []string{"auto-rule"}}, false},
		"reviewed, uncategorized": {State{}, false},
	}
	for name, c := range cases {
		if got := (Transaction{State: c.s}).UserDecided(); got != c.want {
			t.Errorf("%s: UserDecided = %v, want %v", name, got, c.want)
		}
	}
}
