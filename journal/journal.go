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
