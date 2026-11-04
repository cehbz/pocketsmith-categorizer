package main

import (
	"context"
	"slices"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/categorize"
	"github.com/cehbz/pocketsmith-categorizer/decide"
	"github.com/cehbz/pocketsmith-categorizer/fx"
	"github.com/cehbz/pocketsmith-categorizer/gmail"
	"github.com/cehbz/pocketsmith-categorizer/grab"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
	"github.com/cehbz/pocketsmith-categorizer/report"
)

// mailbox finds the user's messages.
type mailbox interface {
	Messages(ctx context.Context, query string, from, to time.Time) ([]gmail.Message, error)
}

// rateSource gives the THB per USD rates over a span of days.
type rateSource interface {
	Fetch(ctx context.Context, first, last time.Time) (fx.Table, error)
}

// receiptQuery finds Grab's e-receipts.
const receiptQuery = "from:no-reply@grab.com subject:\"Your Grab E-Receipt\""

// grabLayer decides the Grab charges a receipt matched.
type grabLayer struct {
	decided map[int64]decide.Decision
}

// Decide is the category of the service on t's receipt.
func (g grabLayer) Decide(t ledger.Transaction) (decide.Decision, bool) {
	d, ok := g.decided[t.ID]
	return d, ok
}

// readGrab reads the Grab receipts that arrived from from to today and returns the layer
// deciding the charges among txs they match, and the receipts it couldn't read or match.
func readGrab(ctx context.Context, s *session, txs []ledger.Transaction, from time.Time) (grabLayer, report.Receipts, error) {
	msgs, err := s.receipts.Messages(ctx, receiptQuery, from, s.today)
	if err != nil {
		return grabLayer{}, report.Receipts{}, err
	}
	var receipts report.Receipts
	var payments []grab.Payment
	for _, m := range msgs {
		received := m.Received.In(s.loc)
		p, err := grab.ReadReceipt(m.Text, received, m.To)
		if err != nil {
			receipts.Unreadable = append(receipts.Unreadable, report.Unreadable{ID: m.ID, Received: received, Why: err.Error()})
			continue
		}
		payments = append(payments, p)
	}
	if len(payments) == 0 {
		return grabLayer{}, receipts, nil
	}
	first, last := period(payments, s.loc)
	rates, err := s.rates.Fetch(ctx, first, last)
	if err != nil {
		return grabLayer{}, report.Receipts{}, err
	}
	receipts.StaleRates = rates.Stale
	var charges []ledger.Charge
	for _, t := range txs {
		if slices.Contains(s.grabPayees, t.Payee) {
			charges = append(charges, ledger.Charge{ID: t.ID, USD: -t.Amount, Date: t.Date, Payee: t.Payee, State: t.State})
		}
	}
	m, err := categorize.Match(payments, charges, rates.THBPerUSD, s.loc)
	if err != nil {
		return grabLayer{}, report.Receipts{}, err
	}
	layer := grabLayer{decided: make(map[int64]decide.Decision, len(m.Pairs))}
	for _, pr := range m.Pairs {
		layer.decided[pr.Charge.ID] = decide.Decision{Category: s.grabPolicy.CategoryFor(pr.Payment), Label: s.grabPolicy.Label, Source: "grab " + pr.Payment.Booking}
	}
	receipts.Unmatched = m.Unmatched
	return layer, receipts, nil
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
