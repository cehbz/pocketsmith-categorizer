package report

import (
	"errors"
	"testing"

	"github.com/cehbz/pocketsmith-categorizer/categorize"
	"github.com/cehbz/pocketsmith-categorizer/decide"
	"github.com/cehbz/pocketsmith-categorizer/grab"
	"github.com/cehbz/pocketsmith-categorizer/learn"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
	"github.com/cehbz/pocketsmith-categorizer/trips"
)

func TestARunWithSomethingNewIsDueANoticeCountingIt(t *testing.T) {
	r := Run{
		Flags:        []categorize.Change{{Charge: 1}, {Charge: 2}, {Charge: 3}},
		FlagsApplied: 3,
		Suggestions:  []decide.Suggestion{{}, {}},
		Proposals:    []learn.Proposal{{}},
		Amendments:   []categorize.Change{{Charge: 4}},
		TripsAdded:   []trips.Inferred{{}},
		TripVersions: []trips.Trip{{}, {}},
		Hints:        []trips.Hint{{}},
		Receipts:     Receipts{Unreadable: []Unreadable{{}}},
	}
	subject, due := Notice(r)
	want := "categorize: 3 to review, 2 suggestions, 1 proposed rule, 1 rule amendment, 1 trip added, 2 trips renamed or redated, 1 trip hint, 1 receipt not read"
	if !due || subject != want {
		t.Errorf("Notice = %q, %v; want %q, true", subject, due, want)
	}
}

func TestARunWithOnlyStandingListsOrAppliedChangesIsDueNoNotice(t *testing.T) {
	r := Run{
		Changes:     []categorize.Change{{Charge: 1}},
		Applied:     1,
		Corrections: []categorize.Change{{Charge: 2}},
		Undecided:   []ledger.Transaction{{ID: 3}},
		Receipts:    Receipts{Unmatched: []grab.Payment{{}}},
	}
	if subject, due := Notice(r); due {
		t.Errorf("Notice = %q, true; want no notice", subject)
	}
}

func TestOnlyAppliedFlagsCountAsToReview(t *testing.T) {
	r := Run{Flags: []categorize.Change{{Charge: 1}, {Charge: 2}}, FlagsApplied: 1}
	if subject, _ := Notice(r); subject != "categorize: 1 to review" {
		t.Errorf("Notice = %q; want categorize: 1 to review", subject)
	}
}

func TestAFailureSubjectIsOneLine(t *testing.T) {
	err := errors.Join(errors.New("pocketsmith: GET /me: timeout"), errors.New("second"))
	if got := Failure(err); got != "categorize run failed: pocketsmith: GET /me: timeout" {
		t.Errorf("Failure = %q", got)
	}
}
