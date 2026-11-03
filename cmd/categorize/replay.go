package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/attention"
	"github.com/cehbz/pocketsmith-categorizer/config"
	"github.com/cehbz/pocketsmith-categorizer/decide"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
	"github.com/cehbz/pocketsmith-categorizer/report"
	"github.com/cehbz/pocketsmith-categorizer/tripfile"
	"github.com/cehbz/pocketsmith-categorizer/trips"
)

func replayCmd(ctx context.Context, args []string, out io.Writer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	configFile, _ := config.Paths(home)
	fs := flag.NewFlagSet("replay", flag.ContinueOnError)
	configPath := fs.String("config", configFile, "config file")
	rulesPath := fs.String("rules", config.RulesPath(home), "rules file")
	days := fs.Int("days", 730, "days of history to replay")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("replay: unexpected argument %q", fs.Arg(0))
	}
	s, err := open(ctx, *configPath, *rulesPath)
	if err != nil {
		return err
	}
	from := s.today.AddDate(0, 0, -*days)
	all, err := s.ledger.Transactions(ctx, from.AddDate(0, 0, -replayLead), s.today, false)
	if err != nil {
		return err
	}
	var txs []ledger.Transaction
	for _, t := range all {
		if !t.Date.Before(from) {
			txs = append(txs, t)
		}
	}
	listed, _, err := tripfile.File{List: config.TripsPath(home), Written: config.WrittenTripsPath(home)}.Load()
	if err != nil {
		return err
	}
	inferred, err := replayTrips(ctx, s, all, from, listed)
	if err != nil {
		return err
	}
	users := 0
	for _, t := range txs {
		if t.UserDecided() {
			users++
		}
	}
	decideRule := func(t ledger.Transaction) (ledger.CategoryID, bool) {
		d, ok := s.rules.Decide(t)
		return d.Category, ok
	}
	tally := attention.Replay(all, from, s.today, s.attention, decideRule)
	return report.WriteReplay(out, report.Replay{From: from, To: s.today, Users: users, Stats: s.rules.Replay(txs), Titles: s.titles,
		Attention: &tally, Tolerance: s.attention.Income.Tolerance, Trips: inferred})
}

// replayLead is the days of history replay reads before the period.
const replayLead = 365

// replayTrips is the trips inference finds against the listed trips ending in the period
// from from, inferring over all, read from replayLead days before it, so its warm-up ends
// before the period starts.
func replayTrips(ctx context.Context, s *session, all []ledger.Transaction, from time.Time, listed trips.List) ([]trips.Inferred, error) {
	_, inferred, err := inferTrips(ctx, s, all, decide.Plan{}, from.AddDate(0, 0, -replayLead), listed)
	return slices.DeleteFunc(inferred, func(t trips.Inferred) bool { return t.To.Before(from) }), err
}
