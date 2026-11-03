package trips

import "time"

// Trip is a stretch of days away from home.
type Trip struct {
	Name    string    // its label
	From    time.Time // the first day away
	To      time.Time // the last day away
	VisaRun bool
}

// Contains reports whether d falls in the trip's dates.
func (t Trip) Contains(d time.Time) bool { return !d.Before(t.From) && !d.After(t.To) }

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
