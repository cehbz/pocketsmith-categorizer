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
	"github.com/cehbz/pocketsmith-categorizer/journal"
	"github.com/cehbz/pocketsmith-categorizer/learn"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
	"github.com/cehbz/pocketsmith-categorizer/memo"
	"github.com/cehbz/pocketsmith-categorizer/report"
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
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("run: unexpected argument %q", fs.Arg(0))
	}
	s, err := open(ctx, *configPath, *rulesPath)
	if err != nil {
		return err
	}
	shown, err := memo.Load(config.MemoPath(home))
	if err != nil {
		return err
	}
	store := journal.Store{Dir: journalDir}
	last, err := store.LastSet()
	if err != nil {
		return err
	}

	recent, err := s.ledger.Transactions(ctx, s.today.AddDate(0, 0, -max(*history, *lookback)), s.today, false)
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

	plan := decide.Make(txs, []decide.Layer{s.rules}, last, s.today.AddDate(0, 0, -*lookback))

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

	changes := append(append([]categorize.Change{}, plan.Changes...), plan.Corrections...)
	runID := "(none)"
	applied, applyErr := 0, error(nil)
	if len(changes) > 0 {
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
	corrections := plan.Corrections[:applied-decisions]
	var amendments []categorize.Change
	for _, c := range corrections {
		prev, ok := last[c.Charge]
		if !ok || prev.Source == "" || shown.Seen(amendmentKey(c.Charge, prev.Source)) {
			continue
		}
		amendments = append(amendments, categorize.Change{Charge: c.Charge, Before: prev.After, After: c.After, Source: prev.Source})
	}
	err = report.WriteRun(out, report.Run{
		RunID: runID, Changes: plan.Changes, Applied: decisions,
		Corrections: corrections, Amendments: amendments,
		Undecided: plan.Undecided, Suggestions: suggestions,
		Proposals: proposals, Threshold: threshold, ThresholdFound: found, Trials: trials,
		Titles: s.titles, Transactions: byID,
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
	return errors.Join(applyErr, shown.Save())
}

func amendmentKey(id int64, source string) string { return fmt.Sprintf("amend %d %s", id, source) }
