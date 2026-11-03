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
// cash account's withdrawals and rebates, each with its card evidence and the state the plan
// leaves it in.
func tripTransactions(txs []ledger.Transaction, recs []trips.Record, plan decide.Plan, cards []string, s trips.Settings) []trips.Transaction {
	home := map[int64]ledger.State{}
	for _, c := range plan.Changes {
		home[c.Charge] = c.After
	}
	for _, c := range plan.Corrections {
		home[c.Charge] = c.After
	}
	evidence := trips.Match(txs, recs) // only the records' accounts match
	var out []trips.Transaction
	for _, t := range txs {
		h, ok := home[t.ID]
		if !ok {
			h = t.State
		}
		tt := trips.Transaction{Transaction: t, Evidence: evidence[t.ID], Home: h}
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

// dropTrips removes the named trips from the list and from the record of trips written.
func dropTrips(store trips.Store, names []string) error {
	if len(names) == 0 {
		return nil
	}
	list, written, err := store.Load()
	if err != nil {
		return err
	}
	return store.Save(trips.Without(list, names), trips.Without(written, names))
}

// inferTrips reads the card's records for txs, the ledger read from from, and returns the
// transactions the trips layer sees once plan is applied and the trips inference finds in
// them. Inference has no warm-up of its own: it counts merchants as new from inferWarmup
// days into the read.
func inferTrips(ctx context.Context, s *session, txs []ledger.Transaction, plan decide.Plan, from time.Time) ([]trips.Transaction, []trips.Trip, error) {
	recs, err := s.card.Records(ctx, from.AddDate(0, 0, -recordLead), s.today)
	if err != nil {
		return nil, nil, err
	}
	card := tripTransactions(txs, recs, plan, s.accounts, s.trips)
	return card, trips.Infer(card, s.trips, from.AddDate(0, 0, inferWarmup)), nil
}

// listTrips lists the trips a run added once every write of the run landed: it names them in
// the run's journal first, so undo can remove every trip listed, then writes the list and
// the record of trips written. It returns the trips listed and the run's error.
func listTrips(applyErr error, store trips.Store, j journal.Store, runID string, list trips.List, written, added []trips.Trip) ([]trips.Trip, error) {
	if applyErr != nil || len(added) == 0 {
		return nil, applyErr
	}
	if err := j.SaveTrips(runID, trips.Names(added)); err != nil {
		return nil, err
	}
	if err := store.Save(list, append(slices.Clone(written), added...)); err != nil {
		return nil, err
	}
	return added, nil
}
