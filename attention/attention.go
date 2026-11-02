// Package attention judges which transactions need the user, and why.
package attention

import (
	"math"

	"github.com/cehbz/pocketsmith-categorizer/internal/day"
	"github.com/cehbz/pocketsmith-categorizer/learn"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

// Reason labels. They are the user's labels, not the categorizer's: they stay after review.
const (
	NewPayee       = "attention-new-payee"
	Large          = "attention-large"
	Duplicate      = "attention-duplicate"
	Undecided      = "attention-undecided"
	MonthlyIncome  = "attention-monthly-income"
	IncomeMismatch = "attention-income-mismatch"
)

const (
	historyDays   = 365 // days of history a transaction is judged against
	minHistory    = 5   // fewest same-direction transactions a size judgement needs
	duplicateDays = 3   // days back a duplicate is looked for
	duplicateMin  = 20  // amounts at or below this are never duplicates
)

// Size is when an amount is large.
type Size struct {
	Absolute float64 // an amount at least this large always is
	SD       float64 // standard deviations above the history's mean log amount
}

// Case is a transaction as the run leaves it before attention.
type Case struct {
	Transaction ledger.Transaction
	Category    ledger.CategoryID // its category after the run's decision
	Undecided   bool              // it needs review and no layer decided it
}

// History is the transactions reasons are judged against.
type History struct {
	byKey      map[string][]ledger.Transaction
	byCategory map[ledger.CategoryID][]ledger.Transaction
}

// NewHistory indexes txs by payee key and category.
func NewHistory(txs []ledger.Transaction) *History {
	h := &History{byKey: map[string][]ledger.Transaction{}, byCategory: map[ledger.CategoryID][]ledger.Transaction{}}
	for _, t := range txs {
		h.byKey[payeeKey(t.Payee)] = append(h.byKey[payeeKey(t.Payee)], t)
		if t.State.Category != 0 {
			h.byCategory[t.State.Category] = append(h.byCategory[t.State.Category], t)
		}
	}
	return h
}

// Reasons returns the labels of the reasons c needs the user. income is the Monthly Income
// category.
func (h *History) Reasons(c Case, size Size, income ledger.CategoryID) []string {
	t := c.Transaction
	var rs []string
	if h.newPayee(t) {
		rs = append(rs, NewPayee)
	}
	if h.large(t, c.Category, size) {
		rs = append(rs, Large)
	}
	if h.duplicate(t) {
		rs = append(rs, Duplicate)
	}
	if c.Undecided {
		rs = append(rs, Undecided)
	}
	if t.Amount > 0 && income != 0 && c.Category == income {
		rs = append(rs, MonthlyIncome)
	}
	return rs
}

// payeeKey is the learner's payee key, or the whole payee when it has no letters.
func payeeKey(payee string) string {
	if k := learn.PayeeKey(payee); k != "" {
		return k
	}
	return payee
}

// earlier reports whether e comes before t within days: dated up to days before t, or the
// same day with a lower ID.
func earlier(e, t ledger.Transaction, days int) bool {
	gap := day.Between(e.Date, t.Date)
	return gap > 0 && gap <= days || gap == 0 && e.ID < t.ID
}

func (h *History) newPayee(t ledger.Transaction) bool {
	for _, e := range h.byKey[payeeKey(t.Payee)] {
		if earlier(e, t, historyDays) {
			return false
		}
	}
	return true
}

func (h *History) large(t ledger.Transaction, category ledger.CategoryID, size Size) bool {
	a := math.Abs(t.Amount)
	if a >= size.Absolute {
		return true
	}
	if a == 0 {
		return false
	}
	logs := logAmounts(h.byKey[payeeKey(t.Payee)], t)
	if len(logs) < minHistory {
		logs = logAmounts(h.byCategory[category], t)
	}
	if len(logs) < minHistory {
		return false
	}
	mean, sd := stats(logs)
	return math.Log(a) > mean+size.SD*sd
}

// logAmounts are the log amounts of the transactions in list before t, in t's direction.
func logAmounts(list []ledger.Transaction, t ledger.Transaction) []float64 {
	var out []float64
	for _, e := range list {
		if e.Amount != 0 && (e.Amount < 0) == (t.Amount < 0) && earlier(e, t, historyDays) {
			out = append(out, math.Log(math.Abs(e.Amount)))
		}
	}
	return out
}

// stats returns the mean and sample standard deviation of xs, which has at least two values.
func stats(xs []float64) (mean, sd float64) {
	for _, x := range xs {
		mean += x
	}
	mean /= float64(len(xs))
	var ss float64
	for _, x := range xs {
		ss += (x - mean) * (x - mean)
	}
	return mean, math.Sqrt(ss / float64(len(xs)-1))
}

func (h *History) duplicate(t ledger.Transaction) bool {
	if math.Abs(t.Amount) <= duplicateMin {
		return false
	}
	for _, e := range h.byKey[payeeKey(t.Payee)] {
		if cents(e.Amount) == cents(t.Amount) && earlier(e, t, duplicateDays) {
			return true
		}
	}
	return false
}

func cents(x float64) int64 { return int64(math.Round(x * 100)) }
