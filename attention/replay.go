package attention

import (
	"slices"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/internal/day"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

// Settings are when transactions need the user.
type Settings struct {
	Start  time.Time // transactions from this date on are judged even when they don't need review
	Size   Size
	Income Income
}

// Week counts the transactions flagged in a week.
type Week struct {
	All              int // flagged for any reason
	WithoutUndecided int // flagged for a reason other than Undecided
}

// Tally is what attention would have flagged over a period.
type Tally struct {
	From, To time.Time
	Reasons  map[string]int // transactions drawing each reason
	Weeks    []Week         // seven days each, from From
	Income   []Month        // the income months in the period
}

// Replay judges every transaction dated from through to as if it were new and needed
// review, undecided where decide abstains; txs also holds the history before from.
func Replay(txs []ledger.Transaction, from, to time.Time, s Settings, decide func(ledger.Transaction) (ledger.CategoryID, bool)) Tally {
	cats := map[int64]ledger.CategoryID{}
	decided := map[int64]bool{}
	for _, t := range txs {
		cat, ok := decide(t)
		if !ok {
			cat = t.State.Category
		}
		cats[t.ID], decided[t.ID] = cat, ok
	}
	h := NewHistory(txs)
	tally := Tally{From: from, To: to, Reasons: map[string]int{}, Weeks: make([]Week, day.Between(from, to)/7+1)}
	for _, t := range txs {
		if t.Date.Before(from) || t.Date.After(to) {
			continue
		}
		rs := h.Reasons(Case{Transaction: t, Category: cats[t.ID], Undecided: !decided[t.ID]}, s.Size, s.Income.Category)
		if len(rs) == 0 {
			continue
		}
		for _, r := range rs {
			tally.Reasons[r]++
		}
		w := &tally.Weeks[day.Between(from, t.Date)/7]
		w.All++
		if slices.ContainsFunc(rs, func(r string) bool { return r != Undecided }) {
			w.WithoutUndecided++
		}
	}
	category := func(t ledger.Transaction) ledger.CategoryID { return cats[t.ID] }
	for m := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC); !m.After(to); m = m.AddDate(0, 1, 0) {
		if mo, ok := s.Income.Check(txs, category, m); ok {
			tally.Income = append(tally.Income, mo)
		}
	}
	return tally
}
