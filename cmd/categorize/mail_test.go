package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/evidence"
	"github.com/cehbz/pocketsmith-categorizer/gmail"
	"github.com/cehbz/pocketsmith-categorizer/grab"
	"github.com/cehbz/pocketsmith-categorizer/lazada"
)

// fakeMailbox is a mailbox holding the messages each query finds. It records each search
// and the ids it read in full. A search fails with err when err isn't nil and errOn is
// empty or the search's query.
type fakeMailbox struct {
	messages map[string][]gmail.Message // by query
	err      error
	errOn    string
	searches []search
	read     []string
}

// search is one query over a window of days.
type search struct {
	query    string
	from, to time.Time
}

func (m *fakeMailbox) Messages(_ context.Context, query string, from, to time.Time, skip func(id string) bool) ([]gmail.Message, error) {
	m.searches = append(m.searches, search{query, from, to})
	if m.err != nil && (m.errOn == "" || m.errOn == query) {
		return nil, m.err
	}
	var out []gmail.Message
	for _, msg := range m.messages[query] {
		if skip != nil && skip(msg.ID) {
			continue
		}
		m.read = append(m.read, msg.ID)
		out = append(out, msg)
	}
	return out, nil
}

const (
	rideReceipt = `Grab E-Receipt | Standard | | Hope you enjoyed your ride! Booking ID: A-TESTRIDE0001 | | Total Paid | ฿70 | Grab Thailand`
	noTotal     = `Standard Hope you enjoyed your ride! Booking ID: A-TESTRIDE0003`
)

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// ride is a Grab ride's receipt arriving at received.
func ride(id, received string) gmail.Message {
	return gmail.Message{ID: id, Received: at(received), From: "no-reply@grab.com", To: "a@example.com", Subject: "Your Grab E-Receipt", Text: rideReceipt}
}

// mailSession is a session on 2026-11-03 reading Grab's and Lazada's mail from box.
func mailSession(box *fakeMailbox) *session {
	return &session{today: mustParse("2026-11-03"), receipts: box, merchants: []evidence.Merchant{grab.Merchant(), lazada.Merchant()}, loc: time.UTC}
}

// emptyStore is a store in a new file.
func emptyStore(t *testing.T) (*evidence.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "evidence.json")
	s, err := evidence.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return s, path
}

func load(t *testing.T, path string) *evidence.Store {
	t.Helper()
	s, err := evidence.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAnEmptyStoreReadsTheLookbackOnceAndKeepsWhatItRead(t *testing.T) {
	unreadable := ride("m3", "2026-10-29T08:00:00Z")
	unreadable.Text = noTotal
	box := &fakeMailbox{messages: map[string][]gmail.Message{grab.Query: {ride("m1", "2026-11-01T05:00:00Z"), unreadable}}}
	store, path := emptyStore(t)
	start := mustParse("2026-10-01")
	if err := readMail(context.Background(), mailSession(box), store, start); err != nil {
		t.Fatal(err)
	}
	queries := append([]string{grab.Query}, lazada.Queries...)
	if len(box.searches) != len(queries) {
		t.Fatalf("searches %v; want one of each of %q", box.searches, queries)
	}
	for i, q := range queries {
		if want := (search{q, start, mustParse("2026-11-03")}); box.searches[i] != want {
			t.Errorf("search %d = %v; want %v", i, box.searches[i], want)
		}
	}
	got := load(t, path)
	ride := evidence.Receipt{Merchant: "grab", At: at("2026-11-01T05:00:00Z"), Parts: []evidence.Part{{Ref: "A-TESTRIDE0001", Kind: evidence.Ride, THB: 70}}}
	if !got.ReadTo.Equal(mustParse("2026-11-03")) || !reflect.DeepEqual(got.Evidence(), evidence.Evidence{Receipts: []evidence.Receipt{ride}}) {
		t.Errorf("saved store read to %v with %+v; want read to 2026-11-03 with the ride", got.ReadTo, got.Evidence())
	}
	if u := got.Unreadable(); len(u) != 1 || u[0].ID != "m3" || u[0].Merchant != "grab" || u[0].Unreadable != "no total" {
		t.Errorf("unreadable %+v; want m3, Grab's, without a total", u)
	}
}

func TestARunReadsFromTwoDaysBeforeItsReadPointAndNoMessageTwice(t *testing.T) {
	store, _ := emptyStore(t)
	store.ReadTo = mustParse("2026-11-01")
	store.Add(evidence.Read{ID: "m1", Merchant: "grab", Received: at("2026-10-31T05:00:00Z")})
	order := gmail.Message{ID: "m4", Received: at("2026-11-02T07:06:00Z"), Subject: "We have received your order No 900000000000001",
		Text: "Your Order #900000000000001 placed on November 2, 2026 at 14:05 via Credit or Debit Card has been confirmed. Total (VAT included) ฿460.00"}
	box := &fakeMailbox{messages: map[string][]gmail.Message{
		grab.Query:        {ride("m1", "2026-10-31T05:00:00Z"), ride("m2", "2026-11-02T05:00:00Z")},
		lazada.Queries[0]: {order},
		lazada.Queries[1]: {order}, // found by two searches
	}}
	if err := readMail(context.Background(), mailSession(box), store, mustParse("2026-10-01")); err != nil {
		t.Fatal(err)
	}
	for _, s := range box.searches {
		if !s.from.Equal(mustParse("2026-10-30")) {
			t.Errorf("search %v; want it from 2026-10-30", s)
		}
	}
	if !slices.Equal(box.read, []string{"m2", "m4"}) || !store.Holds("m2") || !store.Holds("m4") {
		t.Errorf("read %v; want m2 and m4 once each, kept", box.read)
	}
}

func TestAReadPointOlderThanTheLookbackReadsFromItsStart(t *testing.T) {
	store, _ := emptyStore(t)
	store.ReadTo = mustParse("2025-01-01")
	box := &fakeMailbox{}
	if err := readMail(context.Background(), mailSession(box), store, mustParse("2026-10-01")); err != nil {
		t.Fatal(err)
	}
	if len(box.searches) == 0 || !box.searches[0].from.Equal(mustParse("2026-10-01")) {
		t.Errorf("searches %v; want them from the lookback's start, 2026-10-01", box.searches)
	}
}

func TestAFailedReadLeavesTheStoreAsItWas(t *testing.T) {
	store, path := emptyStore(t)
	store.ReadTo = mustParse("2026-11-01")
	store.Add(evidence.Read{ID: "m1", Merchant: "grab", Received: at("2026-10-31T05:00:00Z")})
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	quota := errors.New("gmail: 403 Forbidden: Quota exceeded")
	box := &fakeMailbox{messages: map[string][]gmail.Message{grab.Query: {ride("m2", "2026-11-02T05:00:00Z")}}, err: quota, errOn: lazada.Queries[0]}
	if err := readMail(context.Background(), mailSession(box), store, mustParse("2026-10-01")); !errors.Is(err, quota) {
		t.Fatalf("readMail = %v; want the quota error", err)
	}
	for name, s := range map[string]*evidence.Store{"in memory": store, "on disk": load(t, path)} {
		if !s.ReadTo.Equal(mustParse("2026-11-01")) || !s.Holds("m1") || s.Holds("m2") {
			t.Errorf("%s: read to %v, holding m1 %t, m2 %t; want read to 2026-11-01 holding m1 alone", name, s.ReadTo, s.Holds("m1"), s.Holds("m2"))
		}
	}
}

func TestEvidenceOlderThanTheLookbackLeavesTheStore(t *testing.T) {
	store, path := emptyStore(t)
	store.ReadTo = mustParse("2026-11-01")
	store.Add(evidence.Read{ID: "old", Merchant: "grab", Received: at("2026-09-30T23:00:00Z")})
	store.Add(evidence.Read{ID: "kept", Merchant: "grab", Received: at("2026-10-01T01:00:00Z")})
	if err := readMail(context.Background(), mailSession(&fakeMailbox{}), store, mustParse("2026-10-01")); err != nil {
		t.Fatal(err)
	}
	if s := load(t, path); s.Holds("old") || !s.Holds("kept") {
		t.Errorf("saved store holds old %t, kept %t; want kept alone", s.Holds("old"), s.Holds("kept"))
	}
}

func TestTheStoreHoldsNoMessageText(t *testing.T) {
	store, path := emptyStore(t)
	box := &fakeMailbox{messages: map[string][]gmail.Message{grab.Query: {ride("m1", "2026-11-01T05:00:00Z")}}}
	if err := readMail(context.Background(), mailSession(box), store, mustParse("2026-10-01")); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"Hope you enjoyed", "a@example.com", "no-reply@grab.com", "Your Grab E-Receipt"} {
		if strings.Contains(string(b), text) {
			t.Errorf("the store holds %q:\n%s", text, b)
		}
	}
}
