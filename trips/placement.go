package trips

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/categorize"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

// Labels the trips layer sets besides trip names. Placed and Unsure are reasons a transaction
// needs the user; like attention's, they are the user's labels.
const (
	Label   = "auto-trip"             // the categorizer's trip decisions
	VisaRun = "visa-run"              // on a trip that is a visa run
	Booking = "trip-booking"          // a booking no trip has claimed
	Placed  = "attention-trip"        // put in a Big subcategory
	Unsure  = "attention-trip-unsure" // in doubt
)

const (
	sourcePrefix = "trips: "
	revertSource = "revert trips"
)

// IsSource reports whether source is a trips placement's.
func IsSource(source string) bool { return strings.HasPrefix(source, sourcePrefix) }

// SourceTrip is the trip a placement's source names: a trip's name, or Booking.
func SourceTrip(source string) (string, bool) {
	rest, ok := strings.CutPrefix(source, sourcePrefix)
	if !ok {
		return "", false
	}
	name, _, _ := strings.Cut(rest, " ")
	return name, true
}

// PlacedPrefix begins the key of every placement of transaction id.
func PlacedPrefix(id int64) string { return fmt.Sprintf("placed %d ", id) }

// Placer places a run's transactions on the user's trips.
type Placer struct {
	Settings  Settings
	List      List
	Written   []Trip                 // every trip written to the list, deleted ones included
	Seen      func(key string) bool  // the placements already made
	Origins   map[int64]ledger.State // each placed transaction's state before the trips layer changed it
	Corrected map[int64]bool         // transactions the user recategorized since the categorizer set them
	From      time.Time              // the placement start: transactions dated before it are history only
}

// Placing is what placing decided.
type Placing struct {
	Changes  []categorize.Change // Before as the transaction is, After as placed or reverted
	Reasons  map[int64][]string  // why placed transactions need the user
	Keys     map[int64]string    // the placement to remember once its write lands
	Versions []Trip              // listed trips not placed under their version before
}

// placing is a placement in progress.
type placing struct {
	Placer
	Placing
	haunts Haunts                 // the places against the list
	names  map[string]bool        // trips' names, deleted trips' included
	final  map[int64]ledger.State // each transaction's state once placed
}

// Place places each charge in a trip's dates and each booking, then each refund. A
// transaction the trips layer placed that no longer places returns to its origin.
// Transactions dated before the placement start are history only: placing reads them but
// leaves them as they are.
func (p Placer) Place(txs []Transaction) Placing {
	w := &placing{Placer: p, Placing: Placing{Reasons: map[int64][]string{}, Keys: map[int64]string{}},
		haunts: NewPlaces(txs, p.Settings).Haunts(p.List), names: map[string]bool{}, final: map[int64]ledger.State{}}
	for _, t := range append(slices.Clone([]Trip(p.List)), p.Written...) {
		w.names[t.Name] = true
	}
	homeActive := map[string]bool{}
	for _, t := range txs {
		w.final[t.ID] = t.Home
		if trip, ok := p.List.At(t.Date); ok && w.haunts.Home(t) {
			homeActive[trip.Name] = true
		}
	}
	for _, t := range txs {
		if p.Corrected[t.ID] || !t.Charge() {
			continue
		}
		if t.Date.Before(p.From) {
			w.hold(t)
			continue
		}
		trip, in := p.List.At(t.Date)
		switch {
		case in:
			w.charge(t, trip, homeActive[trip.Name])
		case w.claimed(t):
			w.set(t, t.State, "") // left as it is, whatever another layer decided
		case p.Settings.Kinds.Codes.IsBooking(t.Details.Code) && !named(t, p.Settings.Home):
			w.booking(t)
		default:
			w.revert(t)
		}
	}
	w.refunds(txs)
	for _, t := range p.List {
		if !p.Seen(t.VersionKey()) {
			w.Versions = append(w.Versions, t)
		}
	}
	return w.Placing
}

// charge places a charge in trip's dates. A home charge stays; an everywhere charge is in
// doubt while someone was home; any other is a trip charge, put in its kind's Big
// subcategory. A charge the trips layer doesn't own is placed once per version, so the
// user's later correction stands.
func (w *placing) charge(t Transaction, trip Trip, homeActive bool) {
	key := PlacedPrefix(t.ID) + trip.Version()
	first := !w.Seen(key)
	if !first && !t.State.HasLabel(Label) {
		return
	}
	flagged := first && !trip.To.Before(w.Settings.Start)
	switch class := w.haunts.Class(t); {
	case class == HomeCharge:
		w.revert(t)
		return
	case class == EverywhereCharge && homeActive:
		reverted := w.revert(t)
		w.Keys[t.ID] = key
		if (flagged || reverted) && !trip.To.Before(w.Settings.Start) {
			w.flag(t, Unsure)
		}
		return
	}
	labels := []string{Label, trip.Name}
	if trip.VisaRun {
		labels = append(labels, VisaRun)
	}
	w.put(t, w.Settings.Kinds.Big[w.Settings.Kinds.Of(t.Home.Category, t.Details.Code)], labels, trip.Name)
	w.Keys[t.ID] = key
	if flagged {
		w.flag(t, Placed)
	}
}

// booking places a charge with a travel or lodging code outside every trip in its kind's
// Big subcategory under Booking, until the user claims it for a trip.
func (w *placing) booking(t Transaction) {
	key := PlacedPrefix(t.ID) + Booking
	first := !w.Seen(key)
	if !first && !t.State.HasLabel(Label) {
		return
	}
	w.put(t, w.Settings.Kinds.Big[w.Settings.Kinds.Of(t.Home.Category, t.Details.Code)], []string{Label, Booking}, Booking)
	w.Keys[t.ID] = key
	if first && !t.Date.Before(w.Settings.Start) {
		w.flag(t, Placed)
	}
}

// claimed reports whether the user gave t a listed trip's label: a booking, or a charge the
// trips layer doesn't own.
func (w *placing) claimed(t Transaction) bool {
	return slices.ContainsFunc(t.State.Labels, w.List.Named) && (t.State.HasLabel(Booking) || !t.State.HasLabel(Label))
}

// refunds places each credit that follows a trip charge or a booking from its merchant: it
// takes the charge's category and trip labels as the charge stands. A refund not within 3%
// of its charge's amount is unsure. An ATM fee rebate follows its account's latest
// withdrawal instead, and is never unsure.
func (w *placing) refunds(txs []Transaction) {
	charges := map[string][]Transaction{}     // by merchant
	withdrawals := map[string][]Transaction{} // by account
	for _, t := range txs {
		if !t.Charge() {
			continue
		}
		charges[t.Merchant()] = append(charges[t.Merchant()], t)
		if w.Settings.IsCash(t) {
			withdrawals[t.Account.Name] = append(withdrawals[t.Account.Name], t)
		}
	}
	for _, cs := range charges {
		slices.SortFunc(cs, byDate)
	}
	for _, ws := range withdrawals {
		slices.SortFunc(ws, byDate)
	}
	for _, t := range txs {
		if w.Corrected[t.ID] || !t.Credit() {
			continue
		}
		if t.Date.Before(w.From) {
			w.hold(t)
			continue
		}
		c, within, ok := follows(t, charges[t.Merchant()])
		if w.Settings.IsCash(t) {
			c, ok = latest(t, withdrawals[t.Account.Name])
			within = true
		}
		trip, on := w.onTrip(w.final[c.ID])
		if !ok || !on {
			w.revert(t)
			continue
		}
		key := PlacedPrefix(t.ID) + fmt.Sprintf("refund %d", c.ID)
		first := !w.Seen(key)
		if !first && !t.State.HasLabel(Label) {
			continue
		}
		labels := []string{Label}
		for _, l := range w.final[c.ID].Labels {
			if w.List.Named(l) || l == Booking || l == VisaRun {
				labels = append(labels, l)
			}
		}
		w.put(t, w.final[c.ID].Category, labels, fmt.Sprintf("%s refund of %d", trip, c.ID))
		w.Keys[t.ID] = key
		switch {
		case !first:
		case !within:
			w.flag(t, Unsure)
		case !t.Date.Before(w.Settings.Start):
			w.flag(t, Placed)
		}
	}
}

// follows is the charge credit follows: its merchant's latest charge on or before it within
// 3% of its amount, else the latest; ok is false when there is none.
func follows(credit Transaction, charges []Transaction) (c Transaction, within, ok bool) {
	for i := len(charges) - 1; i >= 0; i-- {
		ch := charges[i]
		if ch.Date.After(credit.Date) {
			continue
		}
		if !ok {
			c, ok = ch, true
		}
		if near(credit.Amount, ch.Amount) {
			return ch, true, true
		}
	}
	return c, false, ok
}

// latest is the latest of charges on or before credit.
func latest(credit Transaction, charges []Transaction) (Transaction, bool) {
	for i := len(charges) - 1; i >= 0; i-- {
		if !charges[i].Date.After(credit.Date) {
			return charges[i], true
		}
	}
	return Transaction{}, false
}

// onTrip is the listed trip a state's labels put it on, or Booking for a booking.
func (w *placing) onTrip(s ledger.State) (string, bool) {
	for _, l := range s.Labels {
		if w.List.Named(l) {
			return l, true
		}
	}
	if s.HasLabel(Booking) {
		return Booking, true
	}
	return "", false
}

// put puts t in category with labels, keeping its review flag and its user's labels.
func (w *placing) put(t Transaction, category ledger.CategoryID, labels []string, trip string) {
	after := ledger.State{Category: category, NeedsReview: t.State.NeedsReview, Labels: append(w.strip(t.Home.UserLabels()), labels...)}
	w.set(t, after, sourcePrefix+trip)
}

// revert returns a transaction the trips layer placed to its origin; without one, it drops
// the trips layer's labels.
func (w *placing) revert(t Transaction) bool {
	if !t.State.HasLabel(Label) {
		return false
	}
	origin, ok := w.Origins[t.ID]
	if !ok {
		origin = ledger.State{Category: t.State.Category}
	}
	after := ledger.State{Category: origin.Category, NeedsReview: origin.NeedsReview || t.State.NeedsReview, Labels: slices.Clone(origin.Labels)}
	for _, l := range w.strip(t.State.UserLabels()) {
		if l != Placed && l != Unsure && !after.HasLabel(l) {
			after.Labels = append(after.Labels, l)
		}
	}
	w.set(t, after, revertSource)
	return true
}

// hold leaves a transaction the trips layer placed as it is, whatever another layer decided.
func (w *placing) hold(t Transaction) {
	if t.State.HasLabel(Label) {
		w.set(t, t.State, "")
	}
}

func (w *placing) set(t Transaction, after ledger.State, source string) {
	w.final[t.ID] = after
	w.Changes = append(w.Changes, categorize.Change{Charge: t.ID, Before: t.State, After: after, Source: source})
}

func (w *placing) flag(t Transaction, reason string) {
	w.Reasons[t.ID] = append(w.Reasons[t.ID], reason)
}

// strip drops the trips layer's labels: Label, trip names, VisaRun and Booking.
func (w *placing) strip(labels []string) []string {
	var out []string
	for _, l := range labels {
		if l != Label && l != VisaRun && l != Booking && !w.names[l] {
			out = append(out, l)
		}
	}
	return out
}
