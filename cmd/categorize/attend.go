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
// income cycles checked.
type judgement struct {
	reasons map[int64][]string
	judged  []int64
	cycles  []attention.Cycle
}

func judgedKey(id int64) string        { return fmt.Sprintf("judged %d", id) }
func incomeKey(month time.Time) string { return "income " + month.Format("2006-01") }

// attend checks the income cycles whose bills post from the start date's month on and that
// weren't checked before, and judges the transactions not judged before, among those needing
// review, those dated from the start date on and the credits of a mismatched cycle.
// Categories are judged as the plan leaves them. A cycle with a credit corrected this run
// waits for the next run, since flags leave corrections alone.
func attend(txs []ledger.Transaction, plan decide.Plan, set attention.Settings, seen *memo.Memo) judgement {
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
	j := judgement{reasons: map[int64][]string{}}
	mismatched := map[int64]bool{}
	of := func(t ledger.Transaction) ledger.CategoryID { return category[t.ID] }
	from := time.Date(set.Start.Year(), set.Start.Month(), 1, 0, 0, 0, 0, time.UTC)
	for _, cy := range set.Income.Cycles(txs, of) {
		if cy.Month.Before(from) || seen.Seen(incomeKey(cy.Month)) || slices.ContainsFunc(cy.Credits, func(c ledger.Transaction) bool { return corrected[c.ID] }) {
			continue
		}
		j.cycles = append(j.cycles, cy)
		if cy.Mismatch(set.Income.Tolerance) {
			for _, c := range cy.Credits {
				mismatched[c.ID] = true
			}
		}
	}
	h := attention.NewHistory(txs)
	for _, t := range txs {
		if !corrected[t.ID] && !seen.Seen(judgedKey(t.ID)) && (t.State.NeedsReview || mismatched[t.ID] || !t.Date.Before(set.Start)) {
			j.judged = append(j.judged, t.ID)
			c := attention.Case{Transaction: t, Category: category[t.ID], Undecided: undecided[t.ID]}
			if rs := h.Reasons(c, set.Size, set.Income.Category); len(rs) > 0 {
				j.reasons[t.ID] = rs
			}
		}
		if mismatched[t.ID] {
			j.reasons[t.ID] = append(j.reasons[t.ID], attention.IncomeMismatch)
		}
	}
	return j
}

// remember records the judged transactions and the checked cycles whose writes landed; a
// cycle is remembered once it is ready.
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
	for _, cy := range j.cycles {
		written := cy.Ready
		for _, c := range cy.Credits {
			written = written && !failed[c.ID]
		}
		if written {
			seen.Add(incomeKey(cy.Month))
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
