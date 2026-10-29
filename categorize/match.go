// Package categorize pairs Grab payments with ledger charges and sets their categories.
package categorize

import (
	"math"
	"time"

	"github.com/cehbz/munkres"

	"github.com/cehbz/pocketsmith-categorizer/grab"
	"github.com/cehbz/pocketsmith-categorizer/internal/day"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

// Rates gives the THB per USD reference rate for a date.
type Rates func(date time.Time) (float64, error)

// Tolerance is the largest relative amount error a pair may have.
const Tolerance = 0.02

const (
	lagCost    = 0.001 // per day of posting lag
	impossible = 1e3   // finite stand-in for a forbidden pair; munkres rejects infinities
)

// Pair is a payment and the charge it became.
type Pair struct {
	Payment grab.Payment
	Charge  ledger.Charge
}

// Matching is the result of pairing payments with charges.
type Matching struct {
	Pairs     []Pair
	Unmatched []grab.Payment
	Unclaimed []ledger.Charge
}

// Match pairs payments with charges at minimum total cost; loc is the ledger's time zone.
func Match(payments []grab.Payment, charges []ledger.Charge, rates Rates, loc *time.Location) (Matching, error) {
	if len(payments) == 0 || len(charges) == 0 {
		return Matching{Unmatched: payments, Unclaimed: charges}, nil
	}
	cost := make([][]float64, len(payments))
	for i, p := range payments {
		first, _ := p.PostingWindow(loc)
		rate, err := rates(first)
		if err != nil {
			return Matching{}, err
		}
		cost[i] = make([]float64, len(charges))
		for j, c := range charges {
			cost[i][j] = pairCost(p.THB/rate, first, c)
		}
	}
	h, err := munkres.NewHungarianAlgorithm(cost)
	if err != nil {
		return Matching{}, err
	}
	var m Matching
	claimed := make([]bool, len(charges))
	for i, j := range h.Execute() {
		if j < 0 || cost[i][j] >= impossible {
			m.Unmatched = append(m.Unmatched, payments[i])
			continue
		}
		claimed[j] = true
		m.Pairs = append(m.Pairs, Pair{Payment: payments[i], Charge: charges[j]})
	}
	for j, c := range charges {
		if !claimed[j] {
			m.Unclaimed = append(m.Unclaimed, c)
		}
	}
	return m, nil
}

func pairCost(expectedUSD float64, first time.Time, c ledger.Charge) float64 {
	lag := day.Between(first, c.Date)
	if c.USD <= 0 || lag < 0 || lag > grab.MaxPostingLag {
		return impossible
	}
	e := math.Abs(c.USD-expectedUSD) / c.USD
	if e > Tolerance {
		return impossible
	}
	return e + lagCost*float64(lag)
}
