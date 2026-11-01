// Package decide asks the categorizer's layers about each transaction and plans a run.
package decide

import (
	"fmt"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/categorize"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

// Decision is a layer's category for a transaction.
type Decision struct {
	Category ledger.CategoryID
	Label    string // the deciding layer's label
	Source   string // what decided, such as a rule's key
}

// Layer decides a transaction's category or abstains.
type Layer interface {
	Decide(t ledger.Transaction) (Decision, bool)
}

// Suggestion is one of the user's transactions a layer would categorize differently.
type Suggestion struct {
	Transaction ledger.Transaction
	Decision    Decision
}

// Key identifies the suggestion: the transaction, the proposed category and what proposed it.
func (s Suggestion) Key() string {
	return fmt.Sprintf("suggest %d %d %s", s.Transaction.ID, s.Decision.Category, s.Decision.Source)
}

// Plan is what a run will do.
type Plan struct {
	Changes     []categorize.Change  // decisions for transactions in scope
	Corrections []categorize.Change  // categorizer labels removed where the user recategorized
	Undecided   []ledger.Transaction // needing review, no layer decided
	Suggestions []Suggestion         // the user's transactions a layer disagrees with
}

// Make plans a run over txs. set holds the change the journal last recorded for each
// transaction; a state that differs from the record's After and from its Before is the user's correction; since is the first date of the lookback.
func Make(txs []ledger.Transaction, layers []Layer, set map[int64]categorize.Change, since time.Time) Plan {
	var p Plan
	for _, t := range txs {
		owned := t.State.Owned()
		if rec, known := set[t.ID]; owned && known && rec.After.Category != t.State.Category && !t.State.Equal(rec.Before) {
			after := ledger.State{Category: t.State.Category, NeedsReview: t.State.NeedsReview, Labels: t.State.UserLabels()}
			p.Corrections = append(p.Corrections, categorize.Change{Charge: t.ID, Before: t.State, After: after})
			continue
		}
		d, decided := first(layers, t)
		inScope := t.State.NeedsReview || owned && !t.Date.Before(since)
		switch {
		case inScope && decided:
			after := ledger.State{Category: d.Category, Labels: append(t.State.UserLabels(), d.Label)}
			if !t.State.Equal(after) {
				p.Changes = append(p.Changes, categorize.Change{Charge: t.ID, Before: t.State, After: after, Source: d.Source})
			}
		case inScope && t.State.NeedsReview:
			p.Undecided = append(p.Undecided, t)
		case !inScope && !owned && decided && d.Category != t.State.Category:
			p.Suggestions = append(p.Suggestions, Suggestion{Transaction: t, Decision: d})
		}
	}
	return p
}

func first(layers []Layer, t ledger.Transaction) (Decision, bool) {
	for _, l := range layers {
		if d, ok := l.Decide(t); ok {
			return d, true
		}
	}
	return Decision{}, false
}

// Merge concatenates lists, keeping the first copy of each transaction ID.
func Merge(lists ...[]ledger.Transaction) []ledger.Transaction {
	seen := map[int64]bool{}
	var out []ledger.Transaction
	for _, l := range lists {
		for _, t := range l {
			if !seen[t.ID] {
				seen[t.ID] = true
				out = append(out, t)
			}
		}
	}
	return out
}
