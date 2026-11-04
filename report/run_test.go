package report

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/attention"
	"github.com/cehbz/pocketsmith-categorizer/categorize"
	"github.com/cehbz/pocketsmith-categorizer/decide"
	"github.com/cehbz/pocketsmith-categorizer/grab"
	"github.com/cehbz/pocketsmith-categorizer/internal/day"
	"github.com/cehbz/pocketsmith-categorizer/learn"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
	"github.com/cehbz/pocketsmith-categorizer/rules"
	"github.com/cehbz/pocketsmith-categorizer/trips"
)

var titles = map[ledger.CategoryID]string{3: "Phone", 5: "Food", 9: "Cash"}

func rtx(id int64, payee string, cat ledger.CategoryID) ledger.Transaction {
	d, _ := day.Parse("2026-02-01")
	return ledger.Transaction{ID: id, Date: d, Amount: -12.5, Payee: payee, Account: ledger.Account{Name: "Card"}, State: ledger.State{Category: cat}}
}

func TestWriteReplay(t *testing.T) {
	st := []rules.Stats{{Rule: rules.Rule{Payee: "(?i)acme", Category: "Phone"}, Matches: 2, Agree: 1, Disagree: []ledger.Transaction{rtx(1, "ACME 2", 5)}}}
	from, _ := day.Parse("2025-01-01")
	to, _ := day.Parse("2026-01-01")
	var b strings.Builder
	if err := WriteReplay(&b, Replay{From: from, To: to, Users: 10, Stats: st, Titles: titles}); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		"replay 2025-01-01..2026-01-01: 10 of your categorizations, 2 matched a rule, 1 agree",
		`payee="(?i)acme"`,
		"2 matches, 1 agree",
		"2026-02-01 Card -12.50 ACME 2: you chose Food, rule says Phone",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q:\n%s", want, out)
		}
	}
}

func TestWriteReplayTalliesAttention(t *testing.T) {
	from := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	r := Replay{
		From: from, To: from.AddDate(0, 0, 20), Titles: titles, Tolerance: 1500,
		Attention: &attention.Tally{
			From: from, To: from.AddDate(0, 0, 20),
			Reasons: map[string]int{"attention-large": 4, "attention-undecided": 30},
			Weeks:   []attention.Week{{All: 10, WithoutUndecided: 2}, {All: 20, WithoutUndecided: 4}, {All: 6, WithoutUndecided: 0}},
			Income:  []attention.Cycle{{Month: from, Expected: 7000, Actual: 7100, Ready: true}},
		},
	}
	var b strings.Builder
	if err := WriteReplay(&b, r); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		"attention 2026-05-01..2026-05-21, every transaction judged as new: 36 flagged over 3 weeks",
		"all reasons: 12.0 a week (median 10, max 20)",
		"without undecided: 2.0 a week (median 2, max 4)",
		"attention-large: 4 (1.3 a week)",
		"attention-undecided: 30 (10.0 a week)",
		"2026-05: expected 7000.00, got 7100.00, off by 100.00",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q:\n%s", want, out)
		}
	}
}

func reportTrip(name string, visaRun bool) trips.Trip {
	from, _ := day.Parse("2026-03-10")
	to, _ := day.Parse("2026-03-14")
	return trips.Trip{Name: name, From: from, To: to, VisaRun: visaRun}
}

// reportInferred is reportTrip inferred from a flight out, a stay, a new place charged on two
// days and a flight back.
func reportInferred(name string, visaRun bool) trips.Inferred {
	on := func(d string, source trips.Source, sign trips.Sign, what string) trips.Evidence {
		dd, _ := day.Parse(d)
		return trips.Evidence{Day: dd, Source: source, Sign: sign, What: what}
	}
	return trips.Inferred{Trip: reportTrip(name, visaRun), Evidence: []trips.Evidence{
		on("2026-03-10", trips.FromCalendar, trips.FlightOut, "HMT-FAR ZZ101"),
		on("2026-03-10", trips.FromCalendar, trips.Stay, "Stay at Inn Farcity"),
		on("2026-03-11", trips.FromCard, trips.NewPlace, "NOODLES FARCITY"),
		on("2026-03-11", trips.FromCalendar, trips.Stay, "Stay at Inn Farcity"),
		on("2026-03-13", trips.FromCard, trips.NewPlace, "NOODLES FARCITY"),
		on("2026-03-14", trips.FromCalendar, trips.FlightBack, "FAR-HMT ZZ102"),
	}}
}

const reportEvidence = "    calendar: flight out HMT-FAR ZZ101 2026-03-10\n" +
	"    calendar: stay Stay at Inn Farcity 2026-03-10..2026-03-11\n" +
	"    calendar: flight back FAR-HMT ZZ102 2026-03-14\n" +
	"    card: new place NOODLES FARCITY 2026-03-11..2026-03-13\n"

func TestWriteReplayListsInferredTripsWithTheirEvidence(t *testing.T) {
	var b strings.Builder
	if err := WriteReplay(&b, Replay{Trips: []trips.Inferred{reportInferred("trip-2026-03-10", false)}}); err != nil {
		t.Fatal(err)
	}
	if want := "trips inferred (1):\n  trip-2026-03-10 2026-03-10..2026-03-14\n" + reportEvidence; !strings.Contains(b.String(), want) {
		t.Errorf("report lacks %q:\n%s", want, b.String())
	}
}

var runAt = time.Date(2026, 11, 4, 9, 30, 0, 0, time.UTC)

// write is r's report, with its run time set.
func write(t *testing.T, r Run) string {
	t.Helper()
	r.At = runAt
	if r.Titles == nil {
		r.Titles = titles
	}
	var b strings.Builder
	if err := WriteRun(&b, r); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// body is the body of the report's section titled title with its spacing collapsed, a line
// a row; ok is false when the report has no such section.
func body(out, title string) (string, bool) {
	_, rest, ok := strings.Cut(out, "\n"+title+"\n")
	b, _, _ := strings.Cut(rest, "\n\n")
	lines := strings.Split(strings.TrimSuffix(b, "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.Join(strings.Fields(l), " ")
	}
	return strings.Join(lines, "\n"), ok
}

// sections are the report's group and section titles in order: its lines that follow a
// blank line.
func sections(out string) []string {
	var titles []string
	lines := strings.Split(out, "\n")
	for i := 1; i < len(lines); i++ {
		if lines[i-1] == "" && lines[i] != "" {
			titles = append(titles, lines[i])
		}
	}
	return titles
}

func TestAppliedChangesAreGroupedByWhyTheyWereMade(t *testing.T) {
	change := func(id int64, before, after ledger.CategoryID, reason categorize.Reason) categorize.Change {
		return categorize.Change{Charge: id, Before: ledger.State{Category: before}, After: ledger.State{Category: after}, Source: "s", Reason: reason}
	}
	r := Run{
		Changes: []categorize.Change{
			change(1, 3, 5, categorize.Reason{Why: categorize.OnTrip, Trip: "trip-2026-01-farcity"}),
			change(2, 3, 5, categorize.Reason{Why: categorize.ByReceipt, Booking: "A-TESTRIDE0001"}),
			change(3, 3, 5, categorize.Reason{Why: categorize.Refund, Trip: trips.Booking}),
			change(4, 5, 5, categorize.Reason{Why: categorize.ByRule, Rule: "SHOP"}),
			change(5, 3, 5, categorize.Reason{Why: categorize.OffTrip}),
			change(6, 3, 5, categorize.Reason{}),
			change(7, 3, 5, categorize.Reason{Why: categorize.ByRule, Rule: "CAFE"}),
		},
		Applied: 7,
		Transactions: map[int64]ledger.Transaction{
			1: rtx(1, "HOTEL", 3), 2: rtx(2, "GRAB", 3), 3: rtx(3, "AIRLINE", 3), 4: rtx(4, "SHOP", 5), 5: rtx(5, "NOODLES", 3), 6: rtx(6, "ODD", 3), 7: rtx(7, "CAFE", 3),
		},
	}
	out := write(t, r)
	want := []string{"APPLIED", "Grab Receipts", "Confirmed by Rule", "Rules", "Trips", "Removed from Trips", "Refunds", "Other"}
	if got := sections(out); !slices.Equal(got, want) {
		t.Errorf("sections %q, want %q\n%s", got, want, out)
	}
	for title, row := range map[string]string{
		"Grab Receipts":      "Feb 1 −12.50 Phone → Food A-TESTRIDE0001",
		"Confirmed by Rule":  "Feb 1 SHOP −12.50 Food SHOP",
		"Rules":              "Feb 1 CAFE −12.50 Phone → Food CAFE",
		"Trips":              "Feb 1 HOTEL −12.50 Phone → Food Farcity trip",
		"Removed from Trips": "Feb 1 NOODLES −12.50 Phone → Food",
		"Refunds":            "Feb 1 AIRLINE −12.50 Phone → Food trip booking",
		"Other":              "Feb 1 ODD −12.50 Phone → Food s",
	} {
		if b, _ := body(out, title); !strings.Contains(b, row) {
			t.Errorf("%s lacks %q:\n%s", title, row, out)
		}
	}
}

func TestARuleKeepingTheCategoryConfirmsIt(t *testing.T) {
	r := Run{
		Changes: []categorize.Change{{Charge: 1, Before: ledger.State{Category: 5, NeedsReview: true}, After: ledger.State{Category: 5, Labels: []string{"auto-rule"}},
			Reason: categorize.Reason{Why: categorize.ByRule, Rule: "SHOP"}}},
		Applied:      1,
		Transactions: map[int64]ledger.Transaction{1: rtx(1, "SHOP", 5)},
	}
	out := write(t, r)
	b, ok := body(out, "Confirmed by Rule")
	if !ok || strings.Contains(b, "→") || !strings.Contains(b, "Food SHOP") {
		t.Errorf("want the charge confirmed in Food by rule SHOP:\n%s", out)
	}
	if _, ok := body(out, "Rules"); ok {
		t.Errorf("a confirmation is listed among the rules' changes:\n%s", out)
	}
}

func TestEmptySectionsAreLeftOut(t *testing.T) {
	out := write(t, Run{ThresholdFound: true, Trials: []learn.Trial{{Min: 3, Evaluated: 40, Correct: 39}}, Threshold: 3})
	if want := "Run 2026-11-04 09:30 · 0 of 0 changes applied\n"; out != want {
		t.Errorf("report of an empty run = %q, want %q", out, want)
	}
}

func TestTripCorrectionsAreReferenceListedOnce(t *testing.T) {
	r := Run{
		Corrections: []categorize.Change{
			{Charge: 1, Before: ledger.State{Category: 9, Labels: []string{"auto-trip"}}, After: ledger.State{Category: 9}},
			{Charge: 2, Before: ledger.State{Category: 3, Labels: []string{"auto-rule"}}, After: ledger.State{Category: 3}},
		},
		Amendments:   []categorize.Change{{Charge: 1, Before: ledger.State{Category: 5}, After: ledger.State{Category: 9}, Source: "trips: trip-2026-01-farcity"}},
		Transactions: map[int64]ledger.Transaction{1: rtx(1, "HOTEL", 9), 2: rtx(2, "ATM", 3)},
	}
	out := write(t, r)
	if want := []string{"REFERENCE", "Corrected Trip Categories", "Corrections"}; !slices.Equal(sections(out), want) {
		t.Errorf("sections %q, want %q\n%s", sections(out), want, out)
	}
	if b, _ := body(out, "Corrected Trip Categories"); !strings.Contains(b, "Feb 1 HOTEL −12.50 Food Cash") {
		t.Errorf("want the hotel's correction from Food to Cash:\n%s", out)
	}
	if b, _ := body(out, "Corrections"); strings.Contains(b, "HOTEL") || !strings.Contains(b, "Feb 1 ATM −12.50 Phone") {
		t.Errorf("want the ATM's correction alone:\n%s", out)
	}
	if subject, due := Notice(r); due {
		t.Errorf("Notice = %q, true; want no notice for a trip correction", subject)
	}
}

func TestARuleAmendmentNeedsTheUser(t *testing.T) {
	rule := rules.Rule{Payee: "(?i)^atm", Category: "Food"}
	r := Run{
		Amendments:   []categorize.Change{{Charge: 2, Before: ledger.State{Category: 5}, After: ledger.State{Category: 9}, Source: rule.Key()}},
		Transactions: map[int64]ledger.Transaction{2: rtx(2, "ATM", 9)},
	}
	out := write(t, r)
	b, ok := body(out, "Rule Amendments")
	if !ok || !strings.Contains(b, "Feb 1 ATM −12.50 ^atm Food Cash") {
		t.Errorf("want rule ^atm's Food amended to Cash:\n%s", out)
	}
	if subject, due := Notice(r); !due || subject != "Categorizer: 1 rule amendment" {
		t.Errorf("Notice = %q, %v; want Categorizer: 1 rule amendment", subject, due)
	}
}

func TestReviewReasonsReadAsPhrases(t *testing.T) {
	r := Run{
		Flags:        []categorize.Change{{Charge: 2, Before: ledger.State{Category: 9}, After: ledger.State{Category: 9, NeedsReview: true}}},
		FlagsApplied: 1,
		Reasons:      map[int64][]string{2: {attention.NewPayee, attention.Large}},
		Transactions: map[int64]ledger.Transaction{2: rtx(2, "ATM", 9)},
	}
	out := write(t, r)
	if b, _ := body(out, "Review Reasons"); b != "Feb 1 ATM −12.50 new payee, larger than usual" {
		t.Errorf("Review Reasons = %q\n%s", b, out)
	}
	if !strings.HasPrefix(out, "Run 2026-11-04 09:30 · 1 of 1 changes applied\n") {
		t.Errorf("header counts the flag:\n%s", out)
	}
}

var update = flag.Bool("update", false, "rewrite the golden reports")

// golden compares got with the golden file name in testdata, rewriting it under -update.
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("report differs from %s:\n%s", path, got)
	}
}

// fixtureRun is a run with something in every section.
func fixtureRun() Run {
	cats := map[ledger.CategoryID]string{1: "Groceries", 2: "Eating Out", 3: "Rides", 4: "Discretionary", 5: "Trip Food", 6: "Trip Lodging", 7: "Trip Transport", 8: "Monthly Income"}
	on := func(d string) time.Time {
		dd, err := day.Parse(d)
		if err != nil {
			panic(err)
		}
		return dd
	}
	card, checking := ledger.Account{Name: "Card"}, ledger.Account{Name: "Checking"}
	txs := map[int64]ledger.Transaction{}
	tx := func(id int64, d string, a float64, payee string, account ledger.Account, cat ledger.CategoryID) ledger.Transaction {
		t := ledger.Transaction{ID: id, Date: on(d), Amount: a, Payee: payee, Account: account, State: ledger.State{Category: cat}}
		txs[id] = t
		return t
	}
	change := func(id int64, after ledger.CategoryID, reason categorize.Reason) categorize.Change {
		return categorize.Change{Charge: id, Before: txs[id].State, After: ledger.State{Category: after}, Source: "source", Reason: reason}
	}
	tx(101, "2026-11-03", -4.2, "GRAB RIDES", card, 2)
	tx(102, "2026-11-02", -15.75, "GRAB RIDES", card, 2)
	tx(103, "2026-10-30", -23, "CORNER BOOKS", card, 4)
	tx(104, "2025-01-15", 1250, "SPARKLE MARKET", card, 1)
	tx(105, "2026-10-12", -88.4, "HARBOUR HOTEL", card, 4)
	tx(106, "2026-09-20", 12, "LANTERN CAFE", card, 5)
	tx(107, "2026-10-20", 88.4, "HARBOUR HOTEL", card, 4)
	tx(108, "2026-10-25", 300, "FERRY LINE", card, 7)
	tx(109, "2026-10-01", -8.65, "MYSTERY SHOP", card, 4)
	tx(110, "2026-11-01", -64.8, "PET CLINIC", card, 0)
	tx(111, "2026-11-04", -35.5, "NEW DINER", card, 2)
	undecided := tx(112, "2026-10-05", -42, "QUIET SHOP", checking, 4)
	suggested := tx(113, "2026-08-08", -19.5, "CORNER BOOKS", card, 1)
	tx(114, "2026-09-14", -18, "BOOK NOOK", card, 1)
	tx(115, "2026-10-14", -11.3, "SEASIDE TAXI", card, 7)
	tx(116, "2026-09-02", -7.25, "OLD MARKET", card, 1)
	seaside := "trip-2026-10-seaside"
	return Run{
		RunID: "20261104T093000.000000000Z", At: runAt, Titles: cats, Transactions: txs,
		Changes: []categorize.Change{
			change(101, 3, categorize.Reason{Why: categorize.ByReceipt, Booking: "A-FAKE0001"}),
			change(102, 2, categorize.Reason{Why: categorize.ByReceipt, Booking: "A-FAKE0002"}),
			change(103, 4, categorize.Reason{Why: categorize.ByRule, Rule: "CORNER BOOKS"}),
			change(104, 4, categorize.Reason{Why: categorize.ByRule, Rule: "SPARKLE"}),
			change(105, 6, categorize.Reason{Why: categorize.OnTrip, Trip: seaside}),
			change(106, 2, categorize.Reason{Why: categorize.OffTrip}),
			change(107, 6, categorize.Reason{Why: categorize.Refund, Trip: seaside}),
			change(108, 7, categorize.Reason{Why: categorize.Refund, Trip: trips.Booking}),
			change(109, 4, categorize.Reason{}),
			change(110, 4, categorize.Reason{Why: categorize.ByRule, Rule: "PET CLINIC"}),
		},
		Applied:      9,
		Flags:        []categorize.Change{{Charge: 111, Before: txs[111].State, After: ledger.State{Category: 2, NeedsReview: true}}},
		FlagsApplied: 1,
		Reasons:      map[int64][]string{111: {attention.NewPayee, attention.Large}, 105: {trips.Placed}},
		Income: []attention.Cycle{
			{Month: on("2026-10-01"), Expected: 5000, Actual: 5400, Ready: true},
			{Month: on("2026-11-01"), Actual: 5100},
		},
		Tolerance: 300,
		Corrections: []categorize.Change{
			{Charge: 114, Before: ledger.State{Category: 1, Labels: []string{"auto-rule"}}, After: ledger.State{Category: 1}},
			{Charge: 115, Before: ledger.State{Category: 7, Labels: []string{"auto-trip"}}, After: ledger.State{Category: 7}},
			{Charge: 116, Before: ledger.State{Category: 1, Labels: []string{"auto-rule"}}, After: ledger.State{Category: 1}},
		},
		Amendments: []categorize.Change{
			{Charge: 114, Before: ledger.State{Category: 4}, After: ledger.State{Category: 1}, Source: rules.Rule{Payee: "(?i)BOOK NOOK", Category: "Discretionary"}.Key()},
			{Charge: 115, Before: ledger.State{Category: 6}, After: ledger.State{Category: 7}, Source: "trips: " + seaside},
		},
		Undecided:      []ledger.Transaction{undecided},
		Suggestions:    []decide.Suggestion{{Transaction: suggested, Decision: decide.Decision{Category: 4, Reason: categorize.Reason{Why: categorize.ByRule, Rule: "CORNER BOOKS"}}}},
		Proposals:      []learn.Proposal{{Key: "TEA HOUSE", Category: 2, Count: 3, Pattern: learn.Pattern("TEA HOUSE")}},
		Threshold:      3,
		ThresholdFound: true,
		Trials:         []learn.Trial{{Min: 2, Evaluated: 60, Correct: 50}, {Min: 3, Evaluated: 50, Correct: 48}},
		TripsAdded:     []trips.Inferred{reportInferred("trip-2026-03-10", true)},
		TripVersions:   []trips.Trip{reportTrip("trip-2026-03-farcity", false)},
		Hints:          []trips.Hint{{Edited: reportTrip("trip-2026-03-farcity", false), Inferred: reportInferred("trip-2026-03-10", false)}},
		Receipts: Receipts{
			Unreadable: []Unreadable{{ID: "m1", Received: time.Date(2026, 11, 2, 6, 19, 0, 0, time.UTC), Why: "no total"}},
			Unmatched:  []grab.Payment{{Booking: "A-FAKE0003", Service: "Standard", THB: 83, At: time.Date(2026, 11, 1, 13, 5, 0, 0, time.UTC), Account: "rider@example.com"}},
			StaleRates: errors.New("rates kept through 2026-11-03: fx: 503"),
		},
	}
}

func TestWriteRunGolden(t *testing.T) {
	var b strings.Builder
	if err := WriteRun(&b, fixtureRun()); err != nil {
		t.Fatal(err)
	}
	golden(t, "run.txt", b.String())
}

func TestTheHTMLReportTintsWhatNeedsTheUserAndMarksCredits(t *testing.T) {
	refund := rtx(2, "A&B <SHOP>", 3)
	refund.Amount = 7.3
	r := Run{
		At: runAt, Titles: titles,
		Changes:      []categorize.Change{{Charge: 1, Before: ledger.State{Category: 3}, After: ledger.State{Category: 5}, Reason: categorize.Reason{Why: categorize.ByRule, Rule: "CAFE"}}},
		Applied:      1,
		Undecided:    []ledger.Transaction{refund},
		Reasons:      map[int64][]string{2: {attention.NewPayee}},
		Transactions: map[int64]ledger.Transaction{1: rtx(1, "CAFE", 3), 2: refund},
	}
	var b strings.Builder
	if err := WriteRunHTML(&b, r); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		"<title>Run 2026-11-04 09:30 · 1 of 1 changes applied</title>",
		`<h2 class="needs">Needs You</h2>`,
		"<div class=\"box\">\n<h3>Undecided</h3>",
		"<h3>Review Reasons</h3>\n<table class=\"muted\">",
		"<td>A&amp;B &lt;SHOP&gt;</td>",
		`<td class="amt credit">+7.30</td>`,
		`<th class="amt">Amount</th>`,
		"<h2>Applied</h2>",
		`<td>Phone <span class="arrow">→</span> Food</td>`,
		`<td class="wide muted">CAFE</td>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("HTML report lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "<div class=\"box\">\n<h3>Review Reasons</h3>") {
		t.Errorf("Review Reasons is tinted; want it low-key:\n%s", out)
	}
}

func TestWriteRunHTMLGolden(t *testing.T) {
	var b strings.Builder
	if err := WriteRunHTML(&b, fixtureRun()); err != nil {
		t.Fatal(err)
	}
	golden(t, "run.html", b.String())
}
