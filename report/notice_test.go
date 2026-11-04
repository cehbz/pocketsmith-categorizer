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
		Flags:         []categorize.Change{{Charge: 1}, {Charge: 2}, {Charge: 3}},
		FlagsApplied:  3,
		Suggestions:   []decide.Suggestion{{}, {}},
		Proposals:     []learn.Proposal{{}, {}},
		Amendments:    []categorize.Change{{Charge: 4, Source: "payee=\"x\""}, {Charge: 5, Source: "trips: trip-abroad"}},
		TripsAdded:    []trips.Inferred{{}},
		TripVersions:  []trips.Trip{{}, {}},
		Hints:         []trips.Hint{{}, {}, {}},
		NewHints:      1,
		Receipts:      Receipts{Unreadable: []Unreadable{{}, {}, {}}},
		NewUnreadable: 2,
	}
	subject, due := Notice(r)
	want := "Categorizer: 2 rules to approve · 1 rule amendment · 2 suggestions · 1 trip added · 1 trip hint · 2 trips renamed or redated · 2 unreadable receipts · 3 transactions to review"
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

func TestHintsAndUnreadableReceiptsAnEarlierRunCountedMakeNoNotice(t *testing.T) {
	r := Run{Hints: []trips.Hint{{}}, Receipts: Receipts{Unreadable: []Unreadable{{}}}}
	if subject, due := Notice(r); due {
		t.Errorf("Notice = %q, true; want no notice", subject)
	}
}

func TestOnlyAppliedFlagsCountAsToReview(t *testing.T) {
	r := Run{Flags: []categorize.Change{{Charge: 1}, {Charge: 2}}, FlagsApplied: 1}
	if subject, _ := Notice(r); subject != "Categorizer: 1 transaction to review" {
		t.Errorf("Notice = %q; want Categorizer: 1 transaction to review", subject)
	}
}

func TestAFailureSubjectIsOneLine(t *testing.T) {
	err := errors.Join(errors.New("pocketsmith: GET /me: timeout"), errors.New("second"))
	if got := Failure(err); got != "categorize run failed: pocketsmith: GET /me: timeout" {
		t.Errorf("Failure = %q", got)
	}
}
