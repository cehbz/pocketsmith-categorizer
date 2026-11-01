// Command translate turns a PocketSmith category-rules export into a categorizer rules file,
// once: translate -export rules-export.json -out rules.toml
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

	"github.com/cehbz/pocketsmith-categorizer/config"
	"github.com/cehbz/pocketsmith-categorizer/internal/day"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
	"github.com/cehbz/pocketsmith-categorizer/pocketsmith"
	"github.com/cehbz/pocketsmith-categorizer/translate"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "translate:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	configFile, _ := config.Paths(home)
	fs := flag.NewFlagSet("translate", flag.ContinueOnError)
	configPath := fs.String("config", configFile, "config file")
	exportPath := fs.String("export", "", "PocketSmith category-rules export (JSON)")
	outPath := fs.String("out", "", "rules file to write")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *exportPath == "" || *outPath == "" {
		return errors.New("usage: translate [-config file] -export rules-export.json -out rules.toml")
	}
	f, err := os.Open(*exportPath)
	if err != nil {
		return err
	}
	export, err := translate.ReadExport(f)
	f.Close()
	if err != nil {
		return fmt.Errorf("%s: %w", *exportPath, err)
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	key, err := config.ReadKey(ctx, "op", cfg.KeyRef)
	if err != nil {
		return err
	}
	l := &pocketsmith.Ledger{API: &api.Client{BaseURL: api.DefaultBaseURL, Key: key, HTTP: &http.Client{Timeout: 30 * time.Second}}}
	loc, err := l.TimeZone(ctx)
	if err != nil {
		return err
	}
	today := day.Of(time.Now(), loc)
	history, err := l.Transactions(ctx, today.AddDate(-2, 0, 0), today, false)
	if err != nil {
		return err
	}
	cats, err := l.Categories(ctx)
	if err != nil {
		return err
	}
	titles := make(map[ledger.CategoryID]string, len(cats))
	for _, c := range cats {
		titles[c.ID] = c.Title
	}
	r := translate.Translate(export, history, titles)
	w, err := os.OpenFile(*outPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err := translate.WriteRules(w, r.Rules); err != nil {
		w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return translate.WriteReport(out, r)
}
