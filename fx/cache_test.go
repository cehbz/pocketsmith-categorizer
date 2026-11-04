package fx

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/internal/day"
)

// source is a rate source that records the spans it was asked for and returns table or err.
type source struct {
	table Table
	err   error
	spans [][2]string
}

func (s *source) Fetch(ctx context.Context, first, last time.Time) (Table, error) {
	s.spans = append(s.spans, [2]string{day.Format(first), day.Format(last)})
	return s.table, s.err
}

// kept writes rates to a cache file in a fresh directory and returns its path.
func kept(t *testing.T, rates string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rates.json")
	if rates != "" {
		if err := os.WriteFile(path, []byte(rates), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func mustDay(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := day.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func table(t *testing.T, rates map[string]float64) Table {
	t.Helper()
	var tb Table
	for _, d := range []string{"2026-10-01", "2026-10-02", "2026-10-05", "2026-10-06", "2026-10-07"} {
		if r, ok := rates[d]; ok {
			tb.dates = append(tb.dates, mustDay(t, d))
			tb.rates = append(tb.rates, r)
		}
	}
	return tb
}

func rate(t *testing.T, tb Table, d string) float64 {
	t.Helper()
	r, err := tb.THBPerUSD(mustDay(t, d))
	if err != nil {
		t.Fatalf("THBPerUSD(%s): %v", d, err)
	}
	return r
}

func TestACacheFetchesOnlyTheDaysAfterItsLastAndKeepsThem(t *testing.T) {
	path := kept(t, `{"2026-10-01": 33.1, "2026-10-02": 33.2}`)
	src := &source{table: table(t, map[string]float64{"2026-10-02": 33.2, "2026-10-05": 33.5, "2026-10-06": 33.6})}
	c := Cache{Path: path, Source: src}
	got, err := c.Fetch(context.Background(), mustDay(t, "2026-10-01"), mustDay(t, "2026-10-08"))
	if err != nil {
		t.Fatal(err)
	}
	if len(src.spans) != 1 || src.spans[0] != [2]string{"2026-10-03", "2026-10-08"} {
		t.Errorf("fetched %v; want 2026-10-03..2026-10-08", src.spans)
	}
	if got.Stale != nil || rate(t, got, "2026-10-01") != 33.1 || rate(t, got, "2026-10-06") != 33.6 {
		t.Errorf("table %+v; want the kept rates and the fetched ones", got)
	}
	again, err := Cache{Path: path, Source: &source{err: errors.New("down")}}.Fetch(context.Background(), mustDay(t, "2026-10-01"), mustDay(t, "2026-10-08"))
	if err != nil || rate(t, again, "2026-10-06") != 33.6 {
		t.Errorf("after a save the cache holds %+v, %v; want 2026-10-06 at 33.6", again, err)
	}
}

func TestACacheNotReachingTheFirstDayFetchesTheWholeSpan(t *testing.T) {
	path := kept(t, `{"2026-10-05": 33.5}`)
	src := &source{table: table(t, map[string]float64{"2026-10-01": 33.1, "2026-10-05": 33.5})}
	if _, err := (Cache{Path: path, Source: src}).Fetch(context.Background(), mustDay(t, "2026-10-02"), mustDay(t, "2026-10-06")); err != nil {
		t.Fatal(err)
	}
	if len(src.spans) != 1 || src.spans[0] != [2]string{"2026-10-02", "2026-10-06"} {
		t.Errorf("fetched %v; want 2026-10-02..2026-10-06", src.spans)
	}
}

func TestWhenTheSourceFailsTheCacheMatchesOnItsLastRateAndSaysWhy(t *testing.T) {
	path := kept(t, `{"2026-10-01": 33.1, "2026-10-02": 33.2}`)
	down := errors.New("frankfurter: 522")
	got, err := Cache{Path: path, Source: &source{err: down}}.Fetch(context.Background(), mustDay(t, "2026-10-01"), mustDay(t, "2026-10-08"))
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(got.Stale, down) || day.Format(got.Through()) != "2026-10-02" || rate(t, got, "2026-10-07") != 33.2 {
		t.Errorf("table %+v through %s; want the kept rates through 2026-10-02, stale with the source's error", got, day.Format(got.Through()))
	}
}

func TestWhenTheSourceFailsAndTheCacheDoesntReachTheFirstDayFetchFails(t *testing.T) {
	path := kept(t, `{"2026-10-05": 33.5}`)
	if _, err := (Cache{Path: path, Source: &source{err: errors.New("down")}}).Fetch(context.Background(), mustDay(t, "2026-10-02"), mustDay(t, "2026-10-06")); err == nil {
		t.Error("Fetch succeeded without a rate on or before the first day")
	}
	if _, err := (Cache{Path: kept(t, ""), Source: &source{err: errors.New("down")}}).Fetch(context.Background(), mustDay(t, "2026-10-02"), mustDay(t, "2026-10-06")); err == nil {
		t.Error("Fetch succeeded with no cache file")
	}
}
