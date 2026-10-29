package fx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cehbz/pocketsmith-categorizer/internal/day"
)

func TestFetchAndLookUpTheLatestRateOnOrBefore(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/2026-10-02..2026-10-12" || r.URL.Query().Get("from") != "USD" || r.URL.Query().Get("to") != "THB" {
			t.Errorf("request %s", r.URL)
		}
		w.Write([]byte(`{"amount":1.0,"base":"USD","rates":{"2026-10-09":{"THB":33.2},"2026-10-08":{"THB":33.1},"2026-10-12":{"THB":33.4}}}`))
	}))
	defer srv.Close()
	first, _ := day.Parse("2026-10-09")
	last, _ := day.Parse("2026-10-12")
	table, err := Frankfurter{BaseURL: srv.URL, HTTP: srv.Client()}.Fetch(context.Background(), first, last)
	if err != nil {
		t.Fatal(err)
	}
	for date, want := range map[string]float64{"2026-10-08": 33.1, "2026-10-10": 33.2, "2026-10-11": 33.2, "2026-10-12": 33.4, "2026-10-20": 33.4} {
		d, _ := day.Parse(date)
		got, err := table.THBPerUSD(d)
		if err != nil || got != want {
			t.Errorf("THBPerUSD(%s) = %v, %v; want %v", date, got, err, want)
		}
	}
	early, _ := day.Parse("2026-10-07")
	if _, err := table.THBPerUSD(early); err == nil {
		t.Error("THBPerUSD before the first rate succeeded")
	}
}

func TestFetchFailsOnAnErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()
	d, _ := day.Parse("2026-10-09")
	if _, err := (Frankfurter{BaseURL: srv.URL, HTTP: srv.Client()}).Fetch(context.Background(), d, d); err == nil {
		t.Fatal("Fetch succeeded on a 404")
	}
}

func TestFetchFailsWithNoRates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"rates":{}}`))
	}))
	defer srv.Close()
	d, _ := day.Parse("2026-10-09")
	if _, err := (Frankfurter{BaseURL: srv.URL, HTTP: srv.Client()}).Fetch(context.Background(), d, d); err == nil {
		t.Fatal("Fetch succeeded with no rates")
	}
}
