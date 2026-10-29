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
