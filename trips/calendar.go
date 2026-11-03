package trips

import (
	"context"
	"regexp"
	"time"
)

// Event is an event in the user's calendar, on its local dates.
type Event struct {
	Title    string
	Location string
	Start    time.Time // its first local date, midnight UTC
	End      time.Time // its last local date; for an all-day event, the day after its last, as calendars keep it
	AllDay   bool
}

// Calendar is the user's calendar.
type Calendar interface {
	Events(ctx context.Context, from, to time.Time) ([]Event, error)
}

var (
	route    = regexp.MustCompile(`\b([A-Z]{3})\s*-\s*([A-Z]{3})\b`) // HMT-FAR ZZ103, ZZ 155 HMT-FAR, FAR - HMT QQ134
	flightTo = regexp.MustCompile(`(?i)^flight to\b`)                // Gmail's flight, without its airports
	stayAt   = regexp.MustCompile(`(?i)^stay at\b`)                  // Gmail's stay
)

// LastDay is the last local date e covers.
func (e Event) LastDay() time.Time {
	if e.AllDay && e.End.After(e.Start) {
		return e.End.AddDate(0, 0, -1)
	}
	return e.End
}

// Evidence is the travel evidence e is. A flight, an event whose title carries a route of
// airport codes, leaves home on its departure date when it departs a home airport, returns
// on its arrival date when it arrives at one, and is away on its departure date otherwise;
// so is a flight Gmail titled `Flight to ...`. A stay, titled `Stay at ...`, whose location
// doesn't name home, is away on each day from check-in to check-out. Any other event is no
// evidence.
func (e Event) Evidence(h Home) []Evidence {
	sign := func(d time.Time, s Sign) Evidence {
		return Evidence{Day: d, Source: FromCalendar, Sign: s, What: e.Title}
	}
	if m := route.FindStringSubmatch(e.Title); m != nil {
		switch {
		case h.Airport(m[1]):
			return []Evidence{sign(e.Start, FlightOut)}
		case h.Airport(m[2]):
			return []Evidence{sign(e.LastDay(), FlightBack)}
		}
		return []Evidence{sign(e.Start, Flight)}
	}
	if flightTo.MatchString(e.Title) {
		return []Evidence{sign(e.Start, Flight)}
	}
	if !stayAt.MatchString(e.Title) || h.Names(e.Location) {
		return nil
	}
	var out []Evidence
	for d := e.Start; !d.After(e.LastDay()); d = d.AddDate(0, 0, 1) {
		out = append(out, sign(d, Stay))
	}
	return out
}
