// Package journal stores each run's changes so the run can be undone.
package journal

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/categorize"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

// Store keeps one JSON-lines file per run in Dir.
type Store struct{ Dir string }

// Run is an open run journal.
type Run struct {
	ID string
	f  *os.File
}

const idLayout = "20060102T150405.000000000"

// Start opens a new run journal named for now.
func (s Store) Start(now time.Time) (*Run, error) {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return nil, err
	}
	id := now.UTC().Format(idLayout) + "Z"
	f, err := os.OpenFile(filepath.Join(s.Dir, id+".jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	return &Run{ID: id, f: f}, nil
}

// Record appends c and flushes it to disk.
func (r *Run) Record(c categorize.Change) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if _, err := r.f.Write(append(b, '\n')); err != nil {
		return err
	}
	return r.f.Sync()
}

// Close closes the run's journal file.
func (r *Run) Close() error { return r.f.Close() }

// Latest returns the newest run's ID.
func (s Store) Latest() (string, error) {
	names, err := filepath.Glob(filepath.Join(s.Dir, "*.jsonl"))
	if err != nil {
		return "", err
	}
	if len(names) == 0 {
		return "", fmt.Errorf("no runs in %s", s.Dir)
	}
	slices.Sort(names)
	return strings.TrimSuffix(filepath.Base(names[len(names)-1]), ".jsonl"), nil
}

// Load reads a run's changes in the order they were recorded.
func (s Store) Load(id string) ([]categorize.Change, error) {
	f, err := os.Open(filepath.Join(s.Dir, id+".jsonl"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var changes []categorize.Change
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var c categorize.Change
		if err := json.Unmarshal(sc.Bytes(), &c); err != nil {
			return nil, fmt.Errorf("run %s: %w", id, err)
		}
		changes = append(changes, c)
	}
	return changes, sc.Err()
}

const undoPrefix = "undo "

type entry struct {
	run string
	c   categorize.Change
}

// history reads every run's changes, by transaction, oldest first.
func (s Store) history() (map[int64][]entry, error) {
	names, err := filepath.Glob(filepath.Join(s.Dir, "*.jsonl"))
	if err != nil {
		return nil, err
	}
	slices.Sort(names)
	byTx := map[int64][]entry{}
	for _, n := range names {
		id := strings.TrimSuffix(filepath.Base(n), ".jsonl")
		changes, err := s.Load(id)
		if err != nil {
			return nil, err
		}
		for _, c := range changes {
			byTx[c.Charge] = append(byTx[c.Charge], entry{id, c})
		}
	}
	return byTx, nil
}

// active is a transaction's changes still in effect, oldest first: an undo entry (Source
// UndoSource(run ID)) deactivates that run's change unless the undo's own run was itself
// undone, so an undo of an undo reactivates the original. Undo entries are never active.
func active(entries []entry) []categorize.Change {
	undone := map[string]bool{}
	var out []categorize.Change
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if target, ok := strings.CutPrefix(e.c.Source, undoPrefix); ok {
			if !undone[e.run] {
				undone[target] = true
			}
			continue
		}
		if !undone[e.run] {
			out = append(out, e.c)
		}
	}
	slices.Reverse(out)
	return out
}

// LastSet returns, for each transaction, the category the journal last recorded the
// categorizer setting: its last active change whose After is categorizer-owned. Records
// that are not owned (a user's correction) are never returned; a transaction with no such
// change is absent.
func (s Store) LastSet() (map[int64]categorize.Change, error) {
	byTx, err := s.history()
	if err != nil {
		return nil, err
	}
	out := map[int64]categorize.Change{}
	for tx, entries := range byTx {
		cs := active(entries)
		for i := len(cs) - 1; i >= 0; i-- {
			if cs[i].After.Owned() {
				out[tx] = cs[i]
				break
			}
		}
	}
	return out, nil
}

// Origins returns, for each transaction whose latest active change has a source isTrip
// accepts, its state before that unbroken run of such changes began.
func (s Store) Origins(isTrip func(source string) bool) (map[int64]ledger.State, error) {
	byTx, err := s.history()
	if err != nil {
		return nil, err
	}
	out := map[int64]ledger.State{}
	for tx, entries := range byTx {
		var origin *ledger.State
		for _, c := range active(entries) {
			switch {
			case !isTrip(c.Source):
				origin = nil
			case origin == nil:
				before := c.Before
				origin = &before
			}
		}
		if origin != nil {
			out[tx] = *origin
		}
	}
	return out, nil
}

// SaveTrips records the trips run id added to the trip list.
func (s Store) SaveTrips(id string, names []string) error {
	b, err := json.Marshal(names)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.Dir, id+".trips.json"), b, 0o600)
}

// Trips returns the trips run id added; none when it added none.
func (s Store) Trips(id string) ([]string, error) {
	b, err := os.ReadFile(filepath.Join(s.Dir, id+".trips.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	if err := json.Unmarshal(b, &names); err != nil {
		return nil, fmt.Errorf("run %s trips: %w", id, err)
	}
	return names, nil
}

// UndoSource is the Source of an entry that undoes run runID.
func UndoSource(runID string) string { return undoPrefix + runID }
