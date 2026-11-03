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

func TestUpdateAppendsTripsOverlappingNoListedOrWrittenTrip(t *testing.T) {
	listed := Trip{Name: "listed", From: date("2026-03-01"), To: date("2026-03-10")}
	deleted := Trip{Name: "deleted", From: date("2026-05-01"), To: date("2026-05-05")}
	x := Trip{Name: "x", From: date("2026-03-08"), To: date("2026-03-12")}
	y := Trip{Name: "y", From: date("2026-05-04"), To: date("2026-05-06")}
	z := Trip{Name: "z", From: date("2026-07-01"), To: date("2026-07-03")}
	handmade := Trip{Name: "handmade", From: date("2026-09-01"), To: date("2026-09-05")} // listed by hand, never written
	v := Trip{Name: "v", From: date("2026-09-04"), To: date("2026-09-06")}
	list, c := List{listed, handmade}.Update([]Trip{x, y, z, v}, []Trip{listed, deleted})
	if !reflect.DeepEqual(list, List{listed, handmade, z}) || !reflect.DeepEqual(c, Listing{Added: []Trip{z}}) {
		t.Errorf("Update = %v, %+v; want the list with z, and z added", list, c)
	}
}

// inProgress is an inferred trip as a run on its third day wrote it.
var inProgress = Trip{Name: "trip-2026-11-01", From: date("2026-11-01"), To: date("2026-11-03")}

func TestUpdateExtendsAnUneditedTripInferredLongerFromTheSameDay(t *testing.T) {
	longer := Trip{Name: "trip-2026-11-01", From: date("2026-11-01"), To: date("2026-11-05")}
	list, c := List{inProgress}.Update([]Trip{longer}, []Trip{inProgress})
	if !reflect.DeepEqual(list, List{longer}) || !reflect.DeepEqual(c, Listing{Extended: []Extension{{Before: inProgress, After: longer}}}) {
		t.Fatalf("Update = %v, %+v; want the trip extended to 11-05", list, c)
	}
	if got := c.Record([]Trip{inProgress}); !reflect.DeepEqual(got, []Trip{longer}) {
		t.Errorf("Record = %v, want the extended trip in place of the one written", got)
	}
	if l, w := c.Undo(list, []Trip{longer}); !reflect.DeepEqual(l, List{inProgress}) || !reflect.DeepEqual(w, []Trip{inProgress}) {
		t.Errorf("Undo = %v, %v; want the trip back to 11-03 in each", l, w)
	}
}

func TestUpdateLeavesEditedTripsAndExtensionsThatWouldOverlapAnotherTrip(t *testing.T) {
	longer := Trip{Name: "trip-2026-11-01", From: date("2026-11-01"), To: date("2026-11-08")}
	renamed, visaRun, shorter := inProgress, inProgress, inProgress
	renamed.Name = "trip-2026-11-farcity"
	visaRun.VisaRun = true
	shorter.To = date("2026-11-02")
	laterStart := Trip{Name: "trip-2026-11-02", From: date("2026-11-02"), To: date("2026-11-08")}
	for name, c := range map[string]struct {
		list     List
		written  []Trip
		inferred Trip
	}{
		"renamed":            {List{renamed}, []Trip{inProgress}, longer},
		"visa run set":       {List{visaRun}, []Trip{inProgress}, longer},
		"dates edited":       {List{shorter}, []Trip{inProgress}, longer},
		"never written":      {List{inProgress}, nil, longer},
		"listed overlap":     {List{inProgress, {Name: "next", From: date("2026-11-07"), To: date("2026-11-09")}}, []Trip{inProgress}, longer},
		"deleted overlap":    {List{inProgress}, []Trip{inProgress, {Name: "gone", From: date("2026-11-08"), To: date("2026-11-09")}}, longer},
		"starts a day later": {List{inProgress}, []Trip{inProgress}, laterStart},
		"same dates":         {List{inProgress}, []Trip{inProgress}, inProgress},
	} {
		list, got := c.list.Update([]Trip{c.inferred}, c.written)
		if !reflect.DeepEqual(list, c.list) || !got.Empty() {
			t.Errorf("%s: Update = %v, %+v; want the list unchanged", name, list, got)
		}
	}
}

func TestUndoLeavesAnExtendedTripTheUserEditedSince(t *testing.T) {
	longer := inProgress
	longer.To = date("2026-11-05")
	edited := longer
	edited.Name = "trip-2026-11-farcity"
	c := Listing{Extended: []Extension{{Before: inProgress, After: longer}}}
	if l, w := c.Undo(List{edited}, []Trip{longer}); !reflect.DeepEqual(l, List{edited}) || !reflect.DeepEqual(w, []Trip{inProgress}) {
		t.Errorf("Undo = %v, %v; want the user's edit kept and the record restored", l, w)
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

func TestAnInferredTripReachingBeyondAnEditedTripIsAHint(t *testing.T) {
	asWritten := Trip{Name: "trip-2026-03-01", From: date("2026-03-01"), To: date("2026-03-10")}
	edited := Trip{Name: "trip-japan", From: date("2026-05-03"), To: date("2026-05-10")} // written as 05-01..05-10
	byHand := Trip{Name: "trip-farcity", From: date("2026-07-01"), To: date("2026-07-05")}
	written := []Trip{asWritten, {Name: "trip-2026-05-01", From: date("2026-05-01"), To: date("2026-05-10")}}
	inferred := []Inferred{
		{Trip: Trip{Name: "trip-2026-03-01", From: date("2026-03-01"), To: date("2026-03-12")}}, // extends a trip as written: Update's
		{Trip: Trip{Name: "trip-2026-05-01", From: date("2026-05-01"), To: date("2026-05-10")}}, // reaches before the edited trip
		{Trip: Trip{Name: "trip-2026-07-02", From: date("2026-07-02"), To: date("2026-07-04")}}, // inside the trip listed by hand
		{Trip: Trip{Name: "trip-2026-07-04", From: date("2026-07-04"), To: date("2026-07-08")}}, // beyond it
	}
	got := List{asWritten, edited, byHand}.Hints(inferred, written)
	want := []Hint{{Edited: edited, Inferred: inferred[1]}, {Edited: byHand, Inferred: inferred[3]}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Hints = %+v, want %+v", got, want)
	}
}
