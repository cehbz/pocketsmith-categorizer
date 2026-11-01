// Package memo remembers what the categorizer has already shown the user.
package memo

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

// Memo is a set of keys kept in a JSON file.
type Memo struct {
	path string
	seen map[string]bool
}

// Load reads the memo at path; a missing file is an empty memo.
func Load(path string) (*Memo, error) {
	m := &Memo{path: path, seen: map[string]bool{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	var keys []string
	if err := json.Unmarshal(b, &keys); err != nil {
		return nil, fmt.Errorf("memo %s: %w", path, err)
	}
	for _, k := range keys {
		m.seen[k] = true
	}
	return m, nil
}

// Seen reports whether key was shown.
func (m *Memo) Seen(key string) bool { return m.seen[key] }

// Add records key as shown.
func (m *Memo) Add(key string) { m.seen[key] = true }

// Save writes the memo, replacing the file atomically.
func (m *Memo) Save() error {
	keys := make([]string, 0, len(m.seen))
	for k := range m.seen {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	b, err := json.MarshalIndent(keys, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(m.path), ".shown-*")
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
	return os.Rename(tmp.Name(), m.path)
}
