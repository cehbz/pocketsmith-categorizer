package report

import (
	"strings"
	"testing"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/attention"
	"github.com/cehbz/pocketsmith-categorizer/categorize"
	"github.com/cehbz/pocketsmith-categorizer/decide"
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

func TestWriteRunListsEverySection(t *testing.T) {
	r := Run{
		RunID:       "RUN1",
		Changes:     []categorize.Change{{Charge: 1, Before: ledger.State{Category: 3, NeedsReview: true}, After: ledger.State{Category: 5, Labels: []string{"auto-rule"}}, Source: "rule-a"}},
		Applied:     1,
		Corrections: []categorize.Change{{Charge: 2, Before: ledger.State{Category: 9, Labels: []string{"auto-rule"}}, After: ledger.State{Category: 9}}},
		Amendments:  []categorize.Change{{Charge: 2, Before: ledger.State{Category: 5}, After: ledger.State{Category: 9}, Source: "rule-b"}},
		Undecided:   []ledger.Transaction{rtx(3, "NEW PLACE", 0)},
		Suggestions: []decide.Suggestion{{Transaction: rtx(4, "OLD PLACE", 3), Decision: decide.Decision{Category: 5, Source: "rule-c"}}},
		Proposals:   []learn.Proposal{{Key: "CAFE ONE", Category: 5, Count: 4, Pattern: learn.Pattern("CAFE ONE")}},
		Threshold:   3, ThresholdFound: true,
		Trials:       []learn.Trial{{Min: 3, Evaluated: 40, Correct: 39}},
		Titles:       titles,
		Transactions: map[int64]ledger.Transaction{1: rtx(1, "SHOP", 3), 2: rtx(2, "ATM", 9)},
	}
	var b strings.Builder
	if err := WriteRun(&b, r); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		"run RUN1: applied 1 of 1 changes",
		"2026-02-01 Card -12.50 SHOP: Phone -> Food (rule-a)",
		"2026-02-01 Card -12.50 ATM: you set Cash; categorizer label removed",
		"rule-b decided Food; you chose Cash for 2026-02-01 Card -12.50 ATM",
		"2026-02-01 Card -12.50 NEW PLACE (uncategorized)",
		"2026-02-01 Card -12.50 OLD PLACE: Phone -> Food (rule-c)",
		"proposed rules (1), from 3 or more matching categorizations (39 of 40 right in backtest):",
		`payee = "(?i)\\bCAFE[^A-Za-z]*ONE\\b"`,
		`category = "Food" # 4 categorizations of CAFE ONE`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q:\n%s", want, out)
		}
	}
}

func TestWriteRunWithoutAThreshold(t *testing.T) {
	var b strings.Builder
	if err := WriteRun(&b, Run{RunID: "(none)", Titles: titles}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "no proposals: the backtest found no evidence count reaching 95%") {
		t.Errorf("report:\n%s", b.String())
	}
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

func TestWriteRunListsAttention(t *testing.T) {
	march := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	r := Run{
		RunID:        "RUN2",
		Flags:        []categorize.Change{{Charge: 2, Before: ledger.State{Category: 9}, After: ledger.State{Category: 9, NeedsReview: true}}},
		FlagsApplied: 1,
		Reasons:      map[int64][]string{2: {"attention-new-payee", "attention-large"}},
		Income: []attention.Cycle{
			{Month: march, Expected: 7500, Actual: 9200, Ready: true},
			{Month: march.AddDate(0, 1, 0), Actual: 6000},
		},
		Tolerance:    1500,
		Titles:       titles,
		Transactions: map[int64]ledger.Transaction{2: rtx(2, "ATM", 9)},
	}
	var b strings.Builder
	if err := WriteRun(&b, r); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		"run RUN2: applied 1 of 1 changes",
		"needing your attention (1):",
		"2026-02-01 Card -12.50 ATM: attention-new-payee, attention-large",
		"monthly income (2 cycles):",
		"2026-03: expected 7500.00, got 9200.00, off by 1700.00: mismatch",
		"2026-04: got 6000.00, waiting for the card bill and rent",
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

func TestWriteRunListsTripsWithTheirEvidenceAndHints(t *testing.T) {
	r := Run{RunID: "R", Applied: 3, Titles: titles, Transactions: map[int64]ledger.Transaction{},
		Changes: []categorize.Change{
			{Charge: 1, After: ledger.State{Category: 5}, Source: "trips: trip-2026-03-10"},
			{Charge: 2, After: ledger.State{Category: 5}, Source: "trips: trip-2026-03-10 refund of 1"},
			{Charge: 3, After: ledger.State{Category: 5}, Source: "trips: trip-booking"},
			{Charge: 4, After: ledger.State{Category: 5}, Source: "trips: trip-2026-03-10"},
		},
		TripsAdded: []trips.Inferred{reportInferred("trip-2026-03-10", true)}, TripVersions: []trips.Trip{reportTrip("trip-japan", false)},
		Hints: []trips.Hint{{Edited: reportTrip("trip-farcity", false), Inferred: reportInferred("trip-2026-03-10", false)}},
	}
	var b strings.Builder
	if err := WriteRun(&b, r); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		"trips added (1):\n  trip-2026-03-10 2026-03-10..2026-03-14 (visa run)\n" + reportEvidence,
		"trips you edited that inference reaches beyond, left as they are (1):\n  trip-farcity 2026-03-10..2026-03-14: inferred 2026-03-10..2026-03-14\n" + reportEvidence,
		"trips with new names or dates (1):\n  trip-japan 2026-03-10..2026-03-14\n",
		"placed on trips (2):\n  trip-2026-03-10: 2\n  trip-booking: 1\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q:\n%s", want, out)
		}
	}
}

func TestWriteReplayListsInferredTripsWithTheirEvidence(t *testing.T) {
	var b strings.Builder
	if err := WriteReplay(&b, Replay{Trips: []trips.Inferred{reportInferred("trip-2026-03-10", false)}}); err != nil {
		t.Fatal(err)
	}
	if want := "trips inferred (1):\n  trip-2026-03-10 2026-03-10..2026-03-14\n" + reportEvidence; !strings.Contains(b.String(), want) {
		t.Errorf("report lacks %q:\n%s", want, b.String())
	}
}
