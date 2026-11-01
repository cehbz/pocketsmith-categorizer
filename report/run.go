package report

import (
	"fmt"
	"io"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/categorize"
	"github.com/cehbz/pocketsmith-categorizer/decide"
	"github.com/cehbz/pocketsmith-categorizer/internal/day"
	"github.com/cehbz/pocketsmith-categorizer/learn"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
	"github.com/cehbz/pocketsmith-categorizer/rules"
)

// Run is what a categorize run did and found.
type Run struct {
	RunID          string
	Changes        []categorize.Change // planned decisions, applied in order
	Applied        int
	Corrections    []categorize.Change // categorizer labels removed after the user recategorized
	Amendments     []categorize.Change // corrections of a rule's decision: Source is the rule, After the user's category
	Undecided      []ledger.Transaction
	Suggestions    []decide.Suggestion
	Proposals      []learn.Proposal
	Threshold      int
	ThresholdFound bool
	Trials         []learn.Trial
	Titles         map[ledger.CategoryID]string
	Transactions   map[int64]ledger.Transaction // by ID, to describe changes
}

func title(titles map[ledger.CategoryID]string, id ledger.CategoryID) string {
	if id == 0 {
		return "uncategorized"
	}
	if t, ok := titles[id]; ok {
		return t
	}
	return fmt.Sprintf("category %d", id)
}

func describe(t ledger.Transaction) string {
	return fmt.Sprintf("%s %s %.2f %s", day.Format(t.Date), t.Account.Name, t.Amount, t.Payee)
}

// WriteRun prints a run's report.
func WriteRun(w io.Writer, r Run) error {
	p := &printer{w: w}
	p.f("run %s: applied %d of %d changes\n", r.RunID, r.Applied, len(r.Changes))
	p.f("changed (%d):\n", r.Applied)
	for _, c := range r.Changes[:r.Applied] {
		p.f("  %s: %s -> %s (%s)\n", describe(r.Transactions[c.Charge]), title(r.Titles, c.Before.Category), title(r.Titles, c.After.Category), c.Source)
	}
	p.f("your corrections (%d):\n", len(r.Corrections))
	for _, c := range r.Corrections {
		p.f("  %s: you set %s; categorizer label removed\n", describe(r.Transactions[c.Charge]), title(r.Titles, c.After.Category))
	}
	p.f("rule amendments to consider (%d):\n", len(r.Amendments))
	for _, a := range r.Amendments {
		t := r.Transactions[a.Charge]
		p.f("  %s decided %s; you chose %s for %s\n", a.Source, title(r.Titles, ruleCategory(a)), title(r.Titles, a.After.Category), describe(t))
	}
	p.f("still needing review, undecided (%d):\n", len(r.Undecided))
	for _, t := range r.Undecided {
		p.f("  %s (%s)\n", describe(t), title(r.Titles, t.State.Category))
	}
	p.f("suggestions (%d):\n", len(r.Suggestions))
	for _, s := range r.Suggestions {
		p.f("  %s: %s -> %s (%s)\n", describe(s.Transaction), title(r.Titles, s.Transaction.State.Category), title(r.Titles, s.Decision.Category), s.Decision.Source)
	}
	if !r.ThresholdFound {
		p.f("no proposals: the backtest found no evidence count reaching 95%% precision\n")
		return p.err
	}
	var tr learn.Trial
	for _, t := range r.Trials {
		if t.Min == r.Threshold {
			tr = t
		}
	}
	p.f("proposed rules (%d), from %d or more matching categorizations (%d of %d right in backtest):\n", len(r.Proposals), r.Threshold, tr.Correct, tr.Evaluated)
	for _, pr := range r.Proposals {
		p.f("  [[rule]]\n  payee = %q\n  category = %q # %d categorizations of %s\n", pr.Pattern, title(r.Titles, pr.Category), pr.Count, pr.Key)
	}
	return p.err
}

// ruleCategory is the category the categorizer had set before the user's correction.
func ruleCategory(a categorize.Change) ledger.CategoryID { return a.Before.Category }

// Replay is a replay of the rules against the user's categorizations.
type Replay struct {
	From, To time.Time
	Users    int // the user's categorizations in the period
	Stats    []rules.Stats
	Titles   map[ledger.CategoryID]string
}

// WriteReplay prints a replay's report.
func WriteReplay(w io.Writer, r Replay) error {
	p := &printer{w: w}
	matched, agree := 0, 0
	for _, s := range r.Stats {
		matched += s.Matches
		agree += s.Agree
	}
	p.f("replay %s..%s: %d of your categorizations, %d matched a rule, %d agree\n", day.Format(r.From), day.Format(r.To), r.Users, matched, agree)
	for _, s := range r.Stats {
		p.f("rule %s: %d matches, %d agree\n", s.Rule.Key(), s.Matches, s.Agree)
		for _, t := range s.Disagree {
			p.f("  %s: you chose %s, rule says %s\n", describe(t), title(r.Titles, t.State.Category), s.Rule.Category)
		}
	}
	return p.err
}
