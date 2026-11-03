package trips

import (
	"strconv"
	"time"
)

// Source is where a piece of travel evidence came from.
type Source int

const (
	FromCalendar Source = iota + 1
	FromCard
)

func (s Source) String() string {
	switch s {
	case FromCalendar:
		return "calendar"
	case FromCard:
		return "card"
	}
	return "source " + strconv.Itoa(int(s))
}

// Sign is what a piece of travel evidence shows.
type Sign int

const (
	FlightOut       Sign = iota + 1 // a flight leaving home
	FlightBack                      // a flight returning home
	Flight                          // any other flight
	Stay                            // a day of a stay away
	Lodged                          // lodging at a place that isn't settled and doesn't name home
	GroundTransport                 // ground transport at a place
	NewPlace                        // a charge at a place never charged before
	FavouritePlace                  // a charge at a favourite
)

func (s Sign) String() string {
	switch s {
	case FlightOut:
		return "flight out"
	case FlightBack:
		return "flight back"
	case Flight:
		return "flight"
	case Stay:
		return "stay"
	case Lodged:
		return "lodging"
	case GroundTransport:
		return "ground transport"
	case NewPlace:
		return "new place"
	case FavouritePlace:
		return "favourite place"
	}
	return "sign " + strconv.Itoa(int(s))
}

// Evidence is a dated sign of being away, with its source.
type Evidence struct {
	Day    time.Time
	Source Source
	Sign   Sign
	What   string // the calendar event's title or the card charge's place
}

// The card's travel evidence codes.
var (
	lodgingCodes   = []Range{{3501, 3999}, {7011, 7011}}
	transportCodes = []Range{{4111, 4112}, {4121, 4121}, {4131, 4131}, {4789, 4789}}
)

// Evidence is the card's travel evidence in the place charges dated on or after from:
// lodging at a place that isn't settled and doesn't name home; ground transport at a place;
// and a charge at a place that isn't settled and doesn't name home, on the day it was first
// charged or when it is a favourite.
func (h Haunts) Evidence(from time.Time) []Evidence {
	var out []Evidence
	for pl, cs := range h.places.charges {
		for _, t := range cs {
			if t.Date.Before(from) {
				continue
			}
			code, _ := strconv.Atoi(t.Details.Code)
			away := !h.settled[pl] && !named(t, h.places.home)
			add := func(s Sign) { out = append(out, Evidence{Day: t.Date, Source: FromCard, Sign: s, What: string(pl)}) }
			if away && contains(lodgingCodes, code) {
				add(Lodged)
			}
			if contains(transportCodes, code) {
				add(GroundTransport)
			}
			switch {
			case away && h.favourite[pl]:
				add(FavouritePlace)
			case away && h.places.first[pl].Equal(t.Date):
				add(NewPlace)
			}
		}
	}
	return out
}

// homeDays are the days of home activity.
func (h Haunts) homeDays() []time.Time {
	var out []time.Time
	for _, cs := range h.places.charges {
		for _, t := range cs {
			if h.Home(t) {
				out = append(out, t.Date)
			}
		}
	}
	return out
}
