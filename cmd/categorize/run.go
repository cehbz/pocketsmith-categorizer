package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/categorize"
	"github.com/cehbz/pocketsmith-categorizer/config"
	"github.com/cehbz/pocketsmith-categorizer/decide"
	"github.com/cehbz/pocketsmith-categorizer/evidence"
	"github.com/cehbz/pocketsmith-categorizer/internal/day"
	"github.com/cehbz/pocketsmith-categorizer/journal"
	"github.com/cehbz/pocketsmith-categorizer/learn"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
	"github.com/cehbz/pocketsmith-categorizer/memo"
	"github.com/cehbz/pocketsmith-categorizer/report"
	"github.com/cehbz/pocketsmith-categorizer/tripfile"
	"github.com/cehbz/pocketsmith-categorizer/trips"
)

const (
	precision    = 0.95 // proposals' target precision
	maxEvidence  = 10   // largest evidence count the backtest tries
	minEvaluated = 20   // fewest later categorizations a backtest trial needs
	backtestDays = 180  // the backtest scores the last this many days of history
)

func runCmd(ctx context.Context, args []string, out io.Writer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	configFile, journalDir := config.Paths(home)
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	configPath := fs.String("config", configFile, "config file")
	rulesPath := fs.String("rules", config.RulesPath(home), "rules file")
	lookback := fs.Int("lookback", 365, "days back the categorizer re-decides its own work")
	history := fs.Int("history", 730, "days of history read for suggestions and proposals")
	notify := fs.Bool("notify", false, "save the report and email the inbox when the run needs you or fails")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("run: unexpected argument %q", fs.Arg(0))
	}
	if !*notify {
		return scheduled(ctx, home, *configPath, *rulesPath, journalDir, *lookback, *history, nil, out)
	}
	n, err := openNotifier(ctx, *configPath, home)
	if err != nil {
		return err
	}
	return notified(ctx, n, scheduled(ctx, home, *configPath, *rulesPath, journalDir, *lookback, *history, n, out))
}

// scheduled opens the session and the run's memos and runs, delivering the report through n
// when it isn't nil.
func scheduled(ctx context.Context, home, configPath, rulesPath, journalDir string, lookback, history int, n *notifier, out io.Writer) error {
	s, err := open(ctx, configPath, rulesPath)
	if err != nil {
		return err
	}
	r := runner{journal: journal.Store{Dir: journalDir}, trips: tripfile.File{List: config.TripsPath(home), Written: config.WrittenTripsPath(home)},
		lookback: lookback, history: history, notify: n}
	for _, m := range []struct {
		dst  **memo.Memo
		path string
	}{{&r.shown, config.MemoPath(home)}, {&r.judged, config.JudgedPath(home)}, {&r.placed, config.PlacedPath(home)}} {
		if *m.dst, err = memo.Load(m.path); err != nil {
			return err
		}
	}
	if r.evidence, err = evidence.Load(config.EvidencePath(home)); err != nil {
		return err
	}
	return r.run(ctx, s, out)
}

// runner is what a run keeps between runs: what it showed, judged and placed, the evidence
// read from mail, its journal and the trip list.
type runner struct {
	shown, judged, placed *memo.Memo
	evidence              *evidence.Store
	journal               journal.Store
	trips                 trips.Store
	lookback, history     int       // days back the categorizer re-decides its own work, and of history read
	notify                *notifier // with notices on, delivers the report
}

// run decides, places, flags and applies a run's changes, lists the trips it changed, and
// reports.
func (r runner) run(ctx context.Context, s *session, out io.Writer) error {
	started := time.Now()
	shown, judged, placed, store, tripStore := r.shown, r.judged, r.placed, r.journal, r.trips
	last, err := store.LastSet()
	if err != nil {
		return err
	}
	origins, err := store.Origins(trips.IsSource)
	if err != nil {
		return err
	}
	list, written, err := tripStore.Load()
	if err != nil {
		return err
	}

	from := s.today.AddDate(0, 0, -max(r.history, r.lookback))
	recent, err := s.ledger.Transactions(ctx, from, s.today, false)
	if err != nil {
		return err
	}
	waiting, err := s.ledger.Transactions(ctx, time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), s.today, true)
	if err != nil {
		return err
	}
	txs := decide.Merge(waiting, recent)
	byID := make(map[int64]ledger.Transaction, len(txs))
	for _, t := range txs {
		byID[t.ID] = t
	}

	start := s.today.AddDate(0, 0, -r.lookback) // the categorizer changes nothing dated before it
	if err := readMail(ctx, s, r.evidence, start); err != nil {
		return err
	}
	ex, err := explain(ctx, s, r.evidence.Evidence(), r.evidence.Unreadable(), txs, start)
	if err != nil {
		return err
	}
	plan := decide.Make(txs, []decide.Layer{ex.layer, s.rules}, last, start)
	card, inferred, err := inferTrips(ctx, s, txs, plan, from, list)
	if err != nil {
		return err
	}
	hints := list.Hints(inferred, written)
	list, listing := list.Update(trips.Trips(inferred), written)
	pl := trips.Placer{Settings: s.trips, List: list, Written: written, Seen: placed.Seen, Origins: origins, Corrected: corrected(plan), From: start, Paid: ex.paid}.Place(card)
	plan.Place(pl.Changes)
	j := attend(txs, plan, s.attention, judged)
	for id, rs := range pl.Reasons {
		j.reasons[id] = append(j.reasons[id], rs...)
	}
	for _, id := range j.judged { // what the evidence can't explain needs the user once
		if rs := ex.reasons[id]; len(rs) > 0 {
			j.reasons[id] = append(j.reasons[id], rs...)
		}
	}
	plan.Flag(byID, j.reasons)
	inheritSources(plan.Flags, last)

	var suggestions []decide.Suggestion
	for _, sg := range plan.Suggestions {
		if !shown.Seen(sg.Key()) {
			suggestions = append(suggestions, sg)
		}
	}
	trials := learn.Backtest(txs, s.today.AddDate(0, 0, -backtestDays), maxEvidence)
	threshold, found := learn.Threshold(trials, precision, minEvaluated)
	var proposals []learn.Proposal
	if found {
		covered := func(t ledger.Transaction) bool { _, ok := s.rules.Match(t); return ok }
		for _, p := range learn.Propose(txs, threshold, covered) {
			if !shown.Seen(p.ID()) {
				proposals = append(proposals, p)
			}
		}
	}

	changes := append(append(append([]categorize.Change{}, plan.Changes...), plan.Flags...), plan.Corrections...)
	runID := "(none)"
	applied, applyErr := 0, error(nil)
	if len(changes) > 0 || !listing.Empty() {
		jr, err := store.Start(time.Now())
		if err != nil {
			return err
		}
		runID = jr.ID
		applied, applyErr = categorize.Apply(ctx, s.ledger, jr.Record, changes)
		if err := jr.Close(); err != nil && applyErr == nil {
			applyErr = err
		}
	}
	decisions := min(applied, len(plan.Changes))
	flags := min(applied-decisions, len(plan.Flags))
	corrections := plan.Corrections[:applied-decisions-flags]
	unwritten := changes[applied:]
	listed, applyErr := listTrips(applyErr, tripStore, store, runID, list, written, listing)
	var amendments []categorize.Change
	for _, c := range corrections {
		prev, ok := last[c.Charge]
		if !ok || prev.Source == "" || shown.Seen(amendmentKey(c.Charge, prev.Source)) {
			continue
		}
		amendments = append(amendments, categorize.Change{Charge: c.Charge, Before: prev.After, After: c.After, Source: prev.Source})
	}
	var newHints []string
	for _, h := range hints {
		if k := hintKey(h); !shown.Seen(k) {
			newHints = append(newHints, k)
		}
	}
	var newUnreadable []string
	for _, u := range ex.receipts.Unreadable {
		if k := unreadableKey(u); !shown.Seen(k) {
			newUnreadable = append(newUnreadable, k)
		}
	}
	err = r.deliver(ctx, out, report.Run{
		RunID: runID, At: started, Changes: plan.Changes, Applied: decisions,
		Corrections: corrections, Amendments: amendments,
		Undecided: plan.Undecided, Suggestions: suggestions,
		Proposals: proposals, Threshold: threshold, ThresholdFound: found, Trials: trials,
		Titles: s.titles, Transactions: byID,
		Flags: plan.Flags, FlagsApplied: flags, Reasons: landed(j.reasons, unwritten),
		Income: j.cycles, Tolerance: s.attention.Income.Tolerance,
		TripsAdded: withEvidence(listed.Added, inferred), TripVersions: trips.Without(pl.Versions, trips.Names(listing.Added)), Hints: hints,
		Receipts: ex.receipts, NewHints: len(newHints), NewUnreadable: len(newUnreadable),
	})
	if err != nil {
		return errors.Join(applyErr, err)
	}
	for _, sg := range suggestions {
		shown.Add(sg.Key())
	}
	for _, a := range amendments {
		shown.Add(amendmentKey(a.Charge, a.Source))
		// The amendment already showed this rule disagreeing; don't raise it again as a suggestion.
		shown.Add(decide.Suggestion{Transaction: byID[a.Charge], Decision: decide.Decision{Category: a.Before.Category, Source: a.Source}}.Key())
	}
	for _, p := range proposals {
		shown.Add(p.ID())
	}
	for _, k := range append(newHints, newUnreadable...) {
		shown.Add(k)
	}
	remember(judged, j, unwritten)
	rememberPlacements(placed, pl, unwritten)
	return errors.Join(applyErr, shown.Save(), judged.Save(), placed.Save())
}

// deliver writes the run's report to out, or, with notices on, saves it and puts a notice in
// the inbox when it needs the user.
func (r runner) deliver(ctx context.Context, out io.Writer, rep report.Run) error {
	if r.notify == nil {
		return report.WriteRun(out, rep)
	}
	return r.notify.report(ctx, out, rep)
}

func amendmentKey(id int64, source string) string { return fmt.Sprintf("amend %d %s", id, source) }

// hintKey remembers that a notice counted h: the trip the user edited and the inferred span.
func hintKey(h trips.Hint) string {
	return fmt.Sprintf("hint %s %s..%s", h.Edited.Version(), day.Format(h.Inferred.From), day.Format(h.Inferred.To))
}

// unreadableKey remembers that a notice counted u, by its message.
func unreadableKey(u report.Unreadable) string { return "unreadable " + u.ID }

// inheritSources gives each flag on a categorizer-owned transaction the Source of the
// change that last set it, so the journal keeps naming the rule behind the category.
func inheritSources(flags []categorize.Change, last map[int64]categorize.Change) {
	for i, f := range flags {
		if prev, ok := last[f.Charge]; ok && f.Before.Owned() {
			flags[i].Source = prev.Source
		}
	}
}
