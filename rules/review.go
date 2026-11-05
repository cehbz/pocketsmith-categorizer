package rules

import (
	"maps"
	"slices"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/categorize"
	"github.com/cehbz/pocketsmith-categorizer/decide"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

// Amendment is a rule the user's override changed: the rule before and after, and the charge
// whose category the user changed.
type Amendment struct {
	Was, Now Rule
	Charge   ledger.Transaction
}

// Revision is what the user's review of the rules' decisions changes in the rules file.
type Revision struct {
	Edits   []Edit      // in file order
	Amended []Amendment // the overrides among the edits, in file order
}

// Review finds the user's overrides and acceptances of the rules' decisions among txs. last
// holds the change the journal last recorded for each transaction; its Source names the rule
// that decided, which is found by its key, the first rule holding it, and is gone when the user
// has edited the rule since. An override, a categorizer-owned charge the user moved to another
// category, amends the rule: it takes the user's category, becomes tentative and starts at the
// charge's date, or has no start date when the charge carries the apply-all label; of several
// overrides of a rule, the latest-dated charge's decides. An acceptance, a tentative rule's
// charge the user took out of review in the rule's category, confirms the rule unless an
// override amends it.
func (rs Rules) Review(txs []ledger.Transaction, last map[int64]categorize.Change, titles map[ledger.CategoryID]string) Revision {
	byKey := map[string]int{}
	for i, r := range rs {
		if _, dup := byKey[r.Key()]; !dup {
			byKey[r.Key()] = i
		}
	}
	overrides := map[int]ledger.Transaction{}
	accepted := map[int]bool{}
	for _, t := range txs {
		rec, ok := last[t.ID]
		if !ok || !t.State.Owned() {
			continue
		}
		i, ok := byKey[rec.Source]
		if !ok {
			continue
		}
		switch {
		case t.State.Category != rec.After.Category && t.State.Category != 0 && !t.State.Equal(rec.Before):
			if o, seen := overrides[i]; !seen || t.Date.After(o.Date) || t.Date.Equal(o.Date) && t.ID > o.ID {
				overrides[i] = t
			}
		case rs[i].State == Tentative && t.State.Category == rec.After.Category && rec.After.NeedsReview && !t.State.NeedsReview:
			accepted[i] = true
		}
	}
	var v Revision
	edits := map[int]Edit{}
	for _, i := range slices.Sorted(maps.Keys(overrides)) {
		t := overrides[i]
		title, ok := titles[t.State.Category]
		if !ok {
			continue
		}
		now := rs[i]
		now.Category, now.State, now.From = title, Tentative, t.Date
		if t.State.HasLabel(decide.ApplyAll) {
			now.From = time.Time{}
		}
		v.Amended = append(v.Amended, Amendment{Was: rs[i], Now: now, Charge: t})
		edits[i] = Edit{Index: i, Rule: now}
	}
	for i := range accepted {
		if _, amended := edits[i]; !amended {
			r := rs[i]
			r.State = Confirmed
			edits[i] = Edit{Index: i, Rule: r}
		}
	}
	for _, i := range slices.Sorted(maps.Keys(edits)) {
		v.Edits = append(v.Edits, edits[i])
	}
	return v
}
