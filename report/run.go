package report

import (
	"cmp"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/attention"
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
	Flags          []categorize.Change // transactions put in review without a decision, applied after Changes
	FlagsApplied   int
	Reasons        map[int64][]string  // why each transaction put in review needs the user
	Income         []attention.Month   // income months checked
	Tolerance      float64             // income mismatch tolerance
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
	p.f("run %s: applied %d of %d changes\n", r.RunID, r.Applied+r.FlagsApplied, len(r.Changes)+len(r.Flags))
	p.f("changed (%d):\n", r.Applied)
	for _, c := range r.Changes[:r.Applied] {
		p.f("  %s: %s -> %s (%s)\n", describe(r.Transactions[c.Charge]), title(r.Titles, c.Before.Category), title(r.Titles, c.After.Category), c.Source)
	}
	p.f("needing your attention (%d):\n", len(r.Reasons))
	for _, id := range byDate(r.Reasons, r.Transactions) {
		p.f("  %s: %s\n", describe(r.Transactions[id]), strings.Join(r.Reasons[id], ", "))
	}
	p.f("monthly income (%d months):\n", len(r.Income))
	for _, m := range r.Income {
		p.f("  %s\n", month(m, r.Tolerance))
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
	From, To  time.Time
	Users     int // the user's categorizations in the period
	Stats     []rules.Stats
	Titles    map[ledger.CategoryID]string
	Attention *attention.Tally // what attention would have flagged; nil when not replayed
	Tolerance float64          // income mismatch tolerance
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
	if a := r.Attention; a != nil {
		all, without := make([]int, len(a.Weeks)), make([]int, len(a.Weeks))
		total := 0
		for i, w := range a.Weeks {
			all[i], without[i] = w.All, w.WithoutUndecided
			total += w.All
		}
		p.f("attention %s..%s, every transaction judged as new: %d flagged over %d weeks\n", day.Format(a.From), day.Format(a.To), total, len(a.Weeks))
		p.f("  all reasons: %s\n", weekly(all))
		p.f("  without undecided: %s\n", weekly(without))
		for _, reason := range slices.Sorted(maps.Keys(a.Reasons)) {
			p.f("  %s: %d (%.1f a week)\n", reason, a.Reasons[reason], float64(a.Reasons[reason])/float64(len(a.Weeks)))
		}
		for _, m := range a.Income {
			p.f("  %s\n", month(m, r.Tolerance))
		}
	}
	return p.err
}

// byDate orders the transactions with reasons by date, then ID.
func byDate(reasons map[int64][]string, txs map[int64]ledger.Transaction) []int64 {
	ids := slices.Collect(maps.Keys(reasons))
	slices.SortFunc(ids, func(a, b int64) int {
		if c := txs[a].Date.Compare(txs[b].Date); c != 0 {
			return c
		}
		return cmp.Compare(a, b)
	})
	return ids
}

// month describes an income month's check.
func month(m attention.Month, tolerance float64) string {
	label := m.Start.Format("2006-01")
	if !m.Ready {
		return fmt.Sprintf("%s: got %.2f, waiting for the card bill", label, m.Actual)
	}
	s := fmt.Sprintf("%s: expected %.2f, got %.2f, off by %.2f", label, m.Expected, m.Actual, m.Actual-m.Expected)
	if m.Mismatch(tolerance) {
		s += ": mismatch"
	}
	return s
}

// weekly summarizes counts per week.
func weekly(counts []int) string {
	if len(counts) == 0 {
		return "no weeks"
	}
	sorted := slices.Sorted(slices.Values(counts))
	total := 0
	for _, c := range counts {
		total += c
	}
	return fmt.Sprintf("%.1f a week (median %d, max %d)", float64(total)/float64(len(counts)), sorted[len(sorted)/2], sorted[len(sorted)-1])
}
