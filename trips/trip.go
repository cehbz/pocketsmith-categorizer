package trips

import (
	"fmt"
	"slices"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/internal/day"
)

// Trip is a stretch of days away from home.
type Trip struct {
	Name    string    // its label
	From    time.Time // the first day away
	To      time.Time // the last day away
	VisaRun bool
}

// Contains reports whether d falls in the trip's dates.
func (t Trip) Contains(d time.Time) bool { return !d.Before(t.From) && !d.After(t.To) }

// Overlaps reports whether t and o share a day.
func (t Trip) Overlaps(o Trip) bool { return !t.To.Before(o.From) && !o.To.Before(t.From) }

// Version is the trip's name and dates: each charge in the trip is placed once per version.
func (t Trip) Version() string {
	return fmt.Sprintf("%s %s..%s", t.Name, day.Format(t.From), day.Format(t.To))
}

// VersionKey remembers that the trip's version was placed.
func (t Trip) VersionKey() string { return "version " + t.Version() }

// List is the user's trip list.
type List []Trip

// At is the first trip in l whose dates hold d.
func (l List) At(d time.Time) (Trip, bool) {
	for _, t := range l {
		if t.Contains(d) {
			return t, true
		}
	}
	return Trip{}, false
}

// Named reports whether a trip in l is called name.
func (l List) Named(name string) bool {
	return slices.ContainsFunc(l, func(t Trip) bool { return t.Name == name })
}

// Same reports whether t and o are the same trip: name, dates and visa run.
func (t Trip) Same(o Trip) bool {
	return t.Name == o.Name && t.From.Equal(o.From) && t.To.Equal(o.To) && t.VisaRun == o.VisaRun
}

// Extension is a listed trip a run extended: as it was, and as extended.
type Extension struct{ Before, After Trip }

// Listing is what a run changed in the trip list: the trips it added and the trips it
// extended.
type Listing struct {
	Added    []Trip
	Extended []Extension
}

// Empty reports whether the run changed nothing in the list.
func (c Listing) Empty() bool { return len(c.Added) == 0 && len(c.Extended) == 0 }

// Update brings the inferred trips into l and returns the list and what changed. An inferred
// trip starting on a listed trip's first day and ending later extends that trip, when the
// user hasn't edited it (it is as written) and the longer trip overlaps no other listed or
// written trip: a trip in progress grows run by run. Any other inferred trip overlapping no
// trip in l and none ever written is appended.
func (l List) Update(inferred, written []Trip) (List, Listing) {
	out := slices.Clone(l)
	var c Listing
	for _, t := range inferred {
		if i := slices.IndexFunc(out, func(o Trip) bool {
			return o.From.Equal(t.From) && t.To.After(o.To) && slices.ContainsFunc(written, o.Same)
		}); i >= 0 {
			longer := out[i]
			longer.To = t.To
			other := func(o Trip) bool { return !o.Same(out[i]) && o.Overlaps(longer) }
			if !slices.ContainsFunc(out, other) && !slices.ContainsFunc(written, other) {
				c.Extended = append(c.Extended, Extension{Before: out[i], After: longer})
				out[i] = longer
			}
			continue
		}
		clash := func(o Trip) bool { return o.Overlaps(t) }
		if slices.ContainsFunc(out, clash) || slices.ContainsFunc(written, clash) {
			continue
		}
		out = append(out, t)
		c.Added = append(c.Added, t)
	}
	return out, c
}

// Hint is an inferred trip that overlaps a listed trip the user edited and reaches beyond it.
type Hint struct {
	Edited   Trip
	Inferred Inferred
}

// Hints are the inferred trips that overlap a listed trip the user edited or listed by hand,
// one not as written, and reach beyond its dates. The list is left as it is: the user decides.
func (l List) Hints(inferred []Inferred, written []Trip) []Hint {
	var out []Hint
	for _, t := range inferred {
		for _, o := range l {
			if o.Overlaps(t.Trip) && (t.From.Before(o.From) || t.To.After(o.To)) && !slices.ContainsFunc(written, o.Same) {
				out = append(out, Hint{Edited: o, Inferred: t})
			}
		}
	}
	return out
}

// Record is the record of trips written once the list is written with c: extended trips as
// extended, added trips appended.
func (c Listing) Record(written []Trip) []Trip {
	out := slices.Clone(written)
	for _, e := range c.Extended {
		replace(out, e.Before, e.After)
	}
	return append(out, c.Added...)
}

// Undo reverses c in the list and the record of trips written: the trips it added are
// removed, and the trips it extended go back to their dates where they are still as it left
// them.
func (c Listing) Undo(list List, written []Trip) (List, []Trip) {
	names := Names(c.Added)
	list, written = Without(list, names), Without(written, names)
	for _, e := range c.Extended {
		replace(list, e.After, e.Before)
		replace(written, e.After, e.Before)
	}
	return list, written
}

// replace puts to in place of each trip in ts that is the same as from.
func replace(ts []Trip, from, to Trip) {
	for i, t := range ts {
		if t.Same(from) {
			ts[i] = to
		}
	}
}

// Without is ts less the trips called one of names.
func Without(ts []Trip, names []string) []Trip {
	return slices.DeleteFunc(slices.Clone(ts), func(t Trip) bool { return slices.Contains(names, t.Name) })
}

// Names are the trips' names.
func Names(ts []Trip) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Name
	}
	return out
}

// Store keeps the trip list and the record of every trip written to it.
type Store interface {
	Load() (List, []Trip, error)
	Save(list List, written []Trip) error
}
