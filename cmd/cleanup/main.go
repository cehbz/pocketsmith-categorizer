// Command cleanup categorizes Grab charges from Grab history statements, once.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	api "github.com/cehbz/pocketsmith"

	"github.com/cehbz/pocketsmith-categorizer/categorize"
	"github.com/cehbz/pocketsmith-categorizer/config"
	"github.com/cehbz/pocketsmith-categorizer/fx"
	"github.com/cehbz/pocketsmith-categorizer/grab"
	"github.com/cehbz/pocketsmith-categorizer/history"
	"github.com/cehbz/pocketsmith-categorizer/journal"
	"github.com/cehbz/pocketsmith-categorizer/pocketsmith"
	"github.com/cehbz/pocketsmith-categorizer/report"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "cleanup:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	configFile, journalDir := config.Paths(home)
	fs := flag.NewFlagSet("cleanup", flag.ContinueOnError)
	configPath := fs.String("config", configFile, "config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return errors.New("usage: cleanup [-config file] account=statement.pdf ...")
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	key, err := config.ReadKey(ctx, "op", cfg.KeyRef)
	if err != nil {
		return err
	}
	httpClient := &http.Client{Timeout: 30 * time.Second}
	ps := &pocketsmith.Ledger{API: &api.Client{BaseURL: api.DefaultBaseURL, Key: key, HTTP: httpClient}}
	loc, err := ps.TimeZone(ctx)
	if err != nil {
		return err
	}
	bkk, err := time.LoadLocation("Asia/Bangkok")
	if err != nil {
		return err
	}
	var statements [][]grab.Payment
	for _, a := range fs.Args() {
		account, path, ok := strings.Cut(a, "=")
		if !ok {
			return fmt.Errorf("statement %q: want account=path", a)
		}
		text, err := history.Text(ctx, "pdftotext", path)
		if err != nil {
			return err
		}
		payments, err := history.Parse(text, account, bkk)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		statements = append(statements, payments)
	}
	payments := history.Merge(statements...)
	if len(payments) == 0 {
		return errors.New("the statements hold no payments")
	}
	first, last := period(payments, loc)
	charges, err := ps.Charges(ctx, cfg.AccountID, first, last, cfg.Payees)
	if err != nil {
		return err
	}
	rates, err := fx.Frankfurter{BaseURL: fx.DefaultBaseURL, HTTP: httpClient}.Fetch(ctx, first, last)
	if err != nil {
		return err
	}
	m, err := categorize.Match(payments, charges, rates.THBPerUSD, loc)
	if err != nil {
		return err
	}
	policy := cfg.Policy()
	changes, correct := policy.Changes(m.Pairs)
	tipChanges, tipCorrect, rest, err := settleTips(policy, cfg.Tips, rates.THBPerUSD, m.Unclaimed)
	if err != nil {
		return err
	}
	changes, correct, m.Unclaimed = append(changes, tipChanges...), correct+tipCorrect, rest
	runID := "(none)"
	applied, applyErr := 0, error(nil)
	if len(changes) > 0 {
		jr, err := journal.Store{Dir: journalDir}.Start(time.Now())
		if err != nil {
			return err
		}
		runID = jr.ID
		applied, applyErr = categorize.Apply(ctx, ps, jr.Record, changes)
		if err := jr.Close(); err != nil && applyErr == nil {
			applyErr = err
		}
	}
	err = report.Write(out, report.Cleanup{
		RunID: runID, Changes: changes, Applied: applied, Correct: correct,
		Matching: m, Payments: payments, Rates: rates.THBPerUSD, Ledger: loc,
	})
	return errors.Join(applyErr, err)
}

// period is the span of ledger dates the payments' charges can carry.
func period(payments []grab.Payment, loc *time.Location) (first, last time.Time) {
	for i, p := range payments {
		f, l := p.PostingWindow(loc)
		if i == 0 || f.Before(first) {
			first = f
		}
		if i == 0 || l.After(last) {
			last = l
		}
	}
	return first, last
}
