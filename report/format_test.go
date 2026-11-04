package report

import (
	"testing"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/attention"
	"github.com/cehbz/pocketsmith-categorizer/trips"
)

func TestAttentionLabelsReadAsPhrases(t *testing.T) {
	for label, want := range map[string]string{
		attention.NewPayee:       "new payee",
		attention.Large:          "larger than usual",
		attention.Duplicate:      "possible duplicate",
		attention.Undecided:      "no category decided",
		attention.MonthlyIncome:  "monthly income",
		attention.IncomeMismatch: "income off the estimate",
		trips.Placed:             "placed on a trip",
		trips.Unsure:             "trip placement in doubt",
		"attention-odd-hour":     "odd-hour",
	} {
		if got := phrase(label); got != want {
			t.Errorf("phrase(%q) = %q, want %q", label, got, want)
		}
	}
}

func TestDatesCarryTheirYearOnlyWhenItIsntTheRuns(t *testing.T) {
	for _, c := range []struct {
		d    time.Time
		want string
	}{
		{time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC), "Nov 4"},
		{time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC), "Jan 31"},
		{time.Date(2025, 9, 13, 0, 0, 0, 0, time.UTC), "Sep 13, 2025"},
	} {
		if got := date(c.d, 2026); got != c.want {
			t.Errorf("date(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestAmountsCarryASignAndThousandsSeparators(t *testing.T) {
	for v, want := range map[float64]string{
		-23.47:      "−23.47",
		61.05:       "+61.05",
		2408.3:      "+2,408.30",
		-1234567.89: "−1,234,567.89",
		-999.999:    "−1,000.00",
		0:           "0.00",
		-0.001:      "0.00",
	} {
		if got := amount(v); got != want {
			t.Errorf("amount(%v) = %q, want %q", v, got, want)
		}
	}
	if got := money(4321); got != "4,321.00" {
		t.Errorf("money(4321) = %q, want 4,321.00", got)
	}
}

func TestTripsAreNamedWithoutTheirPrefix(t *testing.T) {
	for name, want := range map[string]string{
		"trip-2026-07-lakeside":    "Lakeside trip",
		"trip-2026-05-port-hollow": "Port Hollow trip",
		"trip-farcity":             "Farcity trip",
		trips.Booking:              "trip booking",
		"trip-2026-03-10":          "trip-2026-03-10",
	} {
		if got := tripName(name); got != want {
			t.Errorf("tripName(%q) = %q, want %q", name, got, want)
		}
	}
}
