// Package learn proposes rules from the user's own categorizations and backtests how much
// evidence a proposal needs.
package learn

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

// PayeeKey is the first two runs of letters in a payee, upper-cased: "GOOGLE *FI LzNRQ8" is
// "GOOGLE FI". A payee with no letters has an empty key.
func PayeeKey(payee string) string {
	words := strings.FieldsFunc(strings.ToUpper(payee), func(r rune) bool { return r < 'A' || r > 'Z' })
	if len(words) > 2 {
		words = words[:2]
	}
	return strings.Join(words, " ")
}

// Pattern is a case-insensitive payee pattern for key's words, in order, each a whole word.
func Pattern(key string) string {
	words := strings.Fields(key)
	for i, w := range words {
		words[i] = regexp.QuoteMeta(w)
	}
	return `(?i)\b` + strings.Join(words, `[^A-Za-z]*`) + `\b`
}

// Proposal is a rule learned from the user's categorizations of one payee.
type Proposal struct {
	Key      string // payee key
	Category ledger.CategoryID
	Count    int    // the user's categorizations behind it
	Pattern  string // payee pattern for the rule
}

// ID identifies the proposal for remembering it was shown.
func (p Proposal) ID() string { return fmt.Sprintf("propose %s %d", p.Key, p.Category) }

// Propose returns a proposal for each payee key whose user categorizations in txs are
// unanimous and at least min in number, unless covered reports that a layer already decides
// one of them. Proposals are sorted by key.
func Propose(txs []ledger.Transaction, min int, covered func(ledger.Transaction) bool) []Proposal {
	type tally struct {
		cats    map[ledger.CategoryID]int
		covered bool
	}
	by := map[string]*tally{}
	for _, t := range txs {
		if !t.UserDecided() {
			continue
		}
		k := PayeeKey(t.Payee)
		if k == "" {
			continue
		}
		tl := by[k]
		if tl == nil {
			tl = &tally{cats: map[ledger.CategoryID]int{}}
			by[k] = tl
		}
		tl.cats[t.State.Category]++
		if covered(t) {
			tl.covered = true
		}
	}
	var out []Proposal
	for k, tl := range by {
		if tl.covered || len(tl.cats) != 1 {
			continue
		}
		for cat, n := range tl.cats {
			if n >= min {
				out = append(out, Proposal{Key: k, Category: cat, Count: n, Pattern: Pattern(k)})
			}
		}
	}
	slices.SortFunc(out, func(a, b Proposal) int { return strings.Compare(a.Key, b.Key) })
	return out
}

// Trial is how proposals made with one minimum count fared.
type Trial struct {
	Min       int // categorizations a proposal needed
	Evaluated int // later categorizations of proposed payees
	Correct   int // of those, the ones in the proposed category
}

// Backtest makes proposals from the user's categorizations dated before cutoff with each
// minimum count from 1 to maxMin, and scores them against the user's categorizations from
// cutoff on.
func Backtest(txs []ledger.Transaction, cutoff time.Time, maxMin int) []Trial {
	var before, after []ledger.Transaction
	for _, t := range txs {
		switch {
		case !t.UserDecided():
		case t.Date.Before(cutoff):
			before = append(before, t)
		default:
			after = append(after, t)
		}
	}
	trials := make([]Trial, 0, maxMin)
	for min := 1; min <= maxMin; min++ {
		proposed := map[string]ledger.CategoryID{}
		for _, p := range Propose(before, min, func(ledger.Transaction) bool { return false }) {
			proposed[p.Key] = p.Category
		}
		tr := Trial{Min: min}
		for _, t := range after {
			if cat, ok := proposed[PayeeKey(t.Payee)]; ok {
				tr.Evaluated++
				if cat == t.State.Category {
					tr.Correct++
				}
			}
		}
		trials = append(trials, tr)
	}
	return trials
}

// Threshold returns the smallest minimum count whose trial reached target precision over at
// least minEvaluated categorizations.
func Threshold(trials []Trial, target float64, minEvaluated int) (int, bool) {
	for _, tr := range trials {
		if tr.Evaluated >= minEvaluated && float64(tr.Correct) >= target*float64(tr.Evaluated) {
			return tr.Min, true
		}
	}
	return 0, false
}
