package grab

import (
	"testing"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/internal/day"
)

func TestParseServiceClassifiesKnownLabels(t *testing.T) {
	tests := []struct {
		label string
		want  Service
		kind  Kind
	}{
		{"Standard", "Standard", Ride},
		{"Standard (JustGrab)", "Standard (JustGrab)", Ride},
		{"JustGrab", "JustGrab", Ride},
		{"JustGrab (SVB)", "JustGrab (SVB)", Ride},
		{"Premium", "Premium", Ride},
		{"Taxi", "Taxi", Ride},
		{"RodDaeng", "RodDaeng", Ride},
		{"GrabBike", "GrabBike", Ride},
		{"Bike", "Bike", Ride},
		{"GrabFood", "GrabFood", Delivery},
		{"GrabMart", "GrabMart", Delivery},
		{"GrabExpress", "GrabExpress", Delivery},
		// สั่งซื้อด้วย GrabMart (sang sue duay GrabMart): "order with GrabMart"
		{"สั่งซื้อด้วย GrabMart", "GrabMart", Delivery},
		{"  GrabFood ", "GrabFood", Delivery},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			got, err := ParseService(tt.label)
			if err != nil {
				t.Fatalf("ParseService(%q): %v", tt.label, err)
			}
			if got != tt.want {
				t.Errorf("ParseService(%q) = %q, want %q", tt.label, got, tt.want)
			}
			if k := got.Kind(); k != tt.kind {
				t.Errorf("%q.Kind() = %v, want %v", got, k, tt.kind)
			}
		})
	}
}

func TestParseServiceRejectsUnknownLabel(t *testing.T) {
	if _, err := ParseService("GrabCar"); err == nil {
		t.Fatal("ParseService(GrabCar) succeeded, want error")
	}
}

func TestLabelsListsLongestFirst(t *testing.T) {
	ls := Labels()
	if ls[0] != "Standard (JustGrab)" {
		t.Errorf("Labels()[0] = %q, want Standard (JustGrab)", ls[0])
	}
	for i := 1; i < len(ls); i++ {
		if len(ls[i]) > len(ls[i-1]) {
			t.Errorf("Labels() not longest first at %d: %q after %q", i, ls[i], ls[i-1])
		}
	}
}

func TestPostingWindowStartsOnTheLedgerDate(t *testing.T) {
	bkk, err := time.LoadLocation("Asia/Bangkok")
	if err != nil {
		t.Fatal(err)
	}
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct{ name, at, first, last string }{
		{"daylight time, 11 hours behind", "2026-10-12 14:00", "2026-10-12", "2026-10-15"},
		{"before DST ends", "2026-10-31 11:30", "2026-10-31", "2026-11-03"},
		{"after DST ends, 12 hours behind", "2026-11-02 11:30", "2026-11-01", "2026-11-04"},
		{"after DST ends, same day", "2026-11-02 12:30", "2026-11-02", "2026-11-05"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			at, err := time.ParseInLocation("2006-01-02 15:04", tt.at, bkk)
			if err != nil {
				t.Fatal(err)
			}
			first, last := Payment{At: at}.PostingWindow(ny)
			if day.Format(first) != tt.first || day.Format(last) != tt.last {
				t.Errorf("PostingWindow(%s) = %s..%s, want %s..%s", tt.at, day.Format(first), day.Format(last), tt.first, tt.last)
			}
		})
	}
}
