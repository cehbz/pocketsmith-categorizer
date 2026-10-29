// Package fx supplies daily THB/USD reference rates.
package fx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/internal/day"
)

// DefaultBaseURL is Frankfurter's API, which serves ECB reference rates on business days.
const DefaultBaseURL = "https://api.frankfurter.dev/v1"

// Table holds daily THB per USD rates, oldest first.
type Table struct {
	dates []time.Time
	rates []float64
}

// THBPerUSD returns the latest rate on or before d.
func (t Table) THBPerUSD(d time.Time) (float64, error) {
	i, found := slices.BinarySearchFunc(t.dates, d, func(a, b time.Time) int { return a.Compare(b) })
	if found {
		return t.rates[i], nil
	}
	if i == 0 {
		return 0, fmt.Errorf("no THB rate on or before %s", day.Format(d))
	}
	return t.rates[i-1], nil
}

// Frankfurter fetches ECB reference rates.
type Frankfurter struct {
	BaseURL string
	HTTP    *http.Client
}

// Fetch returns the rates from a week before first through last, so first has a prior business day.
func (f Frankfurter) Fetch(ctx context.Context, first, last time.Time) (Table, error) {
	url := fmt.Sprintf("%s/%s..%s?from=USD&to=THB", f.BaseURL, day.Format(first.AddDate(0, 0, -7)), day.Format(last))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Table{}, err
	}
	resp, err := f.HTTP.Do(req)
	if err != nil {
		return Table{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return Table{}, fmt.Errorf("frankfurter %s: %s: %s", url, resp.Status, msg)
	}
	var body struct {
		Rates map[string]struct {
			THB float64 `json:"THB"`
		} `json:"rates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Table{}, fmt.Errorf("frankfurter %s: %w", url, err)
	}
	keys := make([]string, 0, len(body.Rates))
	for k := range body.Rates {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var t Table
	for _, k := range keys {
		d, err := day.Parse(k)
		if err != nil {
			return Table{}, fmt.Errorf("frankfurter date %q: %w", k, err)
		}
		t.dates = append(t.dates, d)
		t.rates = append(t.rates, body.Rates[k].THB)
	}
	if len(t.dates) == 0 {
		return Table{}, fmt.Errorf("frankfurter %s: no rates", url)
	}
	return t, nil
}
