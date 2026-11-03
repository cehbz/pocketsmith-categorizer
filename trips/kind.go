package trips

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

// Kind is what a trip's spending went on; each kind has its Big subcategory.
type Kind int

const (
	Transport Kind = iota + 1
	Lodging
	Food
	Discretionary
	Cash
)

// Range is an inclusive range of merchant category codes.
type Range struct{ Lo, Hi int }

// ParseRanges reads codes and ranges such as "4111" and "3000-3299".
func ParseRanges(specs []string) ([]Range, error) {
	out := make([]Range, len(specs))
	for i, s := range specs {
		lo, hi, isRange := strings.Cut(s, "-")
		if !isRange {
			hi = lo
		}
		a, errLo := strconv.Atoi(strings.TrimSpace(lo))
		b, errHi := strconv.Atoi(strings.TrimSpace(hi))
		if errLo != nil || errHi != nil || a > b {
			return nil, fmt.Errorf("merchant codes %q: want a code or lo-hi", s)
		}
		out[i] = Range{a, b}
	}
	return out, nil
}

// Codes gives merchant category codes their kinds and names the codes of bookings.
type Codes struct {
	Transport, Lodging, Food []Range
	Booking                  []Range // long-distance travel and lodging, booked ahead
}

// DefaultCodes are the merchant category codes' usual kinds.
func DefaultCodes() Codes {
	return Codes{
		Transport: []Range{{3000, 3299}, {4111, 4111}, {4112, 4112}, {4121, 4121}, {4131, 4131}, {4411, 4411}, {4511, 4511}},
		Lodging:   []Range{{3501, 3999}, {4722, 4722}, {6513, 6513}, {7011, 7011}},
		Food:      []Range{{5411, 5411}, {5422, 5422}, {5441, 5441}, {5451, 5451}, {5462, 5462}, {5499, 5499}, {5811, 5814}},
		Booking:   []Range{{3000, 3299}, {3501, 3999}, {4112, 4112}, {4411, 4411}, {4511, 4511}, {4722, 4722}, {6513, 6513}, {7011, 7011}},
	}
}

// Kind is code's kind, Discretionary outside every range; ok is false when there is no code.
func (c Codes) Kind(code string) (k Kind, ok bool) {
	n, err := strconv.Atoi(code)
	if err != nil {
		return 0, false
	}
	switch {
	case contains(c.Transport, n):
		return Transport, true
	case contains(c.Lodging, n):
		return Lodging, true
	case contains(c.Food, n):
		return Food, true
	}
	return Discretionary, true
}

// IsBooking reports whether code is a long-distance travel or lodging code.
func (c Codes) IsBooking(code string) bool {
	n, err := strconv.Atoi(code)
	return err == nil && contains(c.Booking, n)
}

func contains(rs []Range, n int) bool {
	return slices.ContainsFunc(rs, func(r Range) bool { return r.Lo <= n && n <= r.Hi })
}

// Kinds decides a transaction's kind.
type Kinds struct {
	Codes Codes
	Big   map[Kind]ledger.CategoryID // each kind's Big subcategory
	Home  map[ledger.CategoryID]Kind // home categories with a kind, such as Eating Out's Food
}

// Of is the kind of a transaction in category with merchant code: its Big subcategory's,
// else its code's, else its home category's, else Discretionary.
func (k Kinds) Of(category ledger.CategoryID, code string) Kind {
	if kind, ok := k.BigKind(category); ok {
		return kind
	}
	if kind, ok := k.Codes.Kind(code); ok {
		return kind
	}
	if kind, ok := k.Home[category]; ok {
		return kind
	}
	return Discretionary
}

// BigKind is the kind whose Big subcategory category is.
func (k Kinds) BigKind(category ledger.CategoryID) (Kind, bool) {
	for kind, id := range k.Big {
		if id != 0 && id == category {
			return kind, true
		}
	}
	return 0, false
}
