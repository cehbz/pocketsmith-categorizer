package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"net/http"
	"os"
	"time"

	api "github.com/cehbz/pocketsmith"

	"github.com/cehbz/pocketsmith-categorizer/categorize"
	"github.com/cehbz/pocketsmith-categorizer/config"
	"github.com/cehbz/pocketsmith-categorizer/journal"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
	"github.com/cehbz/pocketsmith-categorizer/memo"
	"github.com/cehbz/pocketsmith-categorizer/pocketsmith"
	"github.com/cehbz/pocketsmith-categorizer/report"
)

func undoCmd(ctx context.Context, args []string, out io.Writer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	configFile, journalDir := config.Paths(home)
	fs := flag.NewFlagSet("undo", flag.ContinueOnError)
	configPath := fs.String("config", configFile, "config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	store := journal.Store{Dir: journalDir}
	target := fs.Arg(0)
	if target == "" {
		if target, err = store.Latest(); err != nil {
			return err
		}
	}
	entries, err := store.Load(target)
	if err != nil {
		return err
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	key, err := config.ReadSecret(ctx, "op", cfg.KeyRef)
	if err != nil {
		return err
	}
	seen, err := memo.Load(config.JudgedPath(home))
	if err != nil {
		return err
	}
	ps := &pocketsmith.Ledger{API: &api.Client{BaseURL: api.DefaultBaseURL, Key: key, HTTP: &http.Client{Timeout: 30 * time.Second}}}
	read := &reading{Ledger: ps, charges: map[int64]ledger.Charge{}}
	changes, skipped, unchanged, err := categorize.Undo(ctx, read, entries)
	if err != nil {
		return err
	}
	for i := range changes {
		changes[i].Source = journal.UndoSource(target)
	}
	runID := "(none)"
	applied, applyErr := 0, error(nil)
	if len(changes) > 0 {
		jr, err := store.Start(time.Now())
		if err != nil {
			return err
		}
		runID = jr.ID
		applied, applyErr = categorize.Apply(ctx, ps, jr.Record, changes)
		if err := jr.Close(); err != nil && applyErr == nil {
			applyErr = err
		}
	}
	var reverted []ledger.Charge
	for _, c := range changes[:applied] {
		reverted = append(reverted, read.charges[c.Charge])
	}
	forget(seen, reverted)
	err = report.WriteUndo(out, report.UndoRun{RunID: runID, Target: target, Changes: changes, Applied: applied, Skipped: skipped, Unchanged: unchanged})
	return errors.Join(applyErr, err, seen.Save())
}

// reading is a ledger that keeps the charges read through it.
type reading struct {
	categorize.Ledger
	charges map[int64]ledger.Charge
}

func (r *reading) Charge(ctx context.Context, id int64) (ledger.Charge, error) {
	c, err := r.Ledger.Charge(ctx, id)
	if err == nil {
		r.charges[id] = c
	}
	return c, err
}

// forget drops the judgements of the reverted charges and the checks of the income cycles
// they may belong to, so the next run judges them again. A charge belongs to the cycle billed
// in its month or the next.
func forget(seen *memo.Memo, reverted []ledger.Charge) {
	for _, c := range reverted {
		month := time.Date(c.Date.Year(), c.Date.Month(), 1, 0, 0, 0, 0, time.UTC)
		seen.Forget(judgedKey(c.ID))
		seen.Forget(incomeKey(month))
		seen.Forget(incomeKey(month.AddDate(0, 1, 0)))
	}
}
