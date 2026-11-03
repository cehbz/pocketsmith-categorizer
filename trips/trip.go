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

// Add appends the inferred trips that overlap no trip in l and none ever written, and
// returns the list and the trips it added.
func (l List) Add(inferred, written []Trip) (List, []Trip) {
	out := slices.Clone(l)
	var added []Trip
	for _, t := range inferred {
		clash := func(o Trip) bool { return o.Overlaps(t) }
		if slices.ContainsFunc(out, clash) || slices.ContainsFunc(written, clash) {
			continue
		}
		out = append(out, t)
		added = append(added, t)
	}
	return out, added
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
