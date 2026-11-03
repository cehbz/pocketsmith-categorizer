package trips

import (
	"slices"
	"strings"
	"time"
)

// Home is where the household lives: the names of its places, its airports and the day it
// started there. Before the home start nothing is settled.
type Home struct {
	Places   []string  // as payees, merchant names and event locations name them
	Airports []string  // IATA codes
	Since    time.Time // the home start
}

// Names reports whether text names a home place, in any case.
func (h Home) Names(text string) bool {
	text = strings.ToUpper(text)
	return slices.ContainsFunc(h.Places, func(place string) bool { return strings.Contains(text, strings.ToUpper(place)) })
}

// Airport reports whether code is one of home's airports.
func (h Home) Airport(code string) bool {
	return slices.ContainsFunc(h.Airports, func(a string) bool { return strings.EqualFold(a, code) })
}
