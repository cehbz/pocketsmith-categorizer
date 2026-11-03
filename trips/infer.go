package trips

import (
	"maps"
	"slices"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/internal/day"
)

const legGap = 2 // most days between a leg's signal days

// Settings are what inference and placing need besides the transactions.
type Settings struct {
	Kinds       Kinds
	Places      []string  // home places, as named in merchant names and payees
	Start       time.Time // the attention start: trips ending before it are back-filled unflagged
	CashAccount string    // the account cash is withdrawn from
}

// Leg is a run of trip-signal days.
type Leg struct {
	From, To time.Time
	Lodging  bool // it has an in-store lodging charge
}

// DayTrip reports whether the leg is a day out from home: no night away and no lodging.
func (l Leg) DayTrip() bool { return l.From.Equal(l.To) && !l.Lodging }

// Infer finds the trips in txs from signal days on or after from: the legs that aren't day
// trips, consecutive ones joined when no home activity falls between them. A trip is named
// for its first day.
func Infer(txs []Transaction, s Settings, from time.Time) []Trip {
	var away List
	for _, l := range Legs(txs, s, from) {
		if !l.DayTrip() {
			away = append(away, Trip{Name: "trip-" + day.Format(l.From), From: l.From, To: l.To})
		}
	}
	m := NewMerchants(txs, away, s.Places)
	var out []Trip
	for _, t := range away {
		if n := len(out); n > 0 && !homeBetween(txs, m, out[n-1].To, t.From) {
			out[n-1].To = t.To
			continue
		}
		out = append(out, t)
	}
	return out
}

// Legs are the runs of signal days on or after from, no more than legGap days apart. A
// signal day has an in-store charge from a merchant never charged before that day and not
// named for a home place, or an in-store lodging charge.
func Legs(txs []Transaction, s Settings, from time.Time) []Leg {
	first := map[string]time.Time{}
	for _, t := range txs {
		if f, ok := first[t.Merchant()]; t.Charge() && (!ok || t.Date.Before(f)) {
			first[t.Merchant()] = t.Date
		}
	}
	signal := map[time.Time]bool{} // signal day → it has lodging
	for _, t := range txs {
		if !t.Charge() || !t.Evidence.InStore || t.Date.Before(from) {
			continue
		}
		lodging := s.Kinds.Of(t.Home.Category, t.Evidence.Code) == Lodging
		if lodging || first[t.Merchant()].Equal(t.Date) && !named(t, s.Places) {
			signal[t.Date] = signal[t.Date] || lodging
		}
	}
	var legs []Leg
	for _, d := range slices.SortedFunc(maps.Keys(signal), time.Time.Compare) {
		if n := len(legs); n > 0 && day.Between(legs[n-1].To, d) <= legGap {
			legs[n-1].To = d
			legs[n-1].Lodging = legs[n-1].Lodging || signal[d]
			continue
		}
		legs = append(legs, Leg{From: d, To: d, Lodging: signal[d]})
	}
	return legs
}

// homeBetween reports whether home activity falls strictly between a and b.
func homeBetween(txs []Transaction, m *Merchants, a, b time.Time) bool {
	return slices.ContainsFunc(txs, func(t Transaction) bool { return t.Date.After(a) && t.Date.Before(b) && m.HomeActivity(t) })
}

// IsCash reports whether t is a withdrawal or an ATM fee rebate: money of kind Cash on the
// cash account.
func (s Settings) IsCash(t Transaction) bool {
	return s.CashAccount != "" && t.Account.Name == s.CashAccount && s.Kinds.Of(t.Home.Category, t.Evidence.Code) == Cash
}
