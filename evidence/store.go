package evidence

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// Read is a message as read: its id, the merchant whose search found it, when it arrived,
// and its evidence or why it couldn't be read. It keeps nothing else of the message.
type Read struct {
	ID         string    `json:"id"`
	Merchant   string    `json:"merchant"`
	Received   time.Time `json:"received"`
	Evidence   Evidence  `json:"evidence"`
	Unreadable string    `json:"unreadable,omitempty"`
}

// Store is the evidence read from mail, kept in a JSON file between runs.
type Store struct {
	ReadTo time.Time // the last day the mail has been read through; zero before the first read
	path   string
	reads  map[string]Read // by message id
}

// file is a store as its file holds it.
type file struct {
	ReadTo time.Time `json:"read_to"`
	Reads  []Read    `json:"reads"`
}

// Load reads the store at path; a missing file is an empty store.
func Load(path string) (*Store, error) {
	s := &Store{path: path, reads: map[string]Read{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var f file
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("evidence %s: %w", path, err)
	}
	s.ReadTo = f.ReadTo
	for _, r := range f.Reads {
		s.reads[r.ID] = r
	}
	return s, nil
}

// Holds reports whether the store has read message id.
func (s *Store) Holds(id string) bool {
	_, ok := s.reads[id]
	return ok
}

// HoldsEvidence reports whether the store has read message id into evidence, as opposed to
// not at all or as unreadable.
func (s *Store) HoldsEvidence(id string) bool {
	r, ok := s.reads[id]
	return ok && r.Unreadable == ""
}

// Add keeps r, replacing any read of the same message.
func (s *Store) Add(r Read) { s.reads[r.ID] = r }

// Drop forgets the messages received before day.
func (s *Store) Drop(day time.Time) {
	maps.DeleteFunc(s.reads, func(_ string, r Read) bool { return r.Received.Before(day) })
}

// Evidence is every read's evidence, each piece with its read's merchant, in the order the
// messages arrived.
func (s *Store) Evidence() Evidence {
	var ev Evidence
	for _, r := range s.sorted() {
		for _, x := range r.Evidence.Receipts {
			x.Merchant = r.Merchant
			ev.Receipts = append(ev.Receipts, x)
		}
		for _, x := range r.Evidence.Cancellations {
			x.Merchant = r.Merchant
			ev.Cancellations = append(ev.Cancellations, x)
		}
		for _, x := range r.Evidence.Deliveries {
			x.Merchant = r.Merchant
			ev.Deliveries = append(ev.Deliveries, x)
		}
	}
	return ev
}

// Unreadable is the reads of messages that couldn't be read, in the order they arrived.
func (s *Store) Unreadable() []Read {
	var out []Read
	for _, r := range s.sorted() {
		if r.Unreadable != "" {
			out = append(out, r)
		}
	}
	return out
}

// sorted is the reads by arrival, then id.
func (s *Store) sorted() []Read {
	return slices.SortedFunc(maps.Values(s.reads), func(a, b Read) int {
		if c := a.Received.Compare(b.Received); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})
}

// Save writes the store, only its owner able to read it, replacing the file atomically.
func (s *Store) Save() error {
	b, err := json.MarshalIndent(file{ReadTo: s.ReadTo, Reads: s.sorted()}, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".evidence-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}
