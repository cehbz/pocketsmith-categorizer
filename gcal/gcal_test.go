package gcal

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/cehbz/pocketsmith-categorizer/internal/day"
	"github.com/cehbz/pocketsmith-categorizer/trips"
)

func mustDay(s string) time.Time {
	d, err := day.Parse(s)
	if err != nil {
		panic(err)
	}
	return d
}

// calendar is a fake Calendar API serving items two to a page, recording each request's
// query and Authorization header; it fails a request after the last page, so a read that
// doesn't end fails an assertion instead of hanging.
func calendar(t *testing.T, items []map[string]any) (srv *httptest.Server, queries *[]map[string]string) {
	t.Helper()
	queries = &[]map[string]string{}
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/calendars/cal@example.com/events" {
			t.Errorf("path %s", r.URL.Path)
		}
		q := map[string]string{"auth": r.Header.Get("Authorization")}
		for k := range r.URL.Query() {
			q[k] = r.URL.Query().Get(k)
		}
		*queries = append(*queries, q)
		page := 0
		if tok := q["pageToken"]; tok != "" {
			page = int(tok[0] - '0')
		}
		if 2*page > len(items) {
			t.Errorf("page %d: the read should have ended", page)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		body := map[string]any{"items": items[2*page : min(2*page+2, len(items))]}
		if 2*page+2 < len(items) {
			body["nextPageToken"] = string(rune('0' + page + 1))
		}
		json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return srv, queries
}

// bearer is an HTTP client carrying access token tok.
func bearer(tok string) *http.Client {
	return oauth2.NewClient(context.Background(), oauth2.StaticTokenSource(&oauth2.Token{AccessToken: tok}))
}

func TestEventsDecodeOnTheirLocalDatesAcrossPages(t *testing.T) {
	srv, queries := calendar(t, []map[string]any{
		{"summary": "HMT-FAR ZZ101", "start": map[string]string{"dateTime": "2026-03-10T23:30:00Z", "timeZone": "Africa/Nairobi"},
			"end": map[string]string{"dateTime": "2026-03-11T03:00:00Z", "timeZone": "Africa/Nairobi"}},
		{"summary": "Stay at Inn Farcity", "location": "1 Harbour Road, Farcity", "start": map[string]string{"date": "2026-03-11"}, "end": map[string]string{"date": "2026-03-14"}},
		{"summary": "Dinner", "start": map[string]string{"dateTime": "2026-03-12T19:00:00-05:00"}, "end": map[string]string{"dateTime": "2026-03-12T21:00:00-05:00"}},
	})
	c := &Client{BaseURL: srv.URL, ID: "cal@example.com", HTTP: bearer("tok3n")}
	got, err := c.Events(context.Background(), mustDay("2026-03-01"), mustDay("2026-03-31"))
	if err != nil {
		t.Fatal(err)
	}
	want := []trips.Event{
		{Title: "HMT-FAR ZZ101", Start: mustDay("2026-03-11"), End: mustDay("2026-03-11")},
		{Title: "Stay at Inn Farcity", Location: "1 Harbour Road, Farcity", Start: mustDay("2026-03-11"), End: mustDay("2026-03-14"), AllDay: true},
		{Title: "Dinner", Start: mustDay("2026-03-12"), End: mustDay("2026-03-12")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Events = %+v, want %+v", got, want)
	}
	if len(*queries) != 2 {
		t.Fatalf("%d requests, want 2 pages", len(*queries))
	}
	q := (*queries)[0]
	if q["singleEvents"] != "true" || q["orderBy"] != "startTime" || q["timeMin"] != "2026-02-28T00:00:00Z" || q["timeMax"] != "2026-04-02T00:00:00Z" || q["auth"] != "Bearer tok3n" {
		t.Errorf("first request %v", q)
	}
	if (*queries)[1]["pageToken"] != "1" {
		t.Errorf("second request %v, want the next page", (*queries)[1])
	}
}

func TestAnAPIErrorFailsTheReadWithItsMessageAndNoToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"code":500,"message":"backend error","status":"INTERNAL"}}`))
	}))
	defer srv.Close()
	_, err := (&Client{BaseURL: srv.URL, ID: "cal@example.com", HTTP: bearer("tok3n")}).Events(context.Background(), mustDay("2026-03-01"), mustDay("2026-03-31"))
	if err == nil || !strings.Contains(err.Error(), "backend error") || strings.Contains(err.Error(), "tok3n") {
		t.Errorf("err = %v; want the API's message without the token", err)
	}
}

func TestARejectedTokenSaysToRunAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) }))
	defer srv.Close()
	_, err := (&Client{BaseURL: srv.URL, ID: "cal@example.com", HTTP: bearer("tok3n")}).Events(context.Background(), mustDay("2026-03-01"), mustDay("2026-03-31"))
	if !errors.Is(err, ErrAuth) || !strings.Contains(err.Error(), "categorize auth") {
		t.Errorf("err = %v; want ErrAuth", err)
	}
}

func TestACalendarIDIsEscapedInThePath(t *testing.T) {
	id := "en.farland#holiday@group.v.calendar.google.com"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/calendars/"+id+"/events" {
			t.Errorf("path %q, want the id whole", r.URL.Path)
		}
		w.Write([]byte(`{"items":[]}`))
	}))
	defer srv.Close()
	if _, err := (&Client{BaseURL: srv.URL, ID: id, HTTP: bearer("tok3n")}).Events(context.Background(), mustDay("2026-03-01"), mustDay("2026-03-31")); err != nil {
		t.Fatal(err)
	}
}
