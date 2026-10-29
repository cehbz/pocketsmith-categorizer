// Package report prints what a run did.
package report

import (
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/categorize"
	"github.com/cehbz/pocketsmith-categorizer/grab"
	"github.com/cehbz/pocketsmith-categorizer/internal/day"
)

// Cleanup is what a cleanup run did.
type Cleanup struct {
	RunID    string
	Changes  []categorize.Change
	Applied  int
	Correct  int
	Matching categorize.Matching
	Payments []grab.Payment // every payment read, for the context of unclaimed charges
	Rates    categorize.Rates
	Ledger   *time.Location
}

type printer struct {
	w   io.Writer
	err error
}

func (p *printer) f(format string, a ...any) {
	if p.err == nil {
		_, p.err = fmt.Fprintf(p.w, format, a...)
	}
}

func (p *printer) payment(indent string, pay grab.Payment) {
	p.f("%s%s %s %s %s THB %.2f\n", indent, pay.Account, pay.Booking, pay.Service, pay.At.Format("2006-01-02 15:04"), pay.THB)
}

// Write prints the cleanup report: changes applied, payments no charge matched, and each
// unclaimed charge with the payments whose posting window contains its date.
func Write(w io.Writer, r Cleanup) error {
	p := &printer{w: w}
	p.f("run %s: applied %d of %d changes, %d already correct\n", r.RunID, r.Applied, len(r.Changes), r.Correct)
	p.f("changed (%d):\n", r.Applied)
	for _, c := range r.Changes[:r.Applied] {
		p.f("  charge %d: category %d -> %d, needs review %t -> %t, labels %q -> %q\n",
			c.Charge, c.Before.Category, c.After.Category, c.Before.NeedsReview, c.After.NeedsReview, c.Before.Labels, c.After.Labels)
	}
	p.f("unmatched payments (%d):\n", len(r.Matching.Unmatched))
	for _, pay := range r.Matching.Unmatched {
		p.payment("  ", pay)
	}
	byTime := slices.SortedFunc(slices.Values(r.Payments), func(a, b grab.Payment) int { return a.At.Compare(b.At) })
	p.f("unclaimed charges (%d):\n", len(r.Matching.Unclaimed))
	for _, c := range r.Matching.Unclaimed {
		rate, err := r.Rates(c.Date)
		if err != nil {
			return err
		}
		p.f("  charge %d %s USD %.2f (THB %.2f)\n", c.ID, day.Format(c.Date), c.USD, c.USD*rate)
		for _, pay := range byTime {
			if first, last := pay.PostingWindow(r.Ledger); !c.Date.Before(first) && !c.Date.After(last) {
				p.payment("    ", pay)
			}
		}
	}
	return p.err
}

// UndoRun is what an undo run did.
type UndoRun struct {
	RunID, Target string
	Changes       []categorize.Change
	Applied       int
	Skipped       []categorize.Change
}

// WriteUndo prints the undo report.
func WriteUndo(w io.Writer, r UndoRun) error {
	p := &printer{w: w}
	p.f("run %s: undid %d of %d changes from run %s, skipped %d\n", r.RunID, r.Applied, len(r.Changes), r.Target, len(r.Skipped))
	for _, c := range r.Skipped {
		p.f("  charge %d: category changed since run %s, left as is\n", c.Charge, r.Target)
	}
	return p.err
}
