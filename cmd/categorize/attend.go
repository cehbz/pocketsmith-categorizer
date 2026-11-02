package main

import (
	"fmt"
	"slices"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/attention"
	"github.com/cehbz/pocketsmith-categorizer/categorize"
	"github.com/cehbz/pocketsmith-categorizer/decide"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
	"github.com/cehbz/pocketsmith-categorizer/memo"
)

// judgement is what a run judged: reasons per transaction, the transactions judged, and the
// income months checked.
type judgement struct {
	reasons map[int64][]string
	judged  []int64
	months  []attention.Month
}

func judgedKey(id int64) string        { return fmt.Sprintf("judged %d", id) }
func incomeKey(month time.Time) string { return "income " + month.Format("2006-01") }

// attend judges the transactions not judged before, among those needing review and those
// dated from the start date on, and checks the income months from the start date's month
// through today not checked before. Categories are judged as the plan leaves them. A month
// with a credit corrected this run waits for the next run, since flags leave corrections alone.
func attend(txs []ledger.Transaction, plan decide.Plan, set attention.Settings, seen *memo.Memo, today time.Time) judgement {
	category := map[int64]ledger.CategoryID{}
	for _, t := range txs {
		category[t.ID] = t.State.Category
	}
	for _, c := range plan.Changes {
		category[c.Charge] = c.After.Category
	}
	undecided, corrected := map[int64]bool{}, map[int64]bool{}
	for _, t := range plan.Undecided {
		undecided[t.ID] = true
	}
	for _, c := range plan.Corrections {
		corrected[c.Charge] = true
	}
	h := attention.NewHistory(txs)
	j := judgement{reasons: map[int64][]string{}}
	for _, t := range txs {
		if corrected[t.ID] || seen.Seen(judgedKey(t.ID)) || !t.State.NeedsReview && t.Date.Before(set.Start) {
			continue
		}
		j.judged = append(j.judged, t.ID)
		c := attention.Case{Transaction: t, Category: category[t.ID], Undecided: undecided[t.ID]}
		if rs := h.Reasons(c, set.Size, set.Income.Category); len(rs) > 0 {
			j.reasons[t.ID] = rs
		}
	}
	of := func(t ledger.Transaction) ledger.CategoryID { return category[t.ID] }
	for m := time.Date(set.Start.Year(), set.Start.Month(), 1, 0, 0, 0, 0, time.UTC); !m.After(today); m = m.AddDate(0, 1, 0) {
		if seen.Seen(incomeKey(m)) {
			continue
		}
		mo, ok := set.Income.Check(txs, of, m)
		if !ok || slices.ContainsFunc(mo.Credits, func(c ledger.Transaction) bool { return corrected[c.ID] }) {
			continue
		}
		j.months = append(j.months, mo)
		if mo.Mismatch(set.Income.Tolerance) {
			for _, c := range mo.Credits {
				j.reasons[c.ID] = append(j.reasons[c.ID], attention.IncomeMismatch)
			}
		}
	}
	return j
}

// remember records the judged transactions and the checked months whose writes landed; a
// month is remembered once its bill has posted.
func remember(seen *memo.Memo, j judgement, unwritten []categorize.Change) {
	failed := map[int64]bool{}
	for _, c := range unwritten {
		failed[c.Charge] = true
	}
	for _, id := range j.judged {
		if !failed[id] {
			seen.Add(judgedKey(id))
		}
	}
	for _, m := range j.months {
		written := m.Ready
		for _, c := range m.Credits {
			written = written && !failed[c.ID]
		}
		if written {
			seen.Add(incomeKey(m.Start))
		}
	}
}

// landed keeps the reasons whose writes landed.
func landed(reasons map[int64][]string, unwritten []categorize.Change) map[int64][]string {
	out := make(map[int64][]string, len(reasons))
	for id, rs := range reasons {
		out[id] = rs
	}
	for _, c := range unwritten {
		delete(out, c.Charge)
	}
	return out
}
