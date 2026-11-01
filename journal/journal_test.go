package journal

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/categorize"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

var change = categorize.Change{
	Charge: 11,
	Before: ledger.State{Category: 9, NeedsReview: true, Labels: []string{"x"}},
	After:  ledger.State{Category: 7, NeedsReview: false, Labels: []string{"x", "grab-auto"}},
}

func TestRecordAndLoadRoundTrip(t *testing.T) {
	s := Store{Dir: filepath.Join(t.TempDir(), "state", "journal")}
	r, err := s.Start(time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	second := change
	second.Charge = 12
	for _, c := range []categorize.Change{change, second} {
		if err := r.Record(c); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if want := []categorize.Change{change, second}; !reflect.DeepEqual(got, want) {
		t.Errorf("Load = %+v, want %+v", got, want)
	}
}

func TestLatestIsTheNewestRun(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	var ids []string
	for _, at := range []time.Time{
		time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC),
	} {
		r, err := s.Start(at)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, r.ID)
		r.Close()
	}
	got, err := s.Latest()
	if err != nil {
		t.Fatal(err)
	}
	if got != ids[1] {
		t.Errorf("Latest = %s, want %s", got, ids[1])
	}
}

func TestLatestWithNoRunsIsAnError(t *testing.T) {
	if _, err := (Store{Dir: t.TempDir()}).Latest(); err == nil {
		t.Fatal("Latest succeeded on an empty journal")
	}
}

func TestStartRefusesToReuseARun(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	at := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	r, err := s.Start(at)
	if err != nil {
		t.Fatal(err)
	}
	r.Close()
	if _, err := s.Start(at); err == nil {
		t.Fatal("Start reopened an existing run")
	}
}
func TestLastSetIsTheNewestRunsChange(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	for i, after := range []ledger.CategoryID{7, 8} {
		r, err := s.Start(time.Date(2026, 10, 29+i, 12, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		c := change
		c.After.Category = after
		c.Source = "r"
		if err := r.Record(c); err != nil {
			t.Fatal(err)
		}
		r.Close()
	}
	got, err := s.LastSet()
	if err != nil {
		t.Fatal(err)
	}
	if got[11].After.Category != 8 || got[11].Source != "r" {
		t.Errorf("LastSet[11] = %+v, want the second run's change", got[11])
	}
}

func TestLastSetOfAnEmptyStoreIsEmpty(t *testing.T) {
	got, err := Store{Dir: filepath.Join(t.TempDir(), "none")}.LastSet()
	if err != nil || len(got) != 0 {
		t.Errorf("LastSet = %v, %v", got, err)
	}
}

func recordRun(t *testing.T, s Store, day int, source string, after ledger.CategoryID) string {
	t.Helper()
	r, err := s.Start(time.Date(2026, 10, day, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	c := change
	c.After.Category = after
	c.Source = source
	if err := r.Record(c); err != nil {
		t.Fatal(err)
	}
	return r.ID
}

func TestLastSetIgnoresAnUndoneOnlyRecord(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	first := recordRun(t, s, 1, "r", 7)
	recordRun(t, s, 2, "undo "+first, 9)
	got, err := s.LastSet()
	if err != nil || len(got) != 0 {
		t.Errorf("LastSet = %v, %v, want empty", got, err)
	}
}

func TestLastSetFallsBackToTheRecordBeforeAnUndoneRun(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	recordRun(t, s, 1, "r1", 7)
	second := recordRun(t, s, 2, "r2", 8)
	recordRun(t, s, 3, "undo "+second, 7)
	got, err := s.LastSet()
	if err != nil || got[11].Source != "r1" {
		t.Errorf("LastSet = %v, %v, want run 1's change", got, err)
	}
}

func TestLastSetUndoOfUndoReactivatesTheRecord(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	first := recordRun(t, s, 1, "r1", 7)
	undo := recordRun(t, s, 2, "undo "+first, 9)
	recordRun(t, s, 3, "undo "+undo, 7)
	got, err := s.LastSet()
	if err != nil || got[11].Source != "r1" {
		t.Errorf("LastSet = %v, %v, want run 1's change", got, err)
	}
}

func recordState(t *testing.T, s Store, day int, source string, after ledger.State) string {
	t.Helper()
	r, err := s.Start(time.Date(2026, 10, day, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	c := change
	c.After = after
	c.Source = source
	if err := r.Record(c); err != nil {
		t.Fatal(err)
	}
	return r.ID
}

func TestLastSetSkipsRecordsTheCategorizerDoesNotOwn(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	recordState(t, s, 1, "r1", ledger.State{Category: 9, Labels: []string{"auto-rule"}})
	recordState(t, s, 2, "", ledger.State{Category: 5})
	got, err := s.LastSet()
	if err != nil || got[11].Source != "r1" {
		t.Errorf("LastSet = %v, %v, want run 1's change", got, err)
	}
}

func TestLastSetOmitsATransactionWhoseOnlyRecordIsNotOwned(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	recordState(t, s, 1, "", ledger.State{Category: 5})
	got, err := s.LastSet()
	if err != nil || len(got) != 0 {
		t.Errorf("LastSet = %v, %v, want empty", got, err)
	}
}

func TestUndoSourceDeactivatesTheNamedRun(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	first := recordState(t, s, 1, "r1", ledger.State{Category: 9, Labels: []string{"auto-rule"}})
	recordState(t, s, 2, UndoSource(first), ledger.State{Category: 3})
	got, err := s.LastSet()
	if err != nil || len(got) != 0 {
		t.Errorf("LastSet = %v, %v, want empty", got, err)
	}
}
