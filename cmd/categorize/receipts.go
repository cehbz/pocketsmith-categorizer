package main

import (
	"context"
	"fmt"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/categorize"
	"github.com/cehbz/pocketsmith-categorizer/decide"
	"github.com/cehbz/pocketsmith-categorizer/evidence"
	"github.com/cehbz/pocketsmith-categorizer/fx"
	"github.com/cehbz/pocketsmith-categorizer/internal/day"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
	"github.com/cehbz/pocketsmith-categorizer/report"
)

// rateSource gives the THB per USD rates over a span of days.
type rateSource interface {
	Fetch(ctx context.Context, first, last time.Time) (fx.Table, error)
}

// receiptLayer decides the charges merchants' receipts explain and the credits refunding
// their parts.
type receiptLayer struct {
	decided map[int64]decide.Decision
}

// Decide is t's category by its merchant's evidence.
func (l receiptLayer) Decide(t ledger.Transaction) (decide.Decision, bool) {
	d, ok := l.decided[t.ID]
	return d, ok
}

// explained is what the merchants' evidence explains among a run's transactions.
type explained struct {
	layer    receiptLayer
	reasons  map[int64][]string // why the charges and credits the evidence can't explain need the user
	paid     map[int64]int64    // by refunding credit, the charge that paid the part it refunds
	receipts report.Receipts    // the messages that couldn't be read and the receipts no charge paid
}

// explain matches each merchant's charges and credits among txs dated from start with the
// evidence in ev, fetching rates only when there are receipts; unreadable are the reads of
// messages that couldn't be read. A charge a receipt paid takes the category of the receipt's
// kind. A credit takes the category of the charge that paid the part it refunds: the charge's
// own when the user categorized it, else the part's kind's. A charge with no receipt and a
// credit with no part to refund need the user.
func explain(ctx context.Context, s *session, ev evidence.Evidence, unreadable []evidence.Read, txs []ledger.Transaction, start time.Time) (explained, error) {
	ex := explained{layer: receiptLayer{decided: map[int64]decide.Decision{}}, reasons: map[int64][]string{}, paid: map[int64]int64{}}
	for _, u := range unreadable {
		ex.receipts.Unreadable = append(ex.receipts.Unreadable, report.Unreadable{ID: u.ID, Merchant: u.Merchant, Received: u.Received.In(s.loc), Why: u.Unreadable})
	}
	var recent []ledger.Transaction
	for _, t := range txs {
		if !t.Date.Before(start) {
			recent = append(recent, t)
		}
	}
	var rates evidence.Rates
	if len(ev.Receipts) > 0 {
		table, err := s.rates.Fetch(ctx, ratesFrom(ev.Receipts, recent, s.loc), s.today)
		if err != nil {
			return explained{}, err
		}
		ex.receipts.StaleRates = table.Stale
		rates = table.THBPerUSD
	}
	for _, m := range s.merchants {
		mt, err := m.Match(ev, recent, rates, s.loc)
		if err != nil {
			return explained{}, err
		}
		paidBy := map[string]ledger.Transaction{} // by part
		for _, p := range mt.Paid {
			ref := p.Receipt.Ref()
			ex.layer.decided[p.Charge.ID] = decide.Decision{Category: m.Categories[p.Receipt.Kind()], Label: m.Label, Source: m.Name + " " + ref,
				Reason: categorize.Reason{Why: categorize.ByReceipt, Merchant: m.Name, Receipt: ref}}
			for _, part := range p.Receipt.Parts {
				paidBy[part.Ref] = p.Charge
			}
		}
		for _, r := range mt.Refunds {
			d := decide.Decision{Category: m.Categories[r.Part.Kind], Label: m.Label, Source: m.Name + " refund of " + r.Part.Ref,
				Reason: categorize.Reason{Why: categorize.Refund, Merchant: m.Name, Receipt: r.Part.Ref}}
			if c, ok := paidBy[r.Part.Ref]; ok {
				d.Source = fmt.Sprintf("%s refund of %d", m.Name, c.ID)
				if c.UserDecided() {
					d.Category = c.State.Category
				}
				ex.paid[r.Credit.ID] = c.ID
			}
			ex.layer.decided[r.Credit.ID] = d
		}
		for _, c := range mt.Unexplained {
			ex.reasons[c.ID] = append(ex.reasons[c.ID], evidence.NoReceipt(m.Name))
		}
		for _, c := range mt.Unrefunded {
			ex.reasons[c.ID] = append(ex.reasons[c.ID], evidence.NoPurchase(m.Name))
		}
		for _, r := range mt.Unmatched {
			if !r.Amountless() { // a tax invoice is often redundant with its bill's receipt
				r.At = r.At.In(s.loc)
				ex.receipts.Unmatched = append(ex.receipts.Unmatched, r)
			}
		}
	}
	return ex, nil
}

// ratesFrom is the first day the matcher can ask a rate for: the day before the earliest
// receipt's, which a charge's window reaches, or the earliest credit's when that is earlier,
// which a credit converts at its own day. The last day asked, 14 days after the latest credit,
// is covered by fetching through today: a day after the last rate takes the last rate.
func ratesFrom(receipts []evidence.Receipt, txs []ledger.Transaction, loc *time.Location) time.Time {
	first := day.Of(receipts[0].At, loc)
	for _, r := range receipts {
		if d := day.Of(r.At, loc); d.Before(first) {
			first = d
		}
	}
	first = first.AddDate(0, 0, -1)
	for _, t := range txs {
		if t.Amount > 0 && t.Date.Before(first) {
			first = t.Date
		}
	}
	return first
}
