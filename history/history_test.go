package history

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/grab"
)

var bkk = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Bangkok")
	if err != nil {
		panic(err)
	}
	return loc
}()

// statement mimics `pdftotext -layout` output of a Grab history statement (synthetic data).
// ตลาด (talat): "market".
const statement = `                                                                     2026-05-01 - 2026-10-28

                                                                     Member Statement
        1 Example Road
Total Trips: 5   Amount: THB 700.00
Date/Time            Booking
       please visit the GrabCode           Pick-up Address                        Drop-off Address      Fleet Type     Currency    Amount


28 Oct 2026,
                    A-TESTAAAA1111AV        Cafe One                              Home                  Standard       THB         80.00
08:00PM


27 Oct 2026,        A-                      ตลาด Two
                                                                                  Home                  GrabMart       THB        400.00
02:00PM             TESTBBBB2222AV          Second line of address

Date/Time            Booking
       please visit the GrabCode           Pick-up Address                        Drop-off Address      Fleet Type     Currency    Amount

26 Oct 2026,
                    A-TESTCCCC3333AV        Home                                  Office                Standard       THB          0.00
10:15AM


25 Oct 2026,
                    A-TESTDDDD4444AV        Office                                Home                  Standard (JustGrab)       THB        100.00
11:30AM


24 Oct 2026,                      Hotel Lobby                                                                        Standard
               A-TESTEEEE5555AV                                                Seafood Place                        THB        120.00
12:00PM                           Second line                                                                       (JustGrab)
`

func at(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, bkk)
	if err != nil {
		panic(err)
	}
	return t
}

func TestParseReadsTripsAndDropsZeroTotals(t *testing.T) {
	got, err := Parse(statement, "alice", bkk)
	if err != nil {
		t.Fatal(err)
	}
	want := []grab.Payment{
		{Booking: "A-TESTAAAA1111AV", Service: "Standard", THB: 80, At: at("2026-10-28 20:00"), Account: "alice"},
		{Booking: "A-TESTBBBB2222AV", Service: "GrabMart", THB: 400, At: at("2026-10-27 14:00"), Account: "alice"},
		{Booking: "A-TESTDDDD4444AV", Service: "Standard (JustGrab)", THB: 100, At: at("2026-10-25 11:30"), Account: "alice"},
		{Booking: "A-TESTEEEE5555AV", Service: "Standard", THB: 120, At: at("2026-10-24 12:00"), Account: "alice"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d payments, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.Booking != w.Booking || g.Service != w.Service || g.THB != w.THB || !g.At.Equal(w.At) || g.Account != w.Account {
			t.Errorf("payment %d = %+v, want %+v", i, g, w)
		}
	}
}

func TestParseFailsWhenTheHeaderDisagrees(t *testing.T) {
	for name, text := range map[string]string{
		"trip count": strings.Replace(statement, "Total Trips: 5", "Total Trips: 6", 1),
		"amount":     strings.Replace(statement, "THB 700.00", "THB 701.00", 1),
	} {
		if _, err := Parse(text, "alice", bkk); err == nil {
			t.Errorf("%s: Parse accepted a mismatched header", name)
		}
	}
}

func TestParseFailsWithoutAHeader(t *testing.T) {
	if _, err := Parse(strings.Replace(statement, "Total Trips:", "Trips:", 1), "alice", bkk); err == nil {
		t.Fatal("Parse accepted a statement with no header")
	}
}

func TestParseFailsOnAnUnknownService(t *testing.T) {
	text := strings.Replace(statement, "Standard (JustGrab)       THB", "GrabCar       THB", 1)
	if _, err := Parse(text, "alice", bkk); err == nil {
		t.Fatal("Parse accepted an unknown service")
	}
}

func TestMergeDropsDuplicateBookings(t *testing.T) {
	a := []grab.Payment{{Booking: "A-1"}, {Booking: "A-2"}}
	b := []grab.Payment{{Booking: "A-2"}, {Booking: "A-3"}}
	got := Merge(a, b)
	if len(got) != 3 || got[0].Booking != "A-1" || got[1].Booking != "A-2" || got[2].Booking != "A-3" {
		t.Errorf("Merge = %+v", got)
	}
}

func TestTextRunsPdftotextInLayoutMode(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "pdftotext")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := Text(context.Background(), bin, "/x/statement.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(got) != "-layout /x/statement.pdf -" {
		t.Errorf("Text ran %q", got)
	}
}
