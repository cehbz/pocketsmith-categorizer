package trips

import (
	"slices"
	"strings"
	"time"
)

// Home is where the household lives: the names of its places and the day it started there.
// Before the home start nothing is settled.
type Home struct {
	Places []string  // as payees and merchant names name them
	Since  time.Time // the home start
}

// Names reports whether text names a home place, in any case.
func (h Home) Names(text string) bool {
	text = strings.ToUpper(text)
	return slices.ContainsFunc(h.Places, func(place string) bool { return strings.Contains(text, strings.ToUpper(place)) })
}
