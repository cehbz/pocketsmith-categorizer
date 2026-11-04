package fx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/internal/day"
)

// Source fetches the rates over a span of days.
type Source interface {
	Fetch(ctx context.Context, first, last time.Time) (Table, error)
}

// Cache keeps the rates its source fetched in the file at Path, so a run fetches only the days
// after the last one kept, and matches on the kept rates when the source fails.
type Cache struct {
	Path   string
	Source Source
}

// Fetch returns the rates from first through last: the kept ones and those the source has for
// the days after them, saved for the next run. When the source fails and a rate on or before
// first is kept, the table is the kept rates, stale with the source's error.
func (c Cache) Fetch(ctx context.Context, first, last time.Time) (Table, error) {
	kept, err := c.load()
	if err != nil {
		return Table{}, err
	}
	_, missing := kept.THBPerUSD(first)
	from := first
	if missing == nil {
		from = kept.Through().AddDate(0, 0, 1)
	}
	if from.After(last) {
		return kept, nil
	}
	fetched, err := c.Source.Fetch(ctx, from, last)
	if err != nil {
		if missing != nil {
			return Table{}, err
		}
		kept.Stale = fmt.Errorf("rates kept through %s: %w", day.Format(kept.Through()), err)
		return kept, nil
	}
	merged := kept.with(fetched)
	return merged, c.save(merged)
}

// load reads the kept rates; a missing file keeps none.
func (c Cache) load() (Table, error) {
	b, err := os.ReadFile(c.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return Table{}, nil
	}
	if err != nil {
		return Table{}, err
	}
	var byDay map[string]float64
	if err := json.Unmarshal(b, &byDay); err != nil {
		return Table{}, fmt.Errorf("rates %s: %w", c.Path, err)
	}
	var t Table
	for _, k := range slices.Sorted(maps.Keys(byDay)) {
		d, err := day.Parse(k)
		if err != nil {
			return Table{}, fmt.Errorf("rates %s: %w", c.Path, err)
		}
		t.dates = append(t.dates, d)
		t.rates = append(t.rates, byDay[k])
	}
	return t, nil
}

// save writes t's rates, replacing the file atomically.
func (c Cache) save(t Table) error {
	byDay := make(map[string]float64, len(t.dates))
	for i, d := range t.dates {
		byDay[day.Format(d)] = t.rates[i]
	}
	b, err := json.MarshalIndent(byDay, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(c.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".rates-*")
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
	return os.Rename(tmp.Name(), c.Path)
}
