// Command categorize manages categorizer runs; "categorize undo [run-id]" reverses a run.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	api "github.com/cehbz/pocketsmith"

	"github.com/cehbz/pocketsmith-categorizer/categorize"
	"github.com/cehbz/pocketsmith-categorizer/config"
	"github.com/cehbz/pocketsmith-categorizer/journal"
	"github.com/cehbz/pocketsmith-categorizer/pocketsmith"
	"github.com/cehbz/pocketsmith-categorizer/report"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "categorize:", err)
		os.Exit(1)
	}
}

const usage = "usage: categorize undo [-config file] [run-id]"

func run(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 || args[0] != "undo" {
		return errors.New(usage)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	configFile, journalDir := config.Paths(home)
	fs := flag.NewFlagSet("undo", flag.ContinueOnError)
	configPath := fs.String("config", configFile, "config file")
	if err := fs.Parse(args[1:]); err != nil {
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
	key, err := config.ReadKey(ctx, "op", cfg.KeyRef)
	if err != nil {
		return err
	}
	ps := &pocketsmith.Ledger{API: &api.Client{BaseURL: api.DefaultBaseURL, Key: key, HTTP: &http.Client{Timeout: 30 * time.Second}}}
	changes, skipped, unchanged, err := categorize.Undo(ctx, ps, entries)
	if err != nil {
		return err
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
	err = report.WriteUndo(out, report.UndoRun{RunID: runID, Target: target, Changes: changes, Applied: applied, Skipped: skipped, Unchanged: unchanged})
	return errors.Join(applyErr, err)
}
