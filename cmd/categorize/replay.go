package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/cehbz/pocketsmith-categorizer/config"
	"github.com/cehbz/pocketsmith-categorizer/report"
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
	txs, err := s.ledger.Transactions(ctx, from, s.today, false)
	if err != nil {
		return err
	}
	users := 0
	for _, t := range txs {
		if t.UserDecided() {
			users++
		}
	}
	return report.WriteReplay(out, report.Replay{From: from, To: s.today, Users: users, Stats: s.rules.Replay(txs), Titles: s.titles})
}
