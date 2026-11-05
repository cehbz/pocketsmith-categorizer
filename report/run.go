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
	"github.com/cehbz/pocketsmith-categorizer/evidence"
	"github.com/cehbz/pocketsmith-categorizer/internal/day"
	"github.com/cehbz/pocketsmith-categorizer/learn"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
	"github.com/cehbz/pocketsmith-categorizer/rules"
	"github.com/cehbz/pocketsmith-categorizer/trips"
)

// Run is what a categorize run did and found.
type Run struct {
	RunID          string
	At             time.Time           // when the run started, in local time
	Changes        []categorize.Change // planned decisions, applied in order
	Applied        int
	Flags          []categorize.Change // transactions put in review without a decision, applied after Changes
	FlagsApplied   int
	Reasons        map[int64][]string  // why each transaction put in review needs the user
	Income         []attention.Cycle   // income cycles checked
	Tolerance      float64             // income mismatch tolerance
	Corrections    []categorize.Change // categorizer labels removed after the user recategorized
	Amendments     []categorize.Change // corrections of a layer's decision: Source is what decided, Before its category, After the user's
	Undecided      []ledger.Transaction
	Suggestions    []decide.Suggestion
	RulesAdded     []learn.Proposal  // rules the run added to the rules file
	RulesAmended   []rules.Amendment // rules the user's overrides changed
	Threshold      int
	ThresholdFound bool
	Trials         []learn.Trial
	Titles         map[ledger.CategoryID]string
	Transactions   map[int64]ledger.Transaction // by ID, to describe changes
	TripsAdded     []trips.Inferred             // trips the run added to the list, with their evidence
	TripVersions   []trips.Trip                 // listed trips placed under new names or dates
	Hints          []trips.Hint                 // inferred trips reaching beyond trips the user edited
	Receipts       Receipts                     // the receipts the run couldn't read or match
	NewHints       int                          // how many of Hints no earlier run counted
	NewUnreadable  int                          // how many of Receipts.Unreadable no earlier run counted
}

// Receipts are the merchants' receipts a run couldn't use: the messages it couldn't read and
// the receipts no charge paid.
type Receipts struct {
	Unreadable []Unreadable
	Unmatched  []evidence.Receipt
	StaleRates error // why the receipts were matched on kept exchange rates; nil when the rates were fetched
}

// Unreadable is a merchant's message that arrived at Received and couldn't be read, and why;
// ID is the message's.
type Unreadable struct {
	ID       string
	Merchant string
	Received time.Time
	Why      string
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

// WriteRun prints a run's report: what needs the user, then what the run applied, then
// what is there for reference, each section left out when empty.
func WriteRun(w io.Writer, r Run) error { return r.layout().writeText(w) }

// WriteRunHTML prints a run's report as an HTML page: the same sections as WriteRun's, as
// tables.
func WriteRunHTML(w io.Writer, r Run) error { return r.layout().writeHTML(w) }

// layout is r's report laid out for the user.
func (r Run) layout() doc {
	d := doc{header: fmt.Sprintf("Run %s · %d of %d changes applied", r.At.Format("2006-01-02 15:04"), r.Applied+r.FlagsApplied, len(r.Changes)+len(r.Flags))}
	if r.Receipts.StaleRates != nil {
		d.notes = append(d.notes, fmt.Sprintf("Receipts matched on stale rates: %v", r.Receipts.StaleRates))
	}
	byRule, onTrips := r.amendments()
	d.add("Needs You", true, r.newRules(), r.overridden(byRule), r.suggestions(),
		r.tripsAdded(), r.tripHints(), r.tripVersions(), r.unreadable(), r.notApplied(), r.undecided(), r.reviewReasons())
	d.add("Applied", false, r.applied()...)
	d.add("Reference", false, r.tripCorrections(onTrips), r.corrections(), r.income(), r.unmatched())
	return d
}

// amendments are r's amendments split by what decided: the trips layer, or a rule or any
// other layer.
func (r Run) amendments() (byRule, onTrips []categorize.Change) {
	for _, a := range r.Amendments {
		if trips.IsSource(a.Source) {
			onTrips = append(onTrips, a)
		} else {
			byRule = append(byRule, a)
		}
	}
	return byRule, onTrips
}

// date is d as r's report shows it.
func (r Run) date(d time.Time) string { return date(d, r.At.Year()) }

// title is a category's title.
func (r Run) title(id ledger.CategoryID) string { return title(r.Titles, id) }

// move is a change of category, or the category alone when it stays.
func (r Run) move(before, after ledger.CategoryID) string {
	if before == after {
		return r.title(after)
	}
	return r.title(before) + " → " + r.title(after)
}

// charge is the date, payee and amount of transaction id.
func (r Run) charge(id int64) []string {
	t := r.Transactions[id]
	return []string{r.date(t.Date), t.Payee, amount(t.Amount)}
}

// span is a trip's dates.
func (r Run) span(from, to time.Time) string { return r.date(from) + " – " + r.date(to) }

// newRules are the rules the run added to the rules file and those the user's overrides
// amended, with how to amend a rule for all its charges when it lists an amendment.
func (r Run) newRules() section {
	s := section{title: "New Rules to Review"}
	var tr learn.Trial
	for _, t := range r.Trials {
		if t.Min == r.Threshold {
			tr = t
		}
	}
	for _, p := range r.RulesAdded {
		s.parts = append(s.parts, part{line: fmt.Sprintf("Added %s → %s · %s · backtest %d/%d", p.Key, r.title(p.Category), plural(p.Count, "categorization", "categorizations"), tr.Correct, tr.Evaluated)})
	}
	for _, a := range r.RulesAmended {
		scope := "for all charges"
		if !a.Now.From.IsZero() {
			scope = "from " + r.date(a.Now.From)
		}
		s.parts = append(s.parts, part{line: fmt.Sprintf("Amended %s: %s → %s %s · %s", a.Now.Name(), a.Was.Category, a.Now.Category, scope, strings.Join(r.charge(a.Charge.ID), " "))})
	}
	if len(r.RulesAmended) > 0 {
		s.parts = append(s.parts, part{line: "A rule you amend by changing a charge's category changes from that charge's date on; add the label apply-all to the charge as well to change it for all its charges."})
	}
	return s
}

// overridden are the user's corrections of a decision a layer other than the rules and trips made.
func (r Run) overridden(amendments []categorize.Change) section {
	t := table{cols: cols([]string{"Date", "Payee", "Amount", "By", "Decided", "You chose"}, "Amount")}
	for _, a := range amendments {
		rule, ok := rules.KeyName(a.Source)
		if !ok {
			rule = a.Source
		}
		t.rows = append(t.rows, append(r.charge(a.Charge), rule, r.title(a.Before.Category), r.title(a.After.Category)))
	}
	return tabled("Overridden Decisions", t)
}

// suggestions are the user's transactions a layer would categorize differently.
func (r Run) suggestions() section {
	t := table{cols: cols([]string{"Date", "Payee", "Amount", "Category", "By"}, "Amount")}
	for _, s := range r.Suggestions {
		tx := s.Transaction
		t.rows = append(t.rows, []string{r.date(tx.Date), tx.Payee, amount(tx.Amount), r.move(tx.State.Category, s.Decision.Category), decider(s.Decision.Reason, s.Decision.Source)})
	}
	return tabled("Suggestions", t)
}

// decider names what made a decision for reason, or its source when it gives none.
func decider(reason categorize.Reason, source string) string {
	switch reason.Why {
	case categorize.ByReceipt:
		return "receipt " + reason.Receipt
	case categorize.ByRule:
		return "rule " + reason.Rule
	case categorize.OnTrip:
		return tripName(reason.Trip)
	case categorize.Refund:
		return "refund, " + refunded(reason)
	case categorize.OffTrip:
		return "removed from trips"
	}
	return source
}

// refunded names what a refund refunds: a merchant's part, or a trip's charge.
func refunded(reason categorize.Reason) string {
	if reason.Merchant != "" {
		return evidence.Title(reason.Merchant) + " " + reason.Receipt
	}
	return tripName(reason.Trip)
}

// tripsAdded are the trips the run listed, with their evidence.
func (r Run) tripsAdded() section {
	s := section{title: "Trips Added"}
	for _, t := range r.TripsAdded {
		s.parts = append(s.parts, part{line: r.trip(t.Trip)}, r.evidence(t.Evidence))
	}
	return s
}

// tripHints are the trips the user edited that inference reaches beyond.
func (r Run) tripHints() section {
	s := section{title: "Trip Hints"}
	for _, h := range r.Hints {
		s.parts = append(s.parts, part{line: fmt.Sprintf("%s · inferred %s", r.trip(h.Edited), r.span(h.Inferred.From, h.Inferred.To))}, r.evidence(h.Inferred.Evidence))
	}
	return s
}

// tripVersions are listed trips placed under a new name or new dates.
func (r Run) tripVersions() section {
	s := section{title: "Trips Renamed or Redated"}
	for _, t := range r.TripVersions {
		s.parts = append(s.parts, part{line: r.trip(t)})
	}
	return s
}

// trip is a trip's name and dates, and whether it is a visa run.
func (r Run) trip(t trips.Trip) string {
	s := tripName(t.Name) + " · " + r.span(t.From, t.To)
	if t.VisaRun {
		s += " · visa run"
	}
	return s
}

// evidence is a table of a trip's evidence: each sign, what showed it and its days.
func (r Run) evidence(evidence []trips.Evidence) part {
	t := table{cols: make([]column, 4)}
	for _, e := range spans(evidence) {
		when := r.date(e.first)
		if !e.last.Equal(e.first) {
			when = r.span(e.first, e.last)
		}
		t.rows = append(t.rows, []string{e.source.String(), e.sign.String(), e.what, when})
	}
	return part{table: &t}
}

// unreadable are the merchants' messages the run couldn't read.
func (r Run) unreadable() section {
	t := table{cols: cols([]string{"Received", "Merchant", "Why"})}
	for _, u := range r.Receipts.Unreadable {
		t.rows = append(t.rows, []string{r.date(u.Received) + u.Received.Format(" 15:04"), evidence.Title(u.Merchant), u.Why})
	}
	return tabled("Unreadable Receipts", t)
}

// notApplied are the changes the run planned and failed to apply.
func (r Run) notApplied() section {
	t := table{cols: cols([]string{"Date", "Payee", "Amount", "Category", "Why"}, "Amount")}
	for _, c := range r.Changes[r.Applied:] {
		t.rows = append(t.rows, append(r.charge(c.Charge), r.move(c.Before.Category, c.After.Category), decider(c.Reason, c.Source)))
	}
	return tabled("Not Applied", t)
}

// undecided are the transactions needing review that no layer decided.
func (r Run) undecided() section {
	t := table{cols: cols([]string{"Date", "Payee", "Account", "Amount", "Category now"}, "Amount")}
	for _, tx := range r.Undecided {
		t.rows = append(t.rows, []string{r.date(tx.Date), tx.Payee, tx.Account.Name, amount(tx.Amount), r.title(tx.State.Category)})
	}
	return tabled("Undecided", t)
}

// reviewReasons are why each transaction put in review needs the user.
func (r Run) reviewReasons() section {
	t := table{cols: []column{{}, {}, {right: true}, {}}}
	for _, id := range byDate(r.Reasons, r.Transactions) {
		phrases := make([]string, len(r.Reasons[id]))
		for i, l := range r.Reasons[id] {
			phrases[i] = phrase(l)
		}
		t.rows = append(t.rows, append(r.charge(id), strings.Join(phrases, ", ")))
	}
	s := tabled("Review Reasons", t)
	s.quiet = true
	return s
}

// applied groups the changes r applied by why they were made.
func (r Run) applied() []section {
	moved := []string{"Date", "Payee", "Amount", "Category"}
	receipts := table{cols: cols([]string{"Date", "Merchant", "Amount", "Category", "Receipt"}, "Amount")}
	confirmed := table{cols: cols(append(moved, "Rule"), "Amount")}
	ruled := table{cols: cols(append(moved, "Rule"), "Amount")}
	placed := table{cols: cols(append(moved, "Trip"), "Amount")}
	removed := table{cols: cols(moved, "Amount")}
	refunds := table{cols: cols(append(moved, "Of"), "Amount")}
	other := table{cols: cols(append(moved, "Source"), "Amount")}
	for _, c := range r.Changes[:r.Applied] {
		row := append(r.charge(c.Charge), r.move(c.Before.Category, c.After.Category))
		switch c.Reason.Why {
		case categorize.ByReceipt:
			t := r.Transactions[c.Charge]
			receipts.rows = append(receipts.rows, []string{r.date(t.Date), evidence.Title(c.Reason.Merchant), amount(t.Amount), r.move(c.Before.Category, c.After.Category), c.Reason.Receipt})
		case categorize.ByRule:
			if c.Before.Category == c.After.Category {
				confirmed.rows = append(confirmed.rows, append(row, c.Reason.Rule))
			} else {
				ruled.rows = append(ruled.rows, append(row, c.Reason.Rule))
			}
		case categorize.OnTrip:
			placed.rows = append(placed.rows, append(row, tripName(c.Reason.Trip)))
		case categorize.OffTrip:
			removed.rows = append(removed.rows, row)
		case categorize.Refund:
			refunds.rows = append(refunds.rows, append(row, refunded(c.Reason)))
		default:
			other.rows = append(other.rows, append(row, c.Source))
		}
	}
	return []section{tabled("Receipts", receipts), tabled("Confirmed by Rule", confirmed), tabled("Rules", ruled),
		tabled("Trips", placed), tabled("Removed from Trips", removed), tabled("Refunds", refunds), tabled("Other", other)}
}

// tripCorrections are the user's corrections of the trips layer's categories.
func (r Run) tripCorrections(amendments []categorize.Change) section {
	t := table{cols: cols([]string{"Date", "Payee", "Amount", "Categorizer", "You"}, "Amount")}
	for _, a := range amendments {
		t.rows = append(t.rows, append(r.charge(a.Charge), r.title(a.Before.Category), r.title(a.After.Category)))
	}
	return tabled("Corrected Trip Categories", t)
}

// corrections are r's corrections not already listed as amendments.
func (r Run) corrections() section {
	listed := map[int64]bool{}
	for _, a := range r.Amendments {
		listed[a.Charge] = true
	}
	t := table{cols: cols([]string{"Date", "Payee", "Amount", "You set"}, "Amount")}
	for _, c := range r.Corrections {
		if !listed[c.Charge] {
			t.rows = append(t.rows, append(r.charge(c.Charge), r.title(c.After.Category)))
		}
	}
	return tabled("Corrections", t)
}

// income is each income cycle checked: what it received and how that compares.
func (r Run) income() section {
	t := table{cols: cols([]string{"Cycle", "Received", "Status"}, "Received")}
	for _, c := range r.Income {
		cycle := c.Month.Format("January")
		if c.Month.Year() != r.At.Year() {
			cycle = c.Month.Format("January 2006")
		}
		status := "awaiting card bill and rent"
		if c.Ready {
			status = fmt.Sprintf("expected %s, off by %s", money(c.Expected), amount(c.Actual-c.Expected))
			if c.Mismatch(r.Tolerance) {
				status += ", mismatch"
			}
		}
		t.rows = append(t.rows, []string{cycle, money(c.Actual), status})
	}
	return tabled("Monthly Income", t)
}

// unmatched are the merchants' receipts no charge paid.
func (r Run) unmatched() section {
	t := table{cols: cols([]string{"Paid", "Merchant", "Kind", "THB", "Receipt"}, "THB")}
	for _, p := range r.Receipts.Unmatched {
		t.rows = append(t.rows, []string{r.date(p.At) + p.At.Format(" 15:04"), evidence.Title(p.Merchant), string(p.Kind()), money(p.THB()), p.Ref()})
	}
	return tabled("Unmatched Receipts", t)
}

// Replay is a replay of the rules against the user's categorizations.
type Replay struct {
	From, To  time.Time
	Users     int // the user's categorizations in the period
	Stats     []rules.Stats
	Titles    map[ledger.CategoryID]string
	Attention *attention.Tally // what attention would have flagged; nil when not replayed
	Tolerance float64          // income mismatch tolerance
	Trips     []trips.Inferred // the trips inference finds over the period, with their evidence
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
	p.f("trips inferred (%d):\n", len(r.Trips))
	for _, t := range r.Trips {
		p.inferred("  "+describeTrip(t.Trip), t.Evidence)
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
		for _, c := range a.Income {
			p.f("  %s\n", cycle(c, r.Tolerance))
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

// cycle describes an income cycle's check, named by its bill's month.
func cycle(c attention.Cycle, tolerance float64) string {
	label := c.Month.Format("2006-01")
	if !c.Ready {
		return fmt.Sprintf("%s: got %.2f, waiting for the card bill and rent", label, c.Actual)
	}
	s := fmt.Sprintf("%s: expected %.2f, got %.2f, off by %.2f", label, c.Expected, c.Actual, c.Actual-c.Expected)
	if c.Mismatch(tolerance) {
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

// describeTrip names a trip with its dates.
func describeTrip(t trips.Trip) string {
	s := fmt.Sprintf("%s %s..%s", t.Name, day.Format(t.From), day.Format(t.To))
	if t.VisaRun {
		s += " (visa run)"
	}
	return s
}

// inferred prints a trip's line and its evidence, in date order, by source, calendar first:
// each sign and what showed it, with its first and last days.
func (p *printer) inferred(line string, evidence []trips.Evidence) {
	p.f("%s\n", line)
	for _, e := range spans(evidence) {
		when := day.Format(e.first)
		if !e.last.Equal(e.first) {
			when += ".." + day.Format(e.last)
		}
		p.f("    %s: %s %s %s\n", e.source, e.sign, e.what, when)
	}
}

// spanned is a sign of a trip, what showed it, and its first and last days.
type spanned struct {
	source      trips.Source
	sign        trips.Sign
	what        string
	first, last time.Time
}

// spans gathers evidence by source, sign and what showed it, in date order, by source,
// calendar first.
func spans(evidence []trips.Evidence) []spanned {
	var out []spanned
	at := map[spanned]int{}
	for _, e := range evidence {
		k := spanned{source: e.Source, sign: e.Sign, what: e.What}
		i, ok := at[k]
		if !ok {
			i = len(out)
			at[k] = i
			out = append(out, spanned{source: e.Source, sign: e.Sign, what: e.What, first: e.Day})
		}
		out[i].last = e.Day
	}
	slices.SortStableFunc(out, func(a, b spanned) int { return cmp.Compare(a.source, b.source) })
	return out
}
