package main

import (
	"context"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	api "github.com/cehbz/pocketsmith"
	"golang.org/x/oauth2"

	"github.com/cehbz/pocketsmith-categorizer/attention"
	"github.com/cehbz/pocketsmith-categorizer/categorize"
	"github.com/cehbz/pocketsmith-categorizer/config"
	"github.com/cehbz/pocketsmith-categorizer/evidence"
	"github.com/cehbz/pocketsmith-categorizer/fx"
	"github.com/cehbz/pocketsmith-categorizer/gcal"
	"github.com/cehbz/pocketsmith-categorizer/gmail"
	"github.com/cehbz/pocketsmith-categorizer/grab"
	"github.com/cehbz/pocketsmith-categorizer/internal/day"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
	"github.com/cehbz/pocketsmith-categorizer/plaid"
	"github.com/cehbz/pocketsmith-categorizer/pocketsmith"
	"github.com/cehbz/pocketsmith-categorizer/retry"
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
	ledger     sessionLedger
	titles     map[ledger.CategoryID]string
	rules      rules.Rules
	today      time.Time          // in the ledger's time zone, midnight UTC
	attention  attention.Settings // when transactions need the user
	trips      trips.Settings
	card       trips.Card
	accounts   []string // the PocketSmith accounts the card covers
	calendar   trips.Calendar
	receipts   mailbox           // the user's mail, for Grab's receipts
	rates      rateSource        // THB per USD
	grabPayees []string          // the payees Grab's charges carry
	grabPolicy categorize.Policy // the category for each Grab service and the label it adds
	loc        *time.Location    // the ledger's time zone
}

// open loads config, reads the PocketSmith key, the Plaid secrets and the Google OAuth client,
// and loads the rules file before any transaction is read.
func open(ctx context.Context, configPath, rulesPath string) (*session, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	key, err := config.ReadSecret(ctx, "op", cfg.KeyRef)
	if err != nil {
		return nil, err
	}
	l := &pocketsmith.Ledger{API: &api.Client{BaseURL: api.DefaultBaseURL, Key: key, HTTP: retry.Client(30 * time.Second)}}
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
	g, err := cfg.Merchant(grab.Merchant(), cats)
	if err != nil {
		return nil, err
	}
	card, err := openCard(ctx, cfg.Plaid)
	if err != nil {
		return nil, err
	}
	cal, mail, err := openGoogle(ctx, cfg.Calendar)
	if err != nil {
		return nil, err
	}
	loc, err := l.TimeZone(ctx)
	if err != nil {
		return nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	titles := make(map[ledger.CategoryID]string, len(cats))
	for _, c := range cats {
		titles[c.ID] = c.Title
	}
	return &session{ledger: l, titles: titles, rules: rs, today: day.Of(time.Now(), loc), attention: set,
		trips: tripSet, card: card, accounts: slices.Sorted(maps.Values(cfg.Plaid.Accounts)), calendar: cal,
		receipts: mail, rates: fx.Cache{Path: config.RatesPath(home), Source: fx.Frankfurter{BaseURL: fx.DefaultBaseURL, HTTP: retry.Client(30 * time.Second)}},
		grabPayees: g.Payees, grabPolicy: categorize.Policy{Ride: g.Categories[evidence.Ride], Delivery: g.Categories[evidence.Food], Label: g.Label}, loc: loc}, nil
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
	c := &plaid.Client{BaseURL: plaid.BaseURL, ClientID: secrets[0], Secret: secrets[1], AccessToken: secrets[2], HTTP: retry.Client(60 * time.Second)}
	return plaid.Card{Client: c, Accounts: p.Accounts}, nil
}

// openGoogle reads the Google OAuth client and returns the calendar and the mailbox its token
// opens, through one authorized HTTP client.
func openGoogle(ctx context.Context, c config.Calendar) (*gcal.Client, *gmail.Client, error) {
	conf, err := googleOAuth(ctx, c)
	if err != nil {
		return nil, nil, err
	}
	h, err := gcal.Authorized(ctx, conf, gcal.TokenFile(c.TokenRef), retry.Client(60*time.Second))
	if err != nil {
		return nil, nil, err
	}
	return &gcal.Client{BaseURL: gcal.BaseURL, ID: c.ID, HTTP: h}, &gmail.Client{BaseURL: gmail.BaseURL, HTTP: h, Log: os.Stderr}, nil
}

// googleOAuth is the Google OAuth client reading the calendar and Gmail and inserting notices
// into the inbox, its id and secret read through the calendar's references.
func googleOAuth(ctx context.Context, c config.Calendar) (*oauth2.Config, error) {
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
	return gcal.OAuth(client[0], client[1], gcal.Scope, gmail.Scope, gmail.InsertScope), nil
}
