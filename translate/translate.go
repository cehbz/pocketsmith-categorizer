// Package translate turns a PocketSmith category-rules export into categorizer rules.
package translate

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

// ExportRule is one PocketSmith category rule: a payee keyword and a category title.
type ExportRule struct {
	PayeeMatches string
	Category     string
}

// ReadExport decodes PocketSmith's category-rules JSON.
func ReadExport(r io.Reader) ([]ExportRule, error) {
	var raw []struct {
		PayeeMatches string `json:"payee_matches"`
		Category     struct {
			Title string `json:"title"`
		} `json:"category"`
	}
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]ExportRule, len(raw))
	for i, x := range raw {
		out[i] = ExportRule{PayeeMatches: x.PayeeMatches, Category: x.Category.Title}
	}
	return out, nil
}

// Rule is a translated rule as written to the rules file.
type Rule struct {
	Payee    string `toml:"payee"`
	Category string `toml:"category"`
}

// Merge is a keyword several rules shared and the category kept for it.
type Merge struct {
	Pattern    string
	Categories []string
	Chosen     string
}

// Result is a translation and what it dropped and merged.
type Result struct {
	Rules   []Rule
	Dropped []ExportRule
	Merged  []Merge
}

// Translate keeps the export's rules whose keyword appears, ignoring case, in some payee of
// history, as PocketSmith matches them; merges rules sharing a keyword into the category the
// user chose most often for the payees it matches; and orders longer keywords first.
func Translate(export []ExportRule, history []ledger.Transaction, titles map[ledger.CategoryID]string) Result {
	var r Result
	type group struct {
		pattern string
		cats    []string
	}
	groups := map[string]*group{}
	var order []string
	for _, e := range export {
		k := strings.ToUpper(strings.TrimSpace(e.PayeeMatches))
		if !matchesAny(k, history) {
			r.Dropped = append(r.Dropped, e)
			continue
		}
		g := groups[k]
		if g == nil {
			g = &group{pattern: strings.TrimSpace(e.PayeeMatches)}
			groups[k] = g
			order = append(order, k)
		}
		if !slices.Contains(g.cats, e.Category) {
			g.cats = append(g.cats, e.Category)
		}
	}
	slices.SortStableFunc(order, func(a, b string) int { return len(b) - len(a) })
	for _, k := range order {
		g := groups[k]
		chosen := g.cats[0]
		if len(g.cats) > 1 {
			chosen = mostChosen(k, g.cats, history, titles)
			r.Merged = append(r.Merged, Merge{Pattern: g.pattern, Categories: g.cats, Chosen: chosen})
		}
		r.Rules = append(r.Rules, Rule{Payee: "(?i)" + regexp.QuoteMeta(g.pattern), Category: chosen})
	}
	return r
}

func matchesAny(upper string, history []ledger.Transaction) bool {
	for _, t := range history {
		if strings.Contains(strings.ToUpper(t.Payee), upper) {
			return true
		}
	}
	return false
}

// mostChosen is the candidate the user chose most often for payees containing upper; ties
// keep the earlier candidate.
func mostChosen(upper string, candidates []string, history []ledger.Transaction, titles map[ledger.CategoryID]string) string {
	counts := map[string]int{}
	for _, t := range history {
		if t.UserDecided() && strings.Contains(strings.ToUpper(t.Payee), upper) {
			counts[titles[t.State.Category]]++
		}
	}
	best := candidates[0]
	for _, c := range candidates[1:] {
		if counts[c] > counts[best] {
			best = c
		}
	}
	return best
}

// WriteRules writes rs as a rules file.
func WriteRules(w io.Writer, rs []Rule) error {
	return toml.NewEncoder(w).Encode(struct {
		Rule []Rule `toml:"rule"`
	}{rs})
}

// WriteReport prints what the translation kept, dropped and merged.
func WriteReport(w io.Writer, r Result) error {
	if _, err := fmt.Fprintf(w, "%d rules kept, %d dropped with no match, %d duplicate patterns merged\n", len(r.Rules), len(r.Dropped), len(r.Merged)); err != nil {
		return err
	}
	for _, d := range r.Dropped {
		if _, err := fmt.Fprintf(w, "dropped: %s (%s)\n", d.PayeeMatches, d.Category); err != nil {
			return err
		}
	}
	for _, m := range r.Merged {
		if _, err := fmt.Fprintf(w, "merged: %s %v -> %s\n", m.Pattern, m.Categories, m.Chosen); err != nil {
			return err
		}
	}
	return nil
}
