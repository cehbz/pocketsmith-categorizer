// Package tripfile keeps the trip list in the TOML file the user edits, and the record of
// every trip the categorizer wrote to it.
package tripfile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/BurntSushi/toml"

	"github.com/cehbz/pocketsmith-categorizer/internal/day"
	"github.com/cehbz/pocketsmith-categorizer/trips"
)

// File is the trip list and the record of trips written, by path.
type File struct {
	List    string // the trip list the user edits
	Written string // every trip the categorizer wrote to the list
}

type doc struct {
	Trip []entry `toml:"trip"`
}

type entry struct {
	Name    string `toml:"name"`
	From    string `toml:"from"`
	To      string `toml:"to"`
	VisaRun bool   `toml:"visa_run"`
}

const header = "# Trips: correct names and dates, set visa_run, add trips and delete what isn't one.\n\n"

// Load reads the list and the record of trips written; a missing file is empty.
func (f File) Load() (trips.List, []trips.Trip, error) {
	list, err := read(f.List)
	if err != nil {
		return nil, nil, err
	}
	written, err := read(f.Written)
	if err != nil {
		return nil, nil, err
	}
	return list, written, nil
}

// Save writes the list and the record of trips written, each replaced atomically.
func (f File) Save(list trips.List, written []trips.Trip) error {
	if err := write(f.List, list); err != nil {
		return err
	}
	return write(f.Written, written)
}

func read(path string) ([]trips.Trip, error) {
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	var d doc
	md, err := toml.DecodeFile(path, &d)
	if err != nil {
		return nil, fmt.Errorf("trips %s: %w", path, err)
	}
	if u := md.Undecoded(); len(u) > 0 {
		return nil, fmt.Errorf("trips %s: unknown key %s", path, u[0])
	}
	out := make([]trips.Trip, len(d.Trip))
	for i, e := range d.Trip {
		from, errFrom := day.Parse(e.From)
		to, errTo := day.Parse(e.To)
		switch {
		case e.Name == "" || strings.ContainsFunc(e.Name, unicode.IsSpace):
			return nil, fmt.Errorf("trips %s: trip %d: want a name without spaces", path, i+1)
		case errFrom != nil || errTo != nil || to.Before(from):
			return nil, fmt.Errorf("trips %s: trip %s: want from and to as YYYY-MM-DD, from first", path, e.Name)
		}
		out[i] = trips.Trip{Name: e.Name, From: from, To: to, VisaRun: e.VisaRun}
	}
	return out, nil
}

func write(path string, ts []trips.Trip) error {
	d := doc{Trip: make([]entry, len(ts))}
	for i, t := range ts {
		d.Trip[i] = entry{Name: t.Name, From: day.Format(t.From), To: day.Format(t.To), VisaRun: t.VisaRun}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".trips-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(header); err != nil {
		tmp.Close()
		return err
	}
	if err := toml.NewEncoder(tmp).Encode(d); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
