package day

import (
	"testing"
	"time"
)

func TestOfUsesTheLocationsCalendarDate(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 12, 2, 0, 0, 0, time.UTC) // Oct 11 22:00 in New York
	got := Of(at, ny)
	want := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("Of = %v, want %v", got, want)
	}
}

func TestParseFormatAndBetween(t *testing.T) {
	a, err := Parse("2026-10-30")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Parse("2026-11-02")
	if err != nil {
		t.Fatal(err)
	}
	if n := Between(a, b); n != 3 {
		t.Errorf("Between = %d, want 3", n)
	}
	if n := Between(b, a); n != -3 {
		t.Errorf("Between reversed = %d, want -3", n)
	}
	if s := Format(b); s != "2026-11-02" {
		t.Errorf("Format = %q, want 2026-11-02", s)
	}
}
