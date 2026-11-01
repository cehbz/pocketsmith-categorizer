package main

import (
	"context"
	"net/http"
	"time"

	api "github.com/cehbz/pocketsmith"

	"github.com/cehbz/pocketsmith-categorizer/config"
	"github.com/cehbz/pocketsmith-categorizer/internal/day"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
	"github.com/cehbz/pocketsmith-categorizer/pocketsmith"
	"github.com/cehbz/pocketsmith-categorizer/rules"
)

// session is what every command needs: the ledger, its categories and rules, and today.
type session struct {
	ledger *pocketsmith.Ledger
	titles map[ledger.CategoryID]string
	rules  rules.Rules
	today  time.Time // in the ledger's time zone, midnight UTC
}

// open loads config, reads the key, and loads the rules file before any transaction is read.
func open(ctx context.Context, configPath, rulesPath string) (*session, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	key, err := config.ReadKey(ctx, "op", cfg.KeyRef)
	if err != nil {
		return nil, err
	}
	l := &pocketsmith.Ledger{API: &api.Client{BaseURL: api.DefaultBaseURL, Key: key, HTTP: &http.Client{Timeout: 30 * time.Second}}}
	cats, err := l.Categories(ctx)
	if err != nil {
		return nil, err
	}
	rs, err := rules.Load(rulesPath, cats)
	if err != nil {
		return nil, err
	}
	loc, err := l.TimeZone(ctx)
	if err != nil {
		return nil, err
	}
	titles := make(map[ledger.CategoryID]string, len(cats))
	for _, c := range cats {
		titles[c.ID] = c.Title
	}
	return &session{ledger: l, titles: titles, rules: rs, today: day.Of(time.Now(), loc)}, nil
}
