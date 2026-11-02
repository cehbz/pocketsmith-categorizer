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
	Tolerance float64           // how far a month may land from the estimate
}

// Month is an income month's check.
type Month struct {
	Start    time.Time            // the month's first day
	Credits  []ledger.Transaction // the month's Monthly Income credits
	Expected float64              // the Monthly Income the month needed
	Actual   float64              // the Monthly Income it got
	Ready    bool                 // the card bill has posted after the first credit, whose closing balance is known
}

// Mismatch reports whether the month's Monthly Income missed the estimate by more than
// tolerance; a month whose bill hasn't posted can't miss.
func (m Month) Mismatch(tolerance float64) bool {
	return m.Ready && math.Abs(m.Actual-m.Expected) > tolerance
}

// Check checks the income month starting at start: the account's balance before the
// month's first Monthly Income credit, plus the month's Monthly Income credits, less the card
// bill, rent and cash posted after that credit, should land at the target. category gives each
// transaction's category as the run leaves it. ok is false for a month without Monthly Income.
func (in Income) Check(txs []ledger.Transaction, category func(ledger.Transaction) ledger.CategoryID, start time.Time) (m Month, ok bool) {
	var month []ledger.Transaction
	for _, t := range txs {
		if t.Account.Name == in.Account && t.Date.Year() == start.Year() && t.Date.Month() == start.Month() {
			month = append(month, t)
		}
	}
	month = InOrder(month)
	m.Start = start
	first := -1
	for i, t := range month {
		if t.Amount > 0 && category(t) == in.Category {
			if first < 0 {
				first = i
			}
			m.Credits = append(m.Credits, t)
			m.Actual += t.Amount
		}
	}
	if first < 0 {
		return Month{}, false
	}
	m.Expected = in.Target - (month[first].ClosingBalance - month[first].Amount)
	for _, t := range month[first+1:] {
		switch category(t) {
		case in.Bill:
			m.Expected -= t.Amount
			m.Ready = m.Ready || t.Amount < 0
		case in.Rent, in.Cash:
			m.Expected -= t.Amount
		}
	}
	// A zero closing balance is PocketSmith's missing one; the estimate can't be trusted.
	m.Ready = m.Ready && month[first].ClosingBalance != 0
	return m, true
}

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
