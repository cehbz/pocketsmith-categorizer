package main

import (
	"context"
	"maps"
	"net/http"
	"slices"
	"time"

	api "github.com/cehbz/pocketsmith"

	"github.com/cehbz/pocketsmith-categorizer/attention"
	"github.com/cehbz/pocketsmith-categorizer/config"
	"github.com/cehbz/pocketsmith-categorizer/internal/day"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
	"github.com/cehbz/pocketsmith-categorizer/plaid"
	"github.com/cehbz/pocketsmith-categorizer/pocketsmith"
	"github.com/cehbz/pocketsmith-categorizer/rules"
	"github.com/cehbz/pocketsmith-categorizer/trips"
)

// session is what every command needs: the ledger, its categories and rules, and today.
type session struct {
	ledger    *pocketsmith.Ledger
	titles    map[ledger.CategoryID]string
	rules     rules.Rules
	today     time.Time          // in the ledger's time zone, midnight UTC
	attention attention.Settings // when transactions need the user
	trips     trips.Settings
	card      trips.Card
	accounts  []string // the PocketSmith accounts the card covers
}

// open loads config, reads the PocketSmith key and the Plaid secrets, and loads the rules
// file before any transaction is read.
func open(ctx context.Context, configPath, rulesPath string) (*session, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	key, err := config.ReadSecret(ctx, "op", cfg.KeyRef)
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
	set, err := cfg.Settings(cats)
	if err != nil {
		return nil, err
	}
	tripSet, err := cfg.TripSettings(cats, set.Start)
	if err != nil {
		return nil, err
	}
	card, err := openCard(ctx, cfg.Plaid)
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
	return &session{ledger: l, titles: titles, rules: rs, today: day.Of(time.Now(), loc), attention: set,
		trips: tripSet, card: card, accounts: slices.Sorted(maps.Values(cfg.Plaid.Accounts))}, nil
}

// openCard reads the Plaid secrets and returns the card they open.
func openCard(ctx context.Context, p config.Plaid) (plaid.Card, error) {
	var secrets [3]string
	for i, ref := range []string{p.ClientIDRef, p.SecretRef, p.AccessTokenRef} {
		s, err := config.ReadSecret(ctx, "op", ref)
		if err != nil {
			return plaid.Card{}, err
		}
		secrets[i] = s
	}
	c := &plaid.Client{BaseURL: plaid.BaseURL, ClientID: secrets[0], Secret: secrets[1], AccessToken: secrets[2], HTTP: &http.Client{Timeout: 60 * time.Second}}
	return plaid.Card{Client: c, Accounts: p.Accounts}, nil
}
