package categorize

import (
	"slices"

	"github.com/cehbz/pocketsmith-categorizer/grab"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

// Policy is the category for each kind of service and the label a run adds.
type Policy struct {
	Ride, Delivery ledger.CategoryID
	Label          string
}

// CategoryFor returns the ledger category for a payment's service.
func (p Policy) CategoryFor(pay grab.Payment) ledger.CategoryID {
	if pay.Service.Kind() == grab.Ride {
		return p.Ride
	}
	return p.Delivery
}

// Target is the state a charge matched to pay should have: its category, no review, the run label.
func (p Policy) Target(pay grab.Payment, current ledger.State) ledger.State {
	return p.Settle(p.CategoryFor(pay), current)
}

// Settle is the state a charge settled in cat should have: no review, the run label.
func (p Policy) Settle(cat ledger.CategoryID, current ledger.State) ledger.State {
	labels := slices.Clone(current.Labels)
	if !current.HasLabel(p.Label) {
		labels = append(labels, p.Label)
	}
	return ledger.State{Category: cat, NeedsReview: false, Labels: labels}
}

// Change moves one charge from one state to another.
type Change struct {
	Charge int64        `json:"charge"`
	Before ledger.State `json:"before"`
	After  ledger.State `json:"after"`
}

// Changes returns a change for each pair whose charge isn't in its target state, and how many already are.
func (p Policy) Changes(pairs []Pair) ([]Change, int) {
	var changes []Change
	correct := 0
	for _, pr := range pairs {
		target := p.Target(pr.Payment, pr.Charge.State)
		if pr.Charge.State.Equal(target) {
			correct++
			continue
		}
		changes = append(changes, Change{Charge: pr.Charge.ID, Before: pr.Charge.State, After: target})
	}
	return changes, correct
}
