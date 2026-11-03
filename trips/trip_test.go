package trips

import (
	"reflect"
	"testing"
)

func TestATripHoldsItsDatesAndOverlapsATripSharingADay(t *testing.T) {
	a := Trip{Name: "a", From: date("2026-03-01"), To: date("2026-03-10")}
	if !a.Contains(date("2026-03-01")) || !a.Contains(date("2026-03-10")) || a.Contains(date("2026-03-11")) {
		t.Error("Contains isn't inclusive of exactly the trip's dates")
	}
	if !a.Overlaps(Trip{From: date("2026-03-10"), To: date("2026-03-12")}) || a.Overlaps(Trip{From: date("2026-03-11"), To: date("2026-03-12")}) {
		t.Error("Overlaps is wrong at the trip's last day")
	}
	if a.Version() != "a 2026-03-01..2026-03-10" || a.VersionKey() != "version a 2026-03-01..2026-03-10" {
		t.Errorf("Version = %q, VersionKey = %q", a.Version(), a.VersionKey())
	}
}

func TestAtPicksTheFirstOfOverlappingTrips(t *testing.T) {
	l := List{{Name: "first", From: date("2026-03-01"), To: date("2026-03-10")}, {Name: "second", From: date("2026-03-05"), To: date("2026-03-15")}}
	if got, ok := l.At(date("2026-03-07")); !ok || got.Name != "first" {
		t.Errorf("At = %v, %v, want first", got, ok)
	}
	if _, ok := l.At(date("2026-03-20")); ok {
		t.Error("At found a trip for a day outside every trip")
	}
}

func TestAddAppendsTripsOverlappingNoListedOrWrittenTrip(t *testing.T) {
	listed := Trip{Name: "listed", From: date("2026-03-01"), To: date("2026-03-10")}
	deleted := Trip{Name: "deleted", From: date("2026-05-01"), To: date("2026-05-05")}
	x := Trip{Name: "x", From: date("2026-03-08"), To: date("2026-03-12")}
	y := Trip{Name: "y", From: date("2026-05-04"), To: date("2026-05-06")}
	z := Trip{Name: "z", From: date("2026-07-01"), To: date("2026-07-03")}
	list, added := List{listed}.Add([]Trip{x, y, z}, []Trip{listed, deleted})
	if !reflect.DeepEqual(list, List{listed, z}) || !reflect.DeepEqual(added, []Trip{z}) {
		t.Errorf("Add = %v, %v; want the list with z, and z", list, added)
	}
}

func TestWithoutDropsTheNamedTrips(t *testing.T) {
	a, b := Trip{Name: "a"}, Trip{Name: "b"}
	if got := Without([]Trip{a, b}, []string{"a"}); !reflect.DeepEqual(got, []Trip{b}) {
		t.Errorf("Without = %v, want b", got)
	}
	if !(List{a}).Named("a") || (List{a}).Named("b") || !reflect.DeepEqual(Names([]Trip{a, b}), []string{"a", "b"}) {
		t.Error("Named or Names is wrong")
	}
}
