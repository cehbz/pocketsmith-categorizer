package main

import (
	"slices"
	"testing"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/categorize"
	"github.com/cehbz/pocketsmith-categorizer/config"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

var (
	tipPolicy  = categorize.Policy{Ride: 7, Delivery: 9, Label: "grab-auto"}
	tipAmounts = config.Tips{Ride: []float64{60}, Delivery: []float64{20, 50}}
)

func thirtyTHB(time.Time) (float64, error) { return 30, nil }

// tipCharge is a charge of thb at 30 THB/USD in category cat, awaiting review.
func tipCharge(id int64, thb float64, cat ledger.CategoryID) ledger.Charge {
	return ledger.Charge{ID: id, USD: thb / 30, State: ledger.State{Category: cat, NeedsReview: true, Labels: []string{"x"}}}
}

func settle(t *testing.T, charges ...ledger.Charge) ([]categorize.Change, int, []ledger.Charge) {
	t.Helper()
	changes, correct, rest, err := settleTips(tipPolicy, tipAmounts, thirtyTHB, charges)
	if err != nil {
		t.Fatal(err)
	}
	return changes, correct, rest
}

func ids(charges []ledger.Charge) []int64 {
	out := make([]int64, len(charges))
	for i, c := range charges {
		out[i] = c.ID
	}
	return out
}

func TestRideTipInDeliveryMovesToRide(t *testing.T) {
	changes, correct, rest := settle(t, tipCharge(1, 60, 9))
	if len(changes) != 1 || correct != 0 || len(rest) != 0 {
		t.Fatalf("changes %v, correct %d, rest %v", changes, correct, rest)
	}
	c := changes[0]
	want := ledger.State{Category: 7, NeedsReview: false, Labels: []string{"x", "grab-auto"}}
	if c.Charge != 1 || c.Before.Category != 9 || !c.After.Equal(want) {
		t.Errorf("change = %+v, want charge 1 from 9 to %+v", c, want)
	}
}

func TestDeliveryTipsInRideMoveToDelivery(t *testing.T) {
	changes, _, rest := settle(t, tipCharge(1, 20, 7), tipCharge(2, 50, 7))
	if len(changes) != 2 || len(rest) != 0 {
		t.Fatalf("changes %v, rest %v", changes, rest)
	}
	for _, c := range changes {
		if c.After.Category != 9 {
			t.Errorf("charge %d -> category %d, want 9", c.Charge, c.After.Category)
		}
	}
}

func TestTipToleranceIsRelative(t *testing.T) {
	inside := tipCharge(1, 60*(1+categorize.Tolerance*0.9), 9)
	outside := tipCharge(2, 60*(1+categorize.Tolerance*1.1), 9)
	changes, _, rest := settle(t, inside, outside)
	if len(changes) != 1 || changes[0].Charge != 1 {
		t.Errorf("changes = %v, want charge 1 only", changes)
	}
	if !slices.Equal(ids(rest), []int64{2}) {
		t.Errorf("rest = %v, want [2]", ids(rest))
	}
}

func TestTipInAnotherCategoryIsLeftAlone(t *testing.T) {
	changes, correct, rest := settle(t, tipCharge(1, 60, 5), tipCharge(2, 20, 0))
	if len(changes) != 0 || correct != 0 {
		t.Errorf("changes %v, correct %d; want none", changes, correct)
	}
	if !slices.Equal(ids(rest), []int64{1, 2}) {
		t.Errorf("rest = %v, want [1 2]", ids(rest))
	}
}

func TestTipAlreadySettledIsCorrect(t *testing.T) {
	c := tipCharge(1, 50, 9)
	c.State = ledger.State{Category: 9, Labels: []string{"grab-auto"}}
	changes, correct, rest := settle(t, c)
	if len(changes) != 0 || correct != 1 || len(rest) != 0 {
		t.Errorf("changes %v, correct %d, rest %v; want none, 1, none", changes, correct, rest)
	}
}

func TestNonTipStaysUnclaimed(t *testing.T) {
	changes, correct, rest := settle(t, tipCharge(1, 135, 7))
	if len(changes) != 0 || correct != 0 || !slices.Equal(ids(rest), []int64{1}) {
		t.Errorf("changes %v, correct %d, rest %v; want none, 0, [1]", changes, correct, ids(rest))
	}
}
