// Package learn proposes rules from the user's own categorizations and backtests how much
// evidence a proposal needs.
package learn

import (
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

// PayeeKey is the first two words of a payee: "GOOGLE *FI AbCDE1" is "GOOGLE FI". A payee
// with no letters has an empty key.
func PayeeKey(payee string) string { return key(words(payee), 2) }

// words are the runs of letters in payee, upper-cased.
func words(payee string) []string {
	return strings.FieldsFunc(strings.ToUpper(payee), func(r rune) bool { return r < 'A' || r > 'Z' })
}

// key is the first n of ws.
func key(ws []string, n int) string { return strings.Join(ws[:min(n, len(ws))], " ") }

// Pattern is a case-insensitive payee pattern for key's words, in order, each a whole word of
// letters: a digit ends a word, as it does for a key.
func Pattern(key string) string {
	words := strings.Fields(key)
	for i, w := range words {
		words[i] = regexp.QuoteMeta(w)
	}
	return `(?i)(?:^|[^A-Za-z])` + strings.Join(words, `[^A-Za-z]*`) + `(?:[^A-Za-z]|$)`
}

// Proposal is a rule learned from the user's categorizations of one payee.
type Proposal struct {
	Key      string // payee key
	Category ledger.CategoryID
	Count    int    // the user's categorizations behind it
	Pattern  string // payee pattern for the rule
}

// Propose returns a proposal for each payee key whose user categorizations in txs are
// unanimous and number least or more, and at least minCategorizations, unless covered reports
// that a layer already decides one of them. A key is a payee's first two words, lengthened a word at a time while the
// categorizations behind it disagree. A proposal is dropped when its key has fewer than 4
// letters or its pattern matches a transaction in txs categorized otherwise, whoever
// categorized it. Proposals are sorted by key.
func Propose(txs []ledger.Transaction, least int, covered func(ledger.Transaction) bool) []Proposal {
	var decided []judged
	for _, t := range txs {
		if !t.UserDecided() {
			continue
		}
		if ws := words(t.Payee); len(ws) > 0 {
			decided = append(decided, judged{t, ws})
		}
	}
	out := propose(decided, 2, max(least, minCategorizations), covered)
	out = slices.DeleteFunc(out, func(p Proposal) bool { return broad(p, txs) })
	slices.SortFunc(out, func(a, b Proposal) int { return strings.Compare(a.Key, b.Key) })
	return out
}

// minCategorizations is the fewest categorizations a proposal needs: the backtest scores only
// payees that come back, so it can't see that a payee categorized once may never recur.
const minCategorizations = 2

// minKeyLetters is the fewest letters a proposal's key needs: a shorter one matches too much.
const minKeyLetters = 4

// broad reports whether p would match too much: its key is short, or its pattern matches a
// transaction in txs, whoever categorized it, in a category other than p's.
func broad(p Proposal, txs []ledger.Transaction) bool {
	if len(strings.ReplaceAll(p.Key, " ", "")) < minKeyLetters {
		return true
	}
	re := regexp.MustCompile(p.Pattern)
	return slices.ContainsFunc(txs, func(t ledger.Transaction) bool {
		return t.State.Category != 0 && t.State.Category != p.Category && re.MatchString(t.Payee)
	})
}

// judged is a user categorization with its payee's words.
type judged struct {
	t     ledger.Transaction
	words []string
}

// propose groups txs by their first n words, proposes each group that agrees and splits each
// group that disagrees by its next word.
func propose(txs []judged, n, least int, covered func(ledger.Transaction) bool) []Proposal {
	groups := map[string][]judged{}
	for _, j := range txs {
		k := key(j.words, n)
		groups[k] = append(groups[k], j)
	}
	var out []Proposal
	for k, g := range groups {
		cat := g[0].t.State.Category
		if slices.ContainsFunc(g, func(j judged) bool { return j.t.State.Category != cat }) {
			var longer []judged
			for _, j := range g {
				if len(j.words) > n {
					longer = append(longer, j)
				}
			}
			out = append(out, propose(longer, n+1, least, covered)...)
			continue
		}
		if len(g) < least || slices.ContainsFunc(g, func(j judged) bool { return covered(j.t) }) {
			continue
		}
		out = append(out, Proposal{Key: k, Category: cat, Count: len(g), Pattern: Pattern(k)})
	}
	return out
}

// Trial is how proposals made with one minimum count fared.
type Trial struct {
	Min       int // categorizations a proposal needed
	Evaluated int // later categorizations of proposed payees
	Correct   int // of those, the ones in the proposed category
}

// Backtest makes proposals from the user's categorizations dated before cutoff with each
// minimum count from minCategorizations to maxMin, and scores them against the user's categorizations from
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
	var trials []Trial
	for least := minCategorizations; least <= maxMin; least++ {
		proposed := map[string]ledger.CategoryID{}
		for _, p := range Propose(before, least, func(ledger.Transaction) bool { return false }) {
			proposed[p.Key] = p.Category
		}
		tr := Trial{Min: least}
		for _, t := range after {
			if cat, ok := proposedFor(proposed, words(t.Payee)); ok {
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

// proposedFor is the category proposed for a payee's words. No proposed key is a prefix of
// another, so at most one prefix of ws is proposed.
func proposedFor(proposed map[string]ledger.CategoryID, ws []string) (ledger.CategoryID, bool) {
	for n := 2; n <= max(2, len(ws)); n++ {
		if cat, ok := proposed[key(ws, n)]; ok {
			return cat, true
		}
	}
	return 0, false
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
