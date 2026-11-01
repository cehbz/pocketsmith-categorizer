// Package rules is the categorizer's explicit rules layer.
package rules

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/cehbz/pocketsmith-categorizer/decide"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

// Label marks the rules layer's decisions.
const Label = "auto-rule"

// Rule gives a category to the transactions that meet all its conditions.
type Rule struct {
	Payee    string  `toml:"payee"`    // Go regular expression matched against the payee
	Account  string  `toml:"account"`  // account name
	Type     string  `toml:"type"`     // "debit" or "credit"
	Min      float64 `toml:"min"`      // smallest absolute amount
	Max      float64 `toml:"max"`      // largest absolute amount; zero means no limit
	Category string  `toml:"category"` // category title

	payee *regexp.Regexp
	id    ledger.CategoryID
}

// Rules is an ordered rule list; the first match decides.
type Rules []Rule

type file struct {
	Rule []Rule `toml:"rule"`
}

// Load reads a rules file and checks every rule, resolving category titles against categories.
func Load(path string, categories []ledger.Category) (Rules, error) {
	var f file
	md, err := toml.DecodeFile(path, &f)
	if err != nil {
		return nil, fmt.Errorf("rules %s: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, k := range undecoded {
			keys[i] = k.String()
		}
		return nil, fmt.Errorf("rules %s: unknown keys %s", path, strings.Join(keys, ", "))
	}
	ids := make(map[string]ledger.CategoryID, len(categories))
	for _, c := range categories {
		ids[c.Title] = c.ID
	}
	for i := range f.Rule {
		if err := f.Rule[i].check(ids); err != nil {
			return nil, fmt.Errorf("rules %s: rule %d: %w", path, i+1, err)
		}
	}
	return Rules(f.Rule), nil
}

func (r *Rule) check(ids map[string]ledger.CategoryID) error {
	if r.Payee == "" && r.Account == "" {
		return errors.New("needs a payee or an account")
	}
	if r.Payee != "" {
		re, err := regexp.Compile(r.Payee)
		if err != nil {
			return err
		}
		r.payee = re
	}
	switch r.Type {
	case "", "debit", "credit":
	default:
		return fmt.Errorf("type %q is neither debit nor credit", r.Type)
	}
	if r.Min < 0 || r.Max < 0 || r.Max != 0 && r.Min > r.Max {
		return fmt.Errorf("amount range %v to %v", r.Min, r.Max)
	}
	id, ok := ids[r.Category]
	if !ok {
		return fmt.Errorf("category %q is not a PocketSmith category", r.Category)
	}
	r.id = id
	return nil
}

// Matches reports whether t meets every condition of r.
func (r Rule) Matches(t ledger.Transaction) bool {
	if r.payee != nil && !r.payee.MatchString(t.Payee) {
		return false
	}
	if r.Account != "" && r.Account != t.Account.Name {
		return false
	}
	if r.Type == "debit" && t.Amount >= 0 || r.Type == "credit" && t.Amount <= 0 {
		return false
	}
	a := math.Abs(t.Amount)
	return a >= r.Min && (r.Max == 0 || a <= r.Max)
}

// Key identifies the rule by its conditions and category; it changes when the rule does.
func (r Rule) Key() string {
	return fmt.Sprintf("payee=%q account=%q type=%q min=%v max=%v category=%q", r.Payee, r.Account, r.Type, r.Min, r.Max, r.Category)
}

// CategoryID is the PocketSmith category the rule gives.
func (r Rule) CategoryID() ledger.CategoryID { return r.id }

// Match returns the first rule that t meets.
func (rs Rules) Match(t ledger.Transaction) (Rule, bool) {
	for _, r := range rs {
		if r.Matches(t) {
			return r, true
		}
	}
	return Rule{}, false
}

// Decide is the rules layer's decision for t.
func (rs Rules) Decide(t ledger.Transaction) (decide.Decision, bool) {
	r, ok := rs.Match(t)
	if !ok {
		return decide.Decision{}, false
	}
	return decide.Decision{Category: r.id, Label: Label, Source: r.Key()}, true
}

// Stats is how a rule's decisions compare with the user's categorizations.
type Stats struct {
	Rule     Rule
	Matches  int
	Agree    int
	Disagree []ledger.Transaction
}

// Replay compares each of the user's categorizations in txs with the first rule that matches
// it, returning one entry per rule that matched any, in rule order.
func (rs Rules) Replay(txs []ledger.Transaction) []Stats {
	stats := make([]Stats, len(rs))
	for i, r := range rs {
		stats[i].Rule = r
	}
	for _, t := range txs {
		if !t.UserDecided() {
			continue
		}
		for i, r := range rs {
			if !r.Matches(t) {
				continue
			}
			stats[i].Matches++
			if r.id == t.State.Category {
				stats[i].Agree++
			} else {
				stats[i].Disagree = append(stats[i].Disagree, t)
			}
			break
		}
	}
	var out []Stats
	for _, s := range stats {
		if s.Matches > 0 {
			out = append(out, s)
		}
	}
	return out
}
