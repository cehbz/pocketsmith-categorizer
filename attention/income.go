package attention

import (
	"cmp"
	"math"
	"slices"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

// Income is the monthly income check.
type Income struct {
	Account   string            // the account Monthly Income arrives in
	Category  ledger.CategoryID // Monthly Income
	Bill      ledger.CategoryID // the card bill paid from the account
	Rent      ledger.CategoryID
	Cash      ledger.CategoryID // cash withdrawn from the account
	Target    float64           // the balance the account is topped up to
	Tolerance float64           // how far a cycle may land from the estimate
}

// Cycle is an income cycle's check. A cycle runs from just after one card bill through the
// next; its rent is the one posted in it, or when it has none, the first posted after its bill.
type Cycle struct {
	Month    time.Time            // the first day of the month its bill posts in
	Credits  []ledger.Transaction // the cycle's Monthly Income credits
	Expected float64              // the Monthly Income the cycle needed
	Actual   float64              // the Monthly Income it got
	Ready    bool                 // its bill and rent have posted, and its first credit's closing balance is known
}

// Mismatch reports whether the cycle's Monthly Income missed the estimate by more than
// tolerance; a cycle whose bill or rent hasn't posted can't miss.
func (c Cycle) Mismatch(tolerance float64) bool {
	return c.Ready && math.Abs(c.Actual-c.Expected) > tolerance
}

// Cycles checks the account's income cycles that have Monthly Income, in order: the balance
// before a cycle's first Monthly Income credit, plus its credits, less its bill, rent and the
// cash withdrawn after that credit, should land at the target. category gives each
// transaction's category as the run leaves it.
func (in Income) Cycles(txs []ledger.Transaction, category func(ledger.Transaction) ledger.CategoryID) []Cycle {
	var account []ledger.Transaction
	for _, t := range txs {
		if t.Account.Name == in.Account {
			account = append(account, t)
		}
	}
	account = InOrder(account)
	var spans [][]ledger.Transaction // each through its bill; the last one open
	from := 0
	for i, t := range account {
		if category(t) == in.Bill && t.Amount < 0 {
			spans = append(spans, account[from:i+1])
			from = i + 1
		}
	}
	spans = append(spans, account[from:])
	var out []Cycle
	taken := map[int64]bool{} // late rents, taken by the cycle before
	for i, span := range spans {
		c, first, rent := Cycle{}, -1, false
		for j, t := range span {
			switch cat := category(t); {
			case taken[t.ID]:
			case t.Amount > 0 && cat == in.Category:
				if first < 0 {
					first = j
					c.Expected = in.Target - (t.ClosingBalance - t.Amount)
				}
				c.Credits = append(c.Credits, t)
				c.Actual += t.Amount
			case cat == in.Rent:
				rent = true
				if first >= 0 {
					c.Expected -= t.Amount
				}
			case first >= 0 && (cat == in.Bill || cat == in.Cash):
				c.Expected -= t.Amount
			}
		}
		if first < 0 {
			continue
		}
		closed := i < len(spans)-1
		if closed && !rent {
			if j := slices.IndexFunc(spans[i+1], func(t ledger.Transaction) bool { return category(t) == in.Rent }); j >= 0 {
				late := spans[i+1][j]
				taken[late.ID], rent = true, true
				c.Expected -= late.Amount
			}
		}
		switch {
		case closed:
			c.Month = monthOf(span[len(span)-1].Date)
		case i > 0:
			c.Month = monthOf(spans[i-1][len(spans[i-1])-1].Date).AddDate(0, 1, 0)
		default:
			c.Month = monthOf(span[first].Date)
		}
		// A zero closing balance is PocketSmith's missing one; the estimate can't be trusted.
		c.Ready = closed && rent && span[first].ClosingBalance != 0
		out = append(out, c)
	}
	return out
}

func monthOf(d time.Time) time.Time { return time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, time.UTC) }

// InOrder sorts one account's transactions into posting order: by date, and within a day
// by following the closing balances, each transaction opening at the balance the one
// before it closed at. A day whose balances don't chain keeps ID order.
func InOrder(txs []ledger.Transaction) []ledger.Transaction {
	out := slices.Clone(txs)
	slices.SortFunc(out, func(a, b ledger.Transaction) int {
		if c := a.Date.Compare(b.Date); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})
	for i := 0; i < len(out); {
		j := i
		for j < len(out) && out[j].Date.Equal(out[i].Date) {
			j++
		}
		if chained, ok := chain(out[i:j]); ok {
			copy(out[i:j], chained)
		}
		i = j
	}
	return out
}

// chain orders one day's transactions by their balances; ok is false when they don't chain.
func chain(day []ledger.Transaction) (out []ledger.Transaction, ok bool) {
	if len(day) < 2 {
		return day, true
	}
	closes := map[int64]int{}
	for _, t := range day {
		closes[cents(t.ClosingBalance)]++
	}
	start := -1
	for i, t := range day {
		if closes[cents(t.ClosingBalance-t.Amount)] == 0 {
			if start >= 0 {
				return nil, false
			}
			start = i
		}
	}
	if start < 0 {
		return nil, false
	}
	used := map[int]bool{start: true}
	out = []ledger.Transaction{day[start]}
	for len(out) < len(day) {
		next := -1
		for i, t := range day {
			if !used[i] && cents(t.ClosingBalance-t.Amount) == cents(out[len(out)-1].ClosingBalance) {
				next = i
				break
			}
		}
		if next < 0 {
			return nil, false
		}
		used[next] = true
		out = append(out, day[next])
	}
	return out, true
}
