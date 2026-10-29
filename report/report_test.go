package report

import (
	"strings"
	"testing"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/categorize"
	"github.com/cehbz/pocketsmith-categorizer/grab"
	"github.com/cehbz/pocketsmith-categorizer/internal/day"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

func load(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

func at(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, load("Asia/Bangkok"))
	if err != nil {
		panic(err)
	}
	return t
}

func TestWriteCleanup(t *testing.T) {
	x := grab.Payment{Booking: "A-X", Service: "GrabFood", THB: 120, At: at("2026-10-12 14:00"), Account: "bob"}
	y := grab.Payment{Booking: "A-Y", Service: "Standard", THB: 75, At: at("2026-10-12 08:00"), Account: "alice"}
	d, _ := day.Parse("2026-10-13")
	change := categorize.Change{
		Charge: 11,
		Before: ledger.State{Category: 5, NeedsReview: true},
		After:  ledger.State{Category: 7, Labels: []string{"grab-auto"}},
	}
	var b strings.Builder
	err := Write(&b, Cleanup{
		RunID:    "R1",
		Changes:  []categorize.Change{change},
		Applied:  1,
		Correct:  1,
		Matching: categorize.Matching{Unmatched: []grab.Payment{x}, Unclaimed: []ledger.Charge{{ID: 12, USD: 1.50, Date: d}}},
		Payments: []grab.Payment{x, y},
		Rates:    func(time.Time) (float64, error) { return 33, nil },
		Ledger:   load("America/New_York"),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `run R1: applied 1 of 1 changes, 1 already correct
changed (1):
  charge 11: category 5 -> 7, needs review true -> false, labels [] -> ["grab-auto"]
unmatched payments (1):
  bob A-X GrabFood 2026-10-12 14:00 THB 120.00
unclaimed charges (1):
  charge 12 2026-10-13 USD 1.50 (THB 49.50)
    alice A-Y Standard 2026-10-12 08:00 THB 75.00
    bob A-X GrabFood 2026-10-12 14:00 THB 120.00
`
	if b.String() != want {
		t.Errorf("report:\n%s\nwant:\n%s", b.String(), want)
	}
}

func TestWriteUndo(t *testing.T) {
	var b strings.Builder
	err := WriteUndo(&b, UndoRun{
		RunID:     "R2",
		Target:    "R1",
		Changes:   []categorize.Change{{Charge: 11}},
		Applied:   1,
		Skipped:   []categorize.Change{{Charge: 12}},
		Unchanged: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `run R2: undid 1 of 1 changes from run R1, skipped 1, already restored 2
  charge 12: category changed since run R1, left as is
`
	if b.String() != want {
		t.Errorf("report:\n%s\nwant:\n%s", b.String(), want)
	}
}
