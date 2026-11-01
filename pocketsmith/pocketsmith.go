// Package pocketsmith adapts the PocketSmith client to the categorizer's ledger.
package pocketsmith

import (
	"context"
	"slices"
	"time"

	api "github.com/cehbz/pocketsmith"

	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

// API is the part of the PocketSmith client the ledger uses.
type API interface {
	Profile(ctx context.Context) (api.Profile, error)
	Transactions(ctx context.Context, q api.Query) ([]api.Transaction, error)
	Transaction(ctx context.Context, id int64) (api.Transaction, error)
	Update(ctx context.Context, id int64, u api.Update) error
	Categories(ctx context.Context, user int64) ([]api.Category, error)
}

// Ledger is the categorizer's ledger in PocketSmith.
type Ledger struct {
	API API
}

func charge(t api.Transaction) ledger.Charge {
	var cat ledger.CategoryID
	if t.Category != nil {
		cat = ledger.CategoryID(t.Category.ID)
	}
	return ledger.Charge{
		ID:    t.ID,
		USD:   -t.Amount,
		Date:  t.Date,
		Payee: t.Payee,
		State: ledger.State{Category: cat, NeedsReview: t.NeedsReview, Labels: t.Labels},
	}
}

// Charges lists an account's transactions from first to last whose payee is one of payees.
func (l *Ledger) Charges(ctx context.Context, account int64, first, last time.Time, payees []string) ([]ledger.Charge, error) {
	txs, err := l.API.Transactions(ctx, api.Query{Account: account, From: first, To: last})
	if err != nil {
		return nil, err
	}
	var charges []ledger.Charge
	for _, t := range txs {
		if slices.Contains(payees, t.Payee) {
			charges = append(charges, charge(t))
		}
	}
	return charges, nil
}

// Charge reads one transaction.
func (l *Ledger) Charge(ctx context.Context, id int64) (ledger.Charge, error) {
	t, err := l.API.Transaction(ctx, id)
	if err != nil {
		return ledger.Charge{}, err
	}
	return charge(t), nil
}

// Update sets a transaction's category, review flag and labels; category zero uncategorizes it.
func (l *Ledger) Update(ctx context.Context, id int64, s ledger.State) error {
	return l.API.Update(ctx, id, api.Update{Category: int64(s.Category), NeedsReview: s.NeedsReview, Labels: s.Labels})
}

// TimeZone returns the location of the user's PocketSmith time zone.
func (l *Ledger) TimeZone(ctx context.Context) (*time.Location, error) {
	p, err := l.API.Profile(ctx)
	if err != nil {
		return nil, err
	}
	return p.TimeZone, nil
}

func transaction(t api.Transaction) ledger.Transaction {
	return ledger.Transaction{
		ID:      t.ID,
		Date:    t.Date,
		Amount:  t.Amount,
		Payee:   t.Payee,
		Account: ledger.Account{ID: t.Account.ID, Name: t.Account.Name},
		State:   charge(t).State,
	}
}

// Transactions lists the user's transactions on every account dated first to last,
// optionally only those needing review.
func (l *Ledger) Transactions(ctx context.Context, first, last time.Time, needsReview bool) ([]ledger.Transaction, error) {
	p, err := l.API.Profile(ctx)
	if err != nil {
		return nil, err
	}
	txs, err := l.API.Transactions(ctx, api.Query{User: p.ID, From: first, To: last, NeedsReview: needsReview})
	if err != nil {
		return nil, err
	}
	out := make([]ledger.Transaction, len(txs))
	for i, t := range txs {
		out[i] = transaction(t)
	}
	return out, nil
}

// Categories lists the user's categories.
func (l *Ledger) Categories(ctx context.Context) ([]ledger.Category, error) {
	p, err := l.API.Profile(ctx)
	if err != nil {
		return nil, err
	}
	cs, err := l.API.Categories(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	out := make([]ledger.Category, len(cs))
	for i, c := range cs {
		out[i] = ledger.Category{ID: ledger.CategoryID(c.ID), Title: c.Title}
	}
	return out, nil
}
