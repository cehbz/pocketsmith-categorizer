package main

import (
	"testing"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/grab"
	"github.com/cehbz/pocketsmith-categorizer/internal/day"
)

func TestPeriodSpansEveryPostingWindow(t *testing.T) {
	bkk, _ := time.LoadLocation("Asia/Bangkok")
	ny, _ := time.LoadLocation("America/New_York")
	p := func(s string) grab.Payment {
		at, err := time.ParseInLocation("2006-01-02 15:04", s, bkk)
		if err != nil {
			t.Fatal(err)
		}
		return grab.Payment{At: at}
	}
	first, last := period([]grab.Payment{p("2026-10-12 14:00"), p("2026-05-13 16:00"), p("2026-10-28 20:00")}, ny)
	if day.Format(first) != "2026-05-13" || day.Format(last) != "2026-10-31" {
		t.Errorf("period = %s..%s, want 2026-05-13..2026-10-31", day.Format(first), day.Format(last))
	}
}
