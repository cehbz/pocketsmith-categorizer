package trips

// Class is where a merchant is charged.
type Class int

const (
	HomeMerchant       Class = iota + 1 // at home and on trips someone was home for, or named for a home place
	EverywhereMerchant                  // at home and on trips nobody was home for: chains, online shops, Grab
	TripMerchant                        // on trips only
)

// Merchants classifies merchants by their charges' history against a trip list, in two
// passes. The first finds the trips someone was home for; in the second a merchant's charges
// on those trips count neither for nor against it, so the café the one at home keeps visiting
// stays a home merchant, and only its charges on trips nobody was home for count against it.
type Merchants struct {
	settings  Settings
	outside   map[string]bool // merchants charged outside every trip
	away      map[string]bool // merchants charged on a trip nobody was home for
	recurring map[int64]bool
}

// NewMerchants indexes the charges in txs against the user's trips; s names home and the
// everywhere merchants.
func NewMerchants(txs []Transaction, trips List, s Settings) *Merchants {
	return newMerchants(txs, trips, s, true)
}

// legMerchants indexes the charges in txs against legs. Only a merchant naming a home place
// shows someone home on a leg: the days between legs are outside every leg, so a chain charged
// at home and on one leg would otherwise be a home merchant there and split the trip.
func legMerchants(txs []Transaction, legs List, s Settings) *Merchants {
	return newMerchants(txs, legs, s, false)
}

// newMerchants indexes the charges in txs against trips. With soleTrip, a merchant charged
// outside every trip and on one trip shows someone home for that trip.
func newMerchants(txs []Transaction, trips List, s Settings, soleTrip bool) *Merchants {
	m := &Merchants{settings: s, outside: map[string]bool{}, away: map[string]bool{}, recurring: recurringCharges(txs)}
	var charges []Transaction
	on := map[string]map[string]bool{} // merchant → the trips it was charged on
	for _, t := range txs {
		if !t.Charge() {
			continue
		}
		name := t.Merchant()
		charges = append(charges, t)
		trip, ok := trips.At(t.Date)
		if !ok {
			m.outside[name] = true
			continue
		}
		if on[name] == nil {
			on[name] = map[string]bool{}
		}
		on[name][trip.Name] = true
	}
	attended := map[string]bool{} // trips someone was home for
	for _, t := range charges {
		if trip, ok := trips.At(t.Date); ok && m.showsHome(t, soleTrip && len(on[t.Merchant()]) == 1) {
			attended[trip.Name] = true
		}
	}
	for _, t := range charges {
		if trip, ok := trips.At(t.Date); ok && !attended[trip.Name] {
			m.away[t.Merchant()] = true
		}
	}
	return m
}

// showsHome reports whether t, a charge on a trip, shows someone was home for it: a
// non-recurring charge, not from an everywhere merchant, from a merchant naming a home place
// or, when only is set, from one charged outside every trip and on no trip but this. This is
// the whole fixed point of "a trip with a non-recurring charge from a home merchant is
// attended": a merchant not naming home is a home merchant only when every trip it was
// charged on is attended, so it can't make a further trip attended.
func (m *Merchants) showsHome(t Transaction, only bool) bool {
	return !everywhere(t, m.settings.Everywhere) && !m.Recurring(t) && (named(t, m.settings.Home) || m.outside[t.Merchant()] && only)
}

// Class is t's merchant's class: an everywhere merchant is one whatever its history; a
// merchant naming a home place is home; otherwise one charged outside every trip is home
// unless it was also charged on a trip nobody was home for, and one charged only on trips is
// a trip merchant.
func (m *Merchants) Class(t Transaction) Class {
	name := t.Merchant()
	switch {
	case everywhere(t, m.settings.Everywhere):
		return EverywhereMerchant
	case named(t, m.settings.Home) || m.outside[name] && !m.away[name]:
		return HomeMerchant
	case m.outside[name]:
		return EverywhereMerchant
	}
	return TripMerchant
}

// Recurring reports whether t is one of a monthly run of its merchant's charges.
func (m *Merchants) Recurring(t Transaction) bool { return m.recurring[t.ID] }

// HomeActivity reports whether t shows someone at home: a home merchant's charge that isn't
// recurring.
func (m *Merchants) HomeActivity(t Transaction) bool {
	return t.Charge() && m.Class(t) == HomeMerchant && !m.Recurring(t)
}
