package trips

import (
	"cmp"
	"slices"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/internal/day"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

const evidenceGap = 2 // most days between a run's evidence days

// Settings are what inference and placing need besides the transactions.
type Settings struct {
	Kinds       Kinds
	Home        Home                // where the household lives
	Everywhere  []string            // merchants that are always everywhere merchants (Grab), as word prefixes of the merchant
	StayHome    []ledger.CategoryID // home categories never trip spending: bills and care paid while away
	HomePayees  []string            // payees whose charges are never trip spending: a merchant's that stays home
	Start       time.Time           // the attention start: trips ending before it are back-filled unflagged
	CashAccount string              // the account cash is withdrawn from
}

// Inferred is an inferred trip and the evidence it rests on. A possible trip rests on card
// evidence alone: it is never listed, only shown to the user.
type Inferred struct {
	Trip
	Evidence []Evidence
	Possible bool
}

// Trips are the inferred trips, possible trips aside.
func Trips(inferred []Inferred) []Trip {
	var out []Trip
	for _, t := range inferred {
		if !t.Possible {
			out = append(out, t.Trip)
		}
	}
	return out
}

// Infer finds the trips in the calendar's evidence and the card's, from the home start on;
// the card's counts from from too, so a place is new only once the history before it is read.
// Settled places and favourites are measured twice: against the listed trips, to find the
// trips the calendar's evidence alone makes, and then against those and the listed trips, to
// gather the card's evidence and infer the trips from all of it.
func Infer(txs []Transaction, calendar []Evidence, listed List, s Settings, from time.Time) []Inferred {
	calendar = slices.DeleteFunc(slices.Clone(calendar), func(e Evidence) bool { return e.Day.Before(s.Home.Since) })
	places := NewPlaces(txs, s)
	alone := assemble(calendar, places.Haunts(listed).homeDays())
	haunts := places.Haunts(append(slices.Clone(listed), Trips(alone)...))
	if s.Home.Since.After(from) {
		from = s.Home.Since
	}
	return assemble(append(calendar, haunts.Evidence(from)...), haunts.homeDays())
}

// run is evidence on days no more than evidenceGap apart, in order.
type run []Evidence

func (r run) first() time.Time { return r[0].Day }
func (r run) last() time.Time  { return r[len(r)-1].Day }

// has reports whether r holds evidence with one of signs.
func (r run) has(signs ...Sign) bool {
	return slices.ContainsFunc(r, func(e Evidence) bool { return slices.Contains(signs, e.Sign) })
}

// home reports whether r ends with a return home: nothing joins it.
func (r run) home() bool { return r[len(r)-1].Sign == FlightBack }

// away reports whether r is a trip: it has calendar evidence and isn't a day trip, a run on a
// single day without a stay, which is home.
func (r run) away() bool {
	return !r.dayTrip() && slices.ContainsFunc(r, func(e Evidence) bool { return e.Source == FromCalendar })
}

// possible reports whether r is a possible trip: no calendar evidence, but new or favourite
// places with ground transport on more than a day.
func (r run) possible() bool {
	return !r.dayTrip() && !r.away() && r.has(NewPlace, FavouritePlace) && r.has(GroundTransport)
}

func (r run) dayTrip() bool { return r.first().Equal(r.last()) && !r.has(Stay) }

// assemble finds the trips and possible trips in evidence. Evidence no more than evidenceGap days apart is one
// run; a flight leaving home starts a run and one returning ends it. Runs are joined across a
// gap with no home activity in it, never across a return home and never into a flight leaving
// home. A joined run that is away, or possibly away, is a trip or a possible trip from its
// first evidence day to its last, named for its first.
func assemble(evidence []Evidence, home []time.Time) []Inferred {
	evidence = slices.Clone(evidence)
	slices.SortFunc(evidence, func(a, b Evidence) int {
		return cmp.Or(a.Day.Compare(b.Day), cmp.Compare(rank(a.Sign), rank(b.Sign)), cmp.Compare(a.Source, b.Source), cmp.Compare(a.What, b.What), cmp.Compare(a.Sign, b.Sign))
	})
	var runs []run
	for _, e := range evidence {
		n := len(runs)
		if n == 0 || runs[n-1].home() || day.Between(runs[n-1].last(), e.Day) > evidenceGap || e.Sign == FlightOut && runs[n-1].last().Before(e.Day) {
			runs = append(runs, nil)
			n++
		}
		runs[n-1] = append(runs[n-1], e)
	}
	var joined []run
	for _, r := range runs {
		if n := len(joined); n > 0 && !joined[n-1].home() && r[0].Sign != FlightOut && !homeBetween(home, joined[n-1].last(), r.first()) {
			joined[n-1] = append(joined[n-1], r...)
			continue
		}
		joined = append(joined, r)
	}
	var out []Inferred
	for _, r := range joined {
		if r.away() || r.possible() {
			out = append(out, Inferred{Trip: Trip{Name: "trip-" + day.Format(r.first()), From: r.first(), To: r.last()}, Evidence: r, Possible: !r.away()})
		}
	}
	return out
}

// rank orders a day's evidence: a flight leaving home first and one returning last.
func rank(s Sign) int {
	switch s {
	case FlightOut:
		return 0
	case FlightBack:
		return 2
	}
	return 1
}

// homeBetween reports whether one of days falls strictly between a and b.
func homeBetween(days []time.Time, a, b time.Time) bool {
	return slices.ContainsFunc(days, func(d time.Time) bool { return d.After(a) && d.Before(b) })
}

// IsCash reports whether t is a withdrawal or an ATM fee rebate: money of kind Cash on the
// cash account.
func (s Settings) IsCash(t Transaction) bool {
	return s.CashAccount != "" && t.Account.Name == s.CashAccount && s.Kinds.Of(t.Home.Category, t.Details.Code) == Cash
}
