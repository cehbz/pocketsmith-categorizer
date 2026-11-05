package evidence

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAStoreKeepsItsReadsAndReadPointAcrossASave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "evidence.json")
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !s.ReadTo.IsZero() || s.Holds("m1") {
		t.Fatalf("a missing file loads as read to %v holding m1 %t; want an empty store", s.ReadTo, s.Holds("m1"))
	}
	ride := Receipt{At: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC), Parts: []Part{{Ref: "A-TESTRIDE0001", Kind: Ride, THB: 78}}}
	s.Add(Read{ID: "m2", Merchant: "grab", Received: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC), Evidence: Evidence{Receipts: []Receipt{ride}}})
	s.Add(Read{ID: "m1", Merchant: "lazada", Received: time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC), Unreadable: "no order total"})
	s.ReadTo = time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	for p, want := range map[string]os.FileMode{path: 0o600, filepath.Dir(path): 0o700} {
		info, err := os.Stat(p)
		if err != nil {
			t.Errorf("%s: %v; want mode %v", p, err, want)
		} else if info.Mode().Perm() != want {
			t.Errorf("%s: mode %v; want mode %v", p, info.Mode().Perm(), want)
		}
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	ride.Merchant = "grab"
	if !got.ReadTo.Equal(s.ReadTo) || !got.Holds("m1") || !got.Holds("m2") ||
		!reflect.DeepEqual(got.Evidence(), Evidence{Receipts: []Receipt{ride}}) {
		t.Errorf("loaded store read to %v with evidence %+v; want read to %v with the ride, holding m1 and m2", got.ReadTo, got.Evidence(), s.ReadTo)
	}
	if u := got.Unreadable(); len(u) != 1 || u[0].ID != "m1" || u[0].Merchant != "lazada" || u[0].Unreadable != "no order total" {
		t.Errorf("unreadable = %+v; want m1, Lazada's, without an order total", u)
	}
}

func TestEvidenceGivesEachPieceItsReadsMerchantInArrivalOrder(t *testing.T) {
	s, _ := Load(filepath.Join(t.TempDir(), "evidence.json"))
	day := func(d int) time.Time { return time.Date(2026, 3, d, 9, 0, 0, 0, time.UTC) }
	s.Add(Read{ID: "m2", Merchant: "lazada", Received: day(5), Evidence: Evidence{Cancellations: []Cancellation{{Ref: "900000000000001", At: day(5)}}}})
	s.Add(Read{ID: "m1", Merchant: "lazada", Received: day(4), Evidence: Evidence{Receipts: []Receipt{{At: day(4), Parts: []Part{{Ref: "900000000000001", Kind: Order, THB: 340}}}}}})
	s.Add(Read{ID: "m3", Merchant: "lazada", Received: day(6), Evidence: Evidence{Deliveries: []Delivery{{Ref: "900000000000002", At: day(6)}}}})
	want := Evidence{
		Receipts:      []Receipt{{Merchant: "lazada", At: day(4), Parts: []Part{{Ref: "900000000000001", Kind: Order, THB: 340}}}},
		Cancellations: []Cancellation{{Merchant: "lazada", Ref: "900000000000001", At: day(5)}},
		Deliveries:    []Delivery{{Merchant: "lazada", Ref: "900000000000002", At: day(6)}},
	}
	if got := s.Evidence(); !reflect.DeepEqual(got, want) {
		t.Errorf("Evidence() = %+v\nwant %+v", got, want)
	}
}

func TestDropForgetsMessagesReceivedBeforeADay(t *testing.T) {
	s, _ := Load(filepath.Join(t.TempDir(), "evidence.json"))
	s.Add(Read{ID: "old", Merchant: "grab", Received: time.Date(2025, 11, 3, 23, 59, 0, 0, time.UTC)})
	s.Add(Read{ID: "kept", Merchant: "grab", Received: time.Date(2025, 11, 4, 0, 0, 0, 0, time.UTC)})
	s.Drop(time.Date(2025, 11, 4, 0, 0, 0, 0, time.UTC))
	if s.Holds("old") || !s.Holds("kept") {
		t.Errorf("after Drop: old %t, kept %t; want only kept", s.Holds("old"), s.Holds("kept"))
	}
}

func TestACorruptStoreFailsNamingItsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evidence.json")
	if err := os.WriteFile(path, []byte(`{"read_to": "2026-11`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("Load error = %v; want one naming %s", err, path)
	}
}
