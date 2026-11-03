package trips

import (
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/cehbz/pocketsmith-categorizer/internal/day"
)

// digitalCodes are the merchant category codes of charges made at no place: telecoms,
// software, digital goods, direct marketing and online services.
var digitalCodes = []Range{{4814, 4814}, {4816, 4816}, {4899, 4899}, {5734, 5735}, {5815, 5818}, {5968, 5968}, {7372, 7372}, {8398, 8398}}

const (
	settledMonths  = 3 // fewest calendar months outside trips a settled place is charged in
	favouriteTrips = 2 // fewest trips a favourite is charged on
	favouriteShare = 0.8
)

const (
	minRun      = 3  // fewest monthly charges that make a subscription
	minInterval = 25 // days between a subscription's charges, at least
	maxInterval = 35 // and at most
)

// Place is where a charge was made in person: its payee less the tokens of three or more
// digits, so a chain's branches are different places and a terminal id isn't part of one.
type Place string

// placeOf is the place a charge to payee was made at: the payee less its numbers, upper-cased,
// each run of characters other than A-Z and 0-9 one space (7-ELEVEN is 7 ELEVEN); a payee with
// no ASCII letters or digits left is its upper-cased self less its numbers.
func placeOf(payee string) Place {
	kept := strings.ToUpper(withoutNumbers(payee))
	if alnum := strings.FieldsFunc(kept, func(r rune) bool { return (r < 'A' || r > 'Z') && (r < '0' || r > '9') }); len(alnum) > 0 {
		return Place(strings.Join(alnum, " "))
	}
	return Place(strings.TrimSpace(kept))
}

// Places are the places a run's charges were made at.
type Places struct {
	home      Home
	at        map[int64]Place // each place charge's place
	charges   map[Place][]Transaction
	first     map[Place]time.Time // the day each place was first charged
	recurring map[int64]bool
}

// NewPlaces finds the places of the charges in txs: an in-store card charge whose merchant
// code isn't digital is made at a place, and so is a withdrawal. Grab and recurring charges
// are never at a place.
func NewPlaces(txs []Transaction, s Settings) *Places {
	p := &Places{home: s.Home, at: map[int64]Place{}, charges: map[Place][]Transaction{}, first: map[Place]time.Time{}, recurring: recurringCharges(txs)}
	for _, t := range txs {
		if !t.Charge() || p.recurring[t.ID] || everywhere(t, s.Everywhere) {
			continue
		}
		if s.IsCash(t) || t.Details.InStore && !digital(t.Details.Code) {
			pl := placeOf(t.Payee)
			p.at[t.ID] = pl
			p.charges[pl] = append(p.charges[pl], t)
			if f, ok := p.first[pl]; !ok || t.Date.Before(f) {
				p.first[pl] = t.Date
			}
		}
	}
	return p
}

// At is the place t was charged at; ok is false when it wasn't charged at a place.
func (p *Places) At(t Transaction) (pl Place, ok bool) {
	pl, ok = p.at[t.ID]
	return pl, ok
}

// Recurring reports whether t is one of a monthly run of its merchant's charges.
func (p *Places) Recurring(t Transaction) bool { return p.recurring[t.ID] }

// Haunts measures the places against trips from the home start on.
func (p *Places) Haunts(trips List) Haunts {
	h := Haunts{places: p, settled: map[Place]bool{}, favourite: map[Place]bool{}}
	for pl, cs := range p.charges {
		months := map[time.Time]bool{} // outside every trip
		on := map[string]bool{}        // the trips it was charged on
		inside, all := 0, 0
		for _, t := range cs {
			if t.Date.Before(p.home.Since) {
				continue
			}
			all++
			if trip, ok := trips.At(t.Date); ok {
				inside++
				on[trip.Name] = true
				continue
			}
			months[time.Date(t.Date.Year(), t.Date.Month(), 1, 0, 0, 0, 0, time.UTC)] = true
		}
		h.favourite[pl] = len(on) >= favouriteTrips && float64(inside) >= favouriteShare*float64(all)
		h.settled[pl] = len(months) >= settledMonths && !h.favourite[pl]
	}
	return h
}

// Haunts are the places measured against a trip list: a settled place is charged in at least
// 3 calendar months outside trips; a favourite is charged on at least 2 trips, with at least
// 80% of its charges inside trips, and isn't settled. Only charges from the home start count.
type Haunts struct {
	places    *Places
	settled   map[Place]bool
	favourite map[Place]bool
}

// Settled reports whether pl is a settled place.
func (h Haunts) Settled(pl Place) bool { return h.settled[pl] }

// Favourite reports whether pl is a favourite.
func (h Haunts) Favourite(pl Place) bool { return h.favourite[pl] }

// Home reports whether t is home activity: a charge at a settled place or at a place naming
// home.
func (h Haunts) Home(t Transaction) bool {
	pl, ok := h.places.At(t)
	return ok && (h.settled[pl] || named(t, h.places.home))
}

// Class is what a charge on a trip is, by where it was made.
type Class int

const (
	HomeCharge       Class = iota + 1 // at a settled place or a place naming home, or recurring
	TripCharge                        // at any other place
	EverywhereCharge                  // at no place: online, digital, Grab, unmatched
)

// Class is t's class.
func (h Haunts) Class(t Transaction) Class {
	pl, ok := h.places.At(t)
	switch {
	case h.places.Recurring(t) || ok && (h.settled[pl] || named(t, h.places.home)):
		return HomeCharge
	case ok:
		return TripCharge
	}
	return EverywhereCharge
}

// digital reports whether code is a digital merchant code, or no code.
func digital(code string) bool {
	n, err := strconv.Atoi(code)
	return err != nil || contains(digitalCodes, n)
}

// recurringCharges are the charges in txs in monthly runs of at least minRun from one
// merchant, each the merchant's next charge within closeShare of the one before and 25 to 35
// days after it, so a foreign-currency subscription billed in dollars chains while a merchant
// charged daily doesn't.
func recurringCharges(txs []Transaction) map[int64]bool {
	byMerchant := map[string][]Transaction{}
	for _, t := range txs {
		if t.Charge() {
			byMerchant[t.Merchant()] = append(byMerchant[t.Merchant()], t)
		}
	}
	out := map[int64]bool{}
	for _, cs := range byMerchant {
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
				out[c.ID] = true
			}
		}
	}
	return out
}

// named reports whether t's merchant or payee names a home place.
func named(t Transaction, h Home) bool { return h.Names(t.Merchant()) || h.Names(t.Payee) }

// everywhere reports whether t's merchant begins with one of prefixes as whole words.
func everywhere(t Transaction, prefixes []string) bool {
	merchant := t.Merchant()
	return slices.ContainsFunc(prefixes, func(p string) bool {
		rest, ok := strings.CutPrefix(merchant, strings.ToUpper(p))
		return ok && (rest == "" || !unicode.IsLetter(rune(rest[0])) && !unicode.IsDigit(rune(rest[0])))
	})
}
