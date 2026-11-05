package report

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cehbz/pocketsmith-categorizer/attention"
	"github.com/cehbz/pocketsmith-categorizer/evidence"
	"github.com/cehbz/pocketsmith-categorizer/internal/day"
	"github.com/cehbz/pocketsmith-categorizer/trips"
)

// phrases are the attention labels in plain words.
var phrases = map[string]string{
	attention.NewPayee:       "new payee",
	attention.Large:          "larger than usual",
	attention.Duplicate:      "possible duplicate",
	attention.Undecided:      "no category decided",
	attention.MonthlyIncome:  "monthly income",
	attention.IncomeMismatch: "income off the estimate",
	trips.Placed:             "placed on a trip",
	trips.Unsure:             "trip placement in doubt",
}

// phrase is an attention label in plain words; an unknown one is shown without its prefix.
func phrase(label string) string {
	if p, ok := phrases[label]; ok {
		return p
	}
	if p, ok := evidence.Phrase(label); ok {
		return p
	}
	return strings.TrimPrefix(label, "attention-")
}

// date is d as the report shows it, its year added when it isn't year.
func date(d time.Time, year int) string {
	if d.Year() == year {
		return d.Format("Jan 2")
	}
	return d.Format("Jan 2, 2006")
}

// amount is v with its sign, a minus sign for a charge, and thousands separators.
func amount(v float64) string {
	s := money(v)
	switch cents := math.Round(v * 100); {
	case cents < 0:
		return "−" + s
	case cents > 0:
		return "+" + s
	}
	return s
}

// money is v unsigned, with thousands separators.
func money(v float64) string {
	s := strconv.FormatFloat(math.Abs(v), 'f', 2, 64)
	whole, cents, _ := strings.Cut(s, ".")
	var b strings.Builder
	for i, d := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(d)
	}
	return b.String() + "." + cents
}

// dated is a trip name's date prefix: the year and month a trip began.
var dated = regexp.MustCompile(`^\d{4}-\d{2}-`)

// tripName is how the report names a trip: its name without trip- and a date prefix,
// title-cased, as "Lakeside trip". A booking no trip claimed is a trip booking, and a name
// that is only a date stays as it is.
func tripName(name string) string {
	if name == trips.Booking {
		return "trip booking"
	}
	rest := strings.TrimPrefix(name, "trip-")
	if _, err := day.Parse(rest); err == nil {
		return name
	}
	words := strings.Split(dated.ReplaceAllString(rest, ""), "-")
	for i, w := range words {
		_, n := utf8.DecodeRuneInString(w)
		words[i] = strings.ToUpper(w[:n]) + w[n:]
	}
	return strings.Join(words, " ") + " trip"
}

// plural is n of a thing, named one or many.
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
