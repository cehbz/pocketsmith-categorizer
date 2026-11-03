package main

import (
	"context"
	"slices"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/categorize"
	"github.com/cehbz/pocketsmith-categorizer/decide"
	"github.com/cehbz/pocketsmith-categorizer/journal"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
	"github.com/cehbz/pocketsmith-categorizer/memo"
	"github.com/cehbz/pocketsmith-categorizer/trips"
)

const (
	inferWarmup = 60 // days of history before inference counts a merchant as new
	recordLead  = 2  // days before a ledger window a card record of it may be dated
)

// tripTransactions gives the trips layer the transactions on the card's accounts and the
// cash account's withdrawals and rebates, each with its card details and the state the plan
// leaves it in.
func tripTransactions(txs []ledger.Transaction, recs []trips.Record, plan decide.Plan, cards []string, s trips.Settings) []trips.Transaction {
	home := map[int64]ledger.State{}
	for _, c := range plan.Changes {
		home[c.Charge] = c.After
	}
	for _, c := range plan.Corrections {
		home[c.Charge] = c.After
	}
	details := trips.Match(txs, recs) // only the records' accounts match
	var out []trips.Transaction
	for _, t := range txs {
		h, ok := home[t.ID]
		if !ok {
			h = t.State
		}
		tt := trips.Transaction{Transaction: t, Details: details[t.ID], Home: h}
		if slices.Contains(cards, t.Account.Name) || s.IsCash(tt) {
			out = append(out, tt)
		}
	}
	return out
}

// corrected is the set of transactions the plan corrects.
func corrected(plan decide.Plan) map[int64]bool {
	out := map[int64]bool{}
	for _, c := range plan.Corrections {
		out[c.Charge] = true
	}
	return out
}

// rememberPlacements records the placements whose writes landed and, once every write
// landed, the versions of the trips placed.
func rememberPlacements(placed *memo.Memo, pl trips.Placing, unwritten []categorize.Change) {
	failed := map[int64]bool{}
	for _, c := range unwritten {
		failed[c.Charge] = true
	}
	for id, key := range pl.Keys {
		if !failed[id] {
			placed.Add(key)
		}
	}
	if len(unwritten) == 0 {
		for _, t := range pl.Versions {
			placed.Add(t.VersionKey())
		}
	}
}

// forgetPlacements drops the reverted transactions' placements, so the next run places them
// again.
func forgetPlacements(placed *memo.Memo, reverted []ledger.Charge) {
	for _, c := range reverted {
		placed.ForgetPrefix(trips.PlacedPrefix(c.ID))
	}
}

// undoListing reverses what a run changed in the trip list, in the list and the record of
// trips written.
func undoListing(store trips.Store, c trips.Listing) error {
	if c.Empty() {
		return nil
	}
	list, written, err := store.Load()
	if err != nil {
		return err
	}
	list, written = c.Undo(list, written)
	return store.Save(list, written)
}

// inferTrips reads the card's records for txs, the ledger read from from, and returns the
// transactions the trips layer sees once plan is applied and the trips inference finds in
// them against the listed trips. Inference has no warm-up of its own: it counts places as new
// from inferWarmup days into the read.
func inferTrips(ctx context.Context, s *session, txs []ledger.Transaction, plan decide.Plan, from time.Time, listed trips.List) ([]trips.Transaction, []trips.Inferred, error) {
	recs, err := s.card.Records(ctx, from.AddDate(0, 0, -recordLead), s.today)
	if err != nil {
		return nil, nil, err
	}
	card := tripTransactions(txs, recs, plan, s.accounts, s.trips)
	return card, trips.Infer(card, nil, listed, s.trips, from.AddDate(0, 0, inferWarmup)), nil
}

// listTrips writes what a run changed in the trip list once every write of the run landed:
// it records the change in the run's journal first, so undo can reverse every change listed,
// then writes the list and the record of trips written. It returns what it listed and the
// run's error.
func listTrips(applyErr error, store trips.Store, j journal.Store, runID string, list trips.List, written []trips.Trip, c trips.Listing) (trips.Listing, error) {
	if applyErr != nil || c.Empty() {
		return trips.Listing{}, applyErr
	}
	if err := j.SaveTrips(runID, c); err != nil {
		return trips.Listing{}, err
	}
	if err := store.Save(list, c.Record(written)); err != nil {
		return trips.Listing{}, err
	}
	return c, nil
}
