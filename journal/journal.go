// Package journal stores each run's changes so the run can be undone.
package journal

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/categorize"
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

// LastSet returns, for each transaction, the category the journal last recorded the
// categorizer setting: its last active non-undo change whose After is categorizer-owned. An
// undo entry (Source UndoSource(run ID)) deactivates that run's change to the transaction
// unless the undo's own run was itself undone, so an undo of an undo reactivates the original.
// Undo entries and records that are not owned (a user's correction) are never returned; a
// transaction with no such change is absent.
func (s Store) LastSet() (map[int64]categorize.Change, error) {
	names, err := filepath.Glob(filepath.Join(s.Dir, "*.jsonl"))
	if err != nil {
		return nil, err
	}
	slices.Sort(names)
	type entry struct {
		run string
		c   categorize.Change
	}
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
	out := map[int64]categorize.Change{}
	for tx, entries := range byTx {
		undone := map[string]bool{}
		for i := len(entries) - 1; i >= 0; i-- {
			e := entries[i]
			if target, ok := strings.CutPrefix(e.c.Source, undoPrefix); ok {
				if !undone[e.run] {
					undone[target] = true
				}
				continue
			}
			if !undone[e.run] && e.c.After.Owned() {
				out[tx] = e.c
				break
			}
		}
	}
	return out, nil
}

// UndoSource is the Source of an entry that undoes run runID.
func UndoSource(runID string) string { return undoPrefix + runID }
