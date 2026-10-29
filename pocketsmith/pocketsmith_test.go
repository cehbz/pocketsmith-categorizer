package pocketsmith

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cehbz/pocketsmith-categorizer/internal/day"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

const key = "test-key"

func server(t *testing.T, handlers map[string]http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	mux := http.NewServeMux()
	for pattern, h := range handlers {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Developer-Key") != key {
				t.Errorf("%s %s without the developer key", r.Method, r.URL.Path)
			}
			h(w, r)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL, Key: key, HTTP: srv.Client()}, srv
}

func TestChargesPagesAndFiltersPayees(t *testing.T) {
	var srvURL string
	c, srv := server(t, map[string]http.HandlerFunc{
		"GET /transaction_accounts/7/transactions": func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			if q.Get("page") == "2" {
				w.Write([]byte(`[{"id":3,"date":"2026-10-13","amount":-1.25,"payee":"WWW.GRAB.COM","category":null,"needs_review":true,"labels":[]}]`))
				return
			}
			if q.Get("start_date") != "2026-10-01" || q.Get("end_date") != "2026-10-31" || q.Get("per_page") != "100" {
				t.Errorf("query %s", r.URL.RawQuery)
			}
			w.Header().Set("Link", fmt.Sprintf(`<%s/transaction_accounts/7/transactions?page=2>; rel="next"`, srvURL))
			w.Write([]byte(`[
				{"id":1,"date":"2026-10-12","amount":-2.50,"payee":"WWW.GRAB.COM","category":{"id":9,"title":"Food"},"needs_review":false,"labels":["x"]},
				{"id":2,"date":"2026-10-12","amount":-40.00,"payee":"SOMEWHERE ELSE","category":{"id":4},"needs_review":false,"labels":[]}
			]`))
		},
	})
	srvURL = srv.URL
	first, _ := day.Parse("2026-10-01")
	last, _ := day.Parse("2026-10-31")
	got, err := c.Charges(context.Background(), 7, first, last, []string{"WWW.GRAB.COM", "GRABTAXI"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != 1 || got[1].ID != 3 {
		t.Fatalf("charges = %+v, want IDs 1 and 3", got)
	}
	if got[0].USD != 2.50 || day.Format(got[0].Date) != "2026-10-12" || got[0].Payee != "WWW.GRAB.COM" {
		t.Errorf("charge 1 = %+v", got[0])
	}
	if !got[0].State.Equal(ledger.State{Category: 9, Labels: []string{"x"}}) {
		t.Errorf("charge 1 state = %+v", got[0].State)
	}
	if got[1].State.Category != 0 || !got[1].State.NeedsReview {
		t.Errorf("uncategorized charge 3 state = %+v", got[1].State)
	}
}

func TestChargeReadsOneTransaction(t *testing.T) {
	c, _ := server(t, map[string]http.HandlerFunc{
		"GET /transactions/5": func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"id":5,"date":"2026-10-12","amount":-2.75,"payee":"WWW.GRAB.COM","category":{"id":7},"needs_review":false,"labels":["grab-auto"]}`))
		},
	})
	got, err := c.Charge(context.Background(), 5)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != 5 || !got.State.Equal(ledger.State{Category: 7, Labels: []string{"grab-auto"}}) {
		t.Errorf("Charge = %+v", got)
	}
}

func updateBody(t *testing.T, s ledger.State) map[string]any {
	t.Helper()
	var body map[string]any
	c, _ := server(t, map[string]http.HandlerFunc{
		"PUT /transactions/5": func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("content type %q", r.Header.Get("Content-Type"))
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			w.Write([]byte(`{"id":5}`))
		},
	})
	if err := c.Update(context.Background(), 5, s); err != nil {
		t.Fatal(err)
	}
	return body
}

func TestUpdateSendsCategoryReviewAndLabels(t *testing.T) {
	body := updateBody(t, ledger.State{Category: 7, NeedsReview: false, Labels: []string{"x", "grab-auto"}})
	if body["category_id"] != float64(7) || body["needs_review"] != false || body["labels"] != "x,grab-auto" {
		t.Errorf("body = %v", body)
	}
}

func TestUpdateSendsEmptyLabelsAndUncategorized(t *testing.T) {
	body := updateBody(t, ledger.State{Category: 0, NeedsReview: true})
	labels, ok := body["labels"]
	if !ok || labels != "" {
		t.Errorf("labels = %v (present %v), want an empty string", labels, ok)
	}
	if body["category_id"] != "" || body["needs_review"] != true {
		t.Errorf("body = %v", body)
	}
}

func TestErrorStatusIsReportedWithoutTheKey(t *testing.T) {
	c, _ := server(t, map[string]http.HandlerFunc{
		"GET /transactions/5": func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"error":"nope"}`, http.StatusForbidden)
		},
	})
	_, err := c.Charge(context.Background(), 5)
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("err = %v, want a 403", err)
	}
	if strings.Contains(err.Error(), key) {
		t.Errorf("error leaks the key: %v", err)
	}
}

func TestTimeZoneResolvesTheProfileZone(t *testing.T) {
	c, _ := server(t, map[string]http.HandlerFunc{
		"GET /me": func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"id":1,"time_zone":"Eastern Time (US & Canada)"}`))
		},
		"GET /time_zones": func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`[{"name":"Hawaii","identifier":"Pacific/Honolulu"},{"name":"Eastern Time (US & Canada)","identifier":"America/New_York"}]`))
		},
	})
	loc, err := c.TimeZone(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if loc.String() != "America/New_York" {
		t.Errorf("TimeZone = %s", loc)
	}
}

func TestTimeZoneFailsOnAnUnknownName(t *testing.T) {
	c, _ := server(t, map[string]http.HandlerFunc{
		"GET /me":         func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"time_zone":"Nowhere"}`)) },
		"GET /time_zones": func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`[]`)) },
	})
	if _, err := c.TimeZone(context.Background()); err == nil {
		t.Fatal("TimeZone succeeded for an unknown zone")
	}
}
