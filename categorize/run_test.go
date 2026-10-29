package categorize

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

type fakeLedger struct {
	states map[int64]ledger.State
	log    *[]string
	failOn int64
}

func (f *fakeLedger) Charge(_ context.Context, id int64) (ledger.Charge, error) {
	s, ok := f.states[id]
	if !ok {
		return ledger.Charge{}, fmt.Errorf("no charge %d", id)
	}
	return ledger.Charge{ID: id, State: s}, nil
}

func (f *fakeLedger) Update(_ context.Context, id int64, s ledger.State) error {
	if id == f.failOn {
		return errors.New("update failed")
	}
	*f.log = append(*f.log, fmt.Sprintf("update %d", id))
	f.states[id] = s
	return nil
}

var (
	before1 = ledger.State{Category: 9, NeedsReview: true}
	after1  = ledger.State{Category: 7, Labels: []string{"grab-auto"}}
	before2 = ledger.State{Category: 7, NeedsReview: true, Labels: []string{"x"}}
	after2  = ledger.State{Category: 7, Labels: []string{"x", "grab-auto"}}
	run2    = []Change{{Charge: 1, Before: before1, After: after1}, {Charge: 2, Before: before2, After: after2}}
)

func setup() (*fakeLedger, *[]string, func(Change) error, *[]Change) {
	log := &[]string{}
	recorded := &[]Change{}
	l := &fakeLedger{states: map[int64]ledger.State{1: before1, 2: before2, 3: before1}, log: log}
	record := func(c Change) error {
		*log = append(*log, fmt.Sprintf("record %d", c.Charge))
		*recorded = append(*recorded, c)
		return nil
	}
	return l, log, record, recorded
}

func TestApplyRecordsEachChangeBeforeUpdating(t *testing.T) {
	l, log, record, _ := setup()
	n, err := Apply(context.Background(), l, record, run2)
	if err != nil || n != 2 {
		t.Fatalf("Apply = %d, %v", n, err)
	}
	want := []string{"record 1", "update 1", "record 2", "update 2"}
	if !slices.Equal(*log, want) {
		t.Errorf("log = %v, want %v", *log, want)
	}
	if !l.states[1].Equal(after1) || !l.states[2].Equal(after2) {
		t.Errorf("states = %v", l.states)
	}
}

func TestApplyStopsAtTheFirstFailedUpdate(t *testing.T) {
	l, _, record, recorded := setup()
	l.failOn = 2
	changes := append(slices.Clone(run2), Change{Charge: 3, Before: before1, After: after1})
	n, err := Apply(context.Background(), l, record, changes)
	if err == nil || n != 1 {
		t.Fatalf("Apply = %d, %v; want 1 and an error", n, err)
	}
	if len(*recorded) != 2 {
		t.Errorf("recorded %d entries, want 2 (the failed one included)", len(*recorded))
	}
	if !l.states[3].Equal(before1) {
		t.Errorf("charge 3 changed after the failure: %+v", l.states[3])
	}
}

func TestApplyDoesNotUpdateWhenRecordingFails(t *testing.T) {
	l, log, _, _ := setup()
	failing := func(Change) error { return errors.New("disk full") }
	if _, err := Apply(context.Background(), l, failing, run2); err == nil {
		t.Fatal("Apply succeeded with a failing journal")
	}
	if len(*log) != 0 {
		t.Errorf("updates sent without a journal entry: %v", *log)
	}
}

func TestUndoRestoresTheBeforeState(t *testing.T) {
	l, _, record, _ := setup()
	if _, err := Apply(context.Background(), l, record, run2); err != nil {
		t.Fatal(err)
	}
	changes, skipped, err := Undo(context.Background(), l, run2)
	if err != nil || len(skipped) != 0 {
		t.Fatalf("Undo: %v, skipped %v", err, skipped)
	}
	if len(changes) != 2 || changes[0].Charge != 2 || changes[1].Charge != 1 {
		t.Fatalf("undo changes = %+v, want charges 2 then 1", changes)
	}
	if _, err := Apply(context.Background(), l, record, changes); err != nil {
		t.Fatal(err)
	}
	if !l.states[1].Equal(before1) || !l.states[2].Equal(before2) {
		t.Errorf("after undo: %v", l.states)
	}
}

func TestUndoSkipsAChargeEditedByHand(t *testing.T) {
	l, _, record, _ := setup()
	if _, err := Apply(context.Background(), l, record, run2); err != nil {
		t.Fatal(err)
	}
	l.states[1] = ledger.State{Category: 42, Labels: []string{"grab-auto"}}
	changes, skipped, err := Undo(context.Background(), l, run2)
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 1 || skipped[0].Charge != 1 {
		t.Errorf("skipped = %+v, want charge 1", skipped)
	}
	if len(changes) != 1 || changes[0].Charge != 2 {
		t.Errorf("changes = %+v, want charge 2 only", changes)
	}
}

func TestUndoOfAnUndoReappliesTheRun(t *testing.T) {
	l, _, record, _ := setup()
	if _, err := Apply(context.Background(), l, record, run2); err != nil {
		t.Fatal(err)
	}
	undo, _, err := Undo(context.Background(), l, run2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(context.Background(), l, record, undo); err != nil {
		t.Fatal(err)
	}
	redo, _, err := Undo(context.Background(), l, undo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(context.Background(), l, record, redo); err != nil {
		t.Fatal(err)
	}
	if !l.states[1].Equal(after1) || !l.states[2].Equal(after2) {
		t.Errorf("after undoing the undo: %v", l.states)
	}
}

func TestUndoAfterAnInterruptedRun(t *testing.T) {
	l, _, record, recorded := setup()
	l.failOn = 2
	if _, err := Apply(context.Background(), l, record, run2); err == nil {
		t.Fatal("want the run to fail on charge 2")
	}
	l.failOn = 0
	changes, skipped, err := Undo(context.Background(), l, *recorded)
	if err != nil || len(skipped) != 0 {
		t.Fatalf("Undo: %v, skipped %v", err, skipped)
	}
	if len(changes) != 1 || changes[0].Charge != 1 {
		t.Errorf("changes = %+v, want charge 1 only", changes)
	}
}
