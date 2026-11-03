package trips

import (
	"slices"
	"strings"

	"github.com/cehbz/pocketsmith-categorizer/internal/day"
)

// Class is where a merchant is charged.
type Class int

const (
	HomeMerchant       Class = iota + 1 // at home only, or named for a home place
	EverywhereMerchant                  // at home and on other trips: chains, online shops
	TripMerchant                        // on trips only
)

const (
	minRun      = 3  // fewest monthly charges that make a subscription
	minInterval = 25 // days between a subscription's charges, at least
	maxInterval = 35 // and at most
)

// Merchants classifies merchants by their charges' history against a trip list.
type Merchants struct {
	trips     List
	places    []string
	outside   map[string]bool            // merchants charged outside every trip
	inside    map[string]map[string]bool // merchant → the trips it was charged on
	recurring map[int64]bool
}

// NewMerchants indexes the charges in txs against trips; places name home.
func NewMerchants(txs []Transaction, trips List, places []string) *Merchants {
	m := &Merchants{trips: trips, places: places, outside: map[string]bool{}, inside: map[string]map[string]bool{}, recurring: map[int64]bool{}}
	byMerchant := map[string][]Transaction{}
	for _, t := range txs {
		if !t.Charge() {
			continue
		}
		name := t.Merchant()
		byMerchant[name] = append(byMerchant[name], t)
		trip, ok := trips.At(t.Date)
		if !ok {
			m.outside[name] = true
			continue
		}
		if m.inside[name] == nil {
			m.inside[name] = map[string]bool{}
		}
		m.inside[name][trip.Name] = true
	}
	for _, cs := range byMerchant {
		m.markRecurring(cs)
	}
	return m
}

// Class is t's merchant's class for t: its history outside every trip and on trips other
// than t's own decides, so a home merchant stays one while someone else is away.
func (m *Merchants) Class(t Transaction) Class {
	name := t.Merchant()
	own := ""
	if trip, ok := m.trips.At(t.Date); ok {
		own = trip.Name
	}
	elsewhere := false
	for trip := range m.inside[name] {
		elsewhere = elsewhere || trip != own
	}
	switch {
	case named(t, m.places) || m.outside[name] && !elsewhere:
		return HomeMerchant
	case m.outside[name]:
		return EverywhereMerchant
	}
	return TripMerchant
}

// Recurring reports whether t is one of a monthly run of its merchant's charges.
func (m *Merchants) Recurring(t Transaction) bool { return m.recurring[t.ID] }

// HomeActivity reports whether t shows someone at home: a home merchant's charge that isn't
// recurring.
func (m *Merchants) HomeActivity(t Transaction) bool {
	return t.Charge() && m.Class(t) == HomeMerchant && !m.Recurring(t)
}

// markRecurring marks the charges in runs of at least minRun, each the merchant's next charge
// within closeShare of the one before and 25 to 35 days after it, so a foreign-currency
// subscription billed in dollars chains while a merchant charged daily doesn't.
func (m *Merchants) markRecurring(cs []Transaction) {
	slices.SortFunc(cs, byDate)
	next := make([]int, len(cs)) // the next charge near in amount, if it is 25 to 35 days later; -1 if none
	for i := range cs {
		next[i] = -1
		for j := i + 1; j < len(cs); j++ {
			if near(cs[j].Amount, cs[i].Amount) {
				if gap := day.Between(cs[i].Date, cs[j].Date); gap >= minInterval && gap <= maxInterval {
					next[i] = j
				}
				break
			}
		}
	}
	back, fwd := make([]int, len(cs)), make([]int, len(cs)) // run lengths ending and starting at each charge
	for i := range cs {
		back[i] = max(back[i], 1)
		if next[i] >= 0 {
			back[next[i]] = max(back[next[i]], back[i]+1)
		}
	}
	for i := len(cs) - 1; i >= 0; i-- {
		fwd[i] = 1
		if next[i] >= 0 {
			fwd[i] = fwd[next[i]] + 1
		}
	}
	for i, c := range cs {
		if back[i]+fwd[i]-1 >= minRun {
			m.recurring[c.ID] = true
		}
	}
}

// named reports whether t's merchant or payee names a home place.
func named(t Transaction, places []string) bool {
	merchant, payee := t.Merchant(), strings.ToUpper(t.Payee)
	return slices.ContainsFunc(places, func(place string) bool {
		place = strings.ToUpper(place)
		return strings.Contains(merchant, place) || strings.Contains(payee, place)
	})
}
