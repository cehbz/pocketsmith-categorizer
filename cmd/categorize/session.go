package main

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	api "github.com/cehbz/pocketsmith"
	"golang.org/x/oauth2"

	"github.com/cehbz/pocketsmith-categorizer/attention"
	"github.com/cehbz/pocketsmith-categorizer/categorize"
	"github.com/cehbz/pocketsmith-categorizer/config"
	"github.com/cehbz/pocketsmith-categorizer/gcal"
	"github.com/cehbz/pocketsmith-categorizer/internal/day"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
	"github.com/cehbz/pocketsmith-categorizer/plaid"
	"github.com/cehbz/pocketsmith-categorizer/pocketsmith"
	"github.com/cehbz/pocketsmith-categorizer/rules"
	"github.com/cehbz/pocketsmith-categorizer/trips"
)

// sessionLedger is the ledger a session reads transactions from and writes changes to.
type sessionLedger interface {
	categorize.Ledger
	Transactions(ctx context.Context, first, last time.Time, needsReview bool) ([]ledger.Transaction, error)
}

// session is what every command needs: the ledger, its categories and rules, and today.
type session struct {
	ledger    sessionLedger
	titles    map[ledger.CategoryID]string
	rules     rules.Rules
	today     time.Time          // in the ledger's time zone, midnight UTC
	attention attention.Settings // when transactions need the user
	trips     trips.Settings
	card      trips.Card
	accounts  []string // the PocketSmith accounts the card covers
	calendar  trips.Calendar
}

// open loads config, reads the PocketSmith key, the Plaid secrets and the calendar's OAuth
// client, and loads the rules file before any transaction is read.
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
	cal, err := openCalendar(ctx, cfg.Calendar)
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
		trips: tripSet, card: card, accounts: slices.Sorted(maps.Values(cfg.Plaid.Accounts)), calendar: cal}, nil
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

// openCalendar reads the calendar's OAuth client and returns the calendar its token opens.
func openCalendar(ctx context.Context, c config.Calendar) (*gcal.Client, error) {
	conf, err := calendarOAuth(ctx, c)
	if err != nil {
		return nil, err
	}
	h, err := gcal.Authorized(ctx, conf, gcal.TokenFile(c.TokenRef), &http.Client{Timeout: 60 * time.Second})
	if err != nil {
		return nil, err
	}
	return &gcal.Client{BaseURL: gcal.BaseURL, ID: c.ID, HTTP: h}, nil
}

// calendarOAuth is the calendar's OAuth client, its id and secret read through their
// references.
func calendarOAuth(ctx context.Context, c config.Calendar) (*oauth2.Config, error) {
	if missing := c.Missing(); len(missing) > 0 {
		return nil, fmt.Errorf("config: missing %s", strings.Join(missing, ", "))
	}
	var client [2]string
	for i, ref := range []string{c.ClientIDRef, c.ClientSecretRef} {
		s, err := config.ReadSecret(ctx, "op", ref)
		if err != nil {
			return nil, err
		}
		client[i] = s
	}
	return gcal.OAuth(client[0], client[1]), nil
}
