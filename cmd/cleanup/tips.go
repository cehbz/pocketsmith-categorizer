package main

import (
	"math"

	"github.com/cehbz/pocketsmith-categorizer/categorize"
	"github.com/cehbz/pocketsmith-categorizer/config"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

// settleTips categorizes each unclaimed Grab charge whose THB amount is a ride or delivery tip
// and whose category is ride or delivery. It returns the changes, how many tips are already in
// their target state, and the charges it left alone.
func settleTips(p categorize.Policy, tips config.Tips, rates categorize.Rates, charges []ledger.Charge) ([]categorize.Change, int, []ledger.Charge, error) {
	var changes []categorize.Change
	var rest []ledger.Charge
	correct := 0
	for _, c := range charges {
		rate, err := rates(c.Date)
		if err != nil {
			return nil, 0, nil, err
		}
		cat, ok := tipCategory(p, tips, c.USD*rate)
		if !ok || (c.State.Category != p.Ride && c.State.Category != p.Delivery) {
			rest = append(rest, c)
			continue
		}
		target := p.Settle(cat, c.State)
		if c.State.Equal(target) {
			correct++
			continue
		}
		changes = append(changes, categorize.Change{Charge: c.ID, Before: c.State, After: target})
	}
	return changes, correct, rest, nil
}

// tipCategory returns the category of the tip thb is, if it is one.
func tipCategory(p categorize.Policy, tips config.Tips, thb float64) (ledger.CategoryID, bool) {
	switch {
	case isTip(thb, tips.Ride):
		return p.Ride, true
	case isTip(thb, tips.Delivery):
		return p.Delivery, true
	}
	return 0, false
}

// isTip reports whether thb is within categorize.Tolerance of one of the amounts.
func isTip(thb float64, amounts []float64) bool {
	for _, a := range amounts {
		if math.Abs(thb/a-1) <= categorize.Tolerance {
			return true
		}
	}
	return false
}
