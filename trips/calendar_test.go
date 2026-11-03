package trips

import (
	"reflect"
	"testing"
)

var flyer = Home{Places: []string{"HOMETOWN"}, Airports: []string{"HMT"}}

// event is a timed event from start to end, local dates.
func event(title, location, start, end string) Event {
	return Event{Title: title, Location: location, Start: date(start), End: date(end)}
}

func TestAFlightsRouteLeavesHomeFromAHomeAirportReturnsToOneAndIsAwayOtherwise(t *testing.T) {
	for _, c := range []struct {
		e    Event
		want Evidence
	}{
		{event("HMT-FAR ZZ103", "", "2025-03-10", "2025-03-10"), cal("2025-03-10", FlightOut, "HMT-FAR ZZ103")},
		{event("ZZ 155 FAR-HMT", "", "2025-03-20", "2025-03-21"), cal("2025-03-21", FlightBack, "ZZ 155 FAR-HMT")},
		{event("BIG - HMT QQ134", "", "2025-04-01", "2025-04-01"), cal("2025-04-01", FlightBack, "BIG - HMT QQ134")},
		{event("FAR-BIG (QQ 273)", "", "2025-03-15", "2025-03-16"), cal("2025-03-15", Flight, "FAR-BIG (QQ 273)")},
		{event("Flight to Farcity (ZZ 912)", "", "2025-05-02", "2025-05-02"), cal("2025-05-02", Flight, "Flight to Farcity (ZZ 912)")},
	} {
		if got := c.e.Evidence(flyer); !reflect.DeepEqual(got, []Evidence{c.want}) {
			t.Errorf("%q: evidence %+v, want %+v", c.e.Title, got, c.want)
		}
	}
}

func TestAStayAwayIsEvidenceFromCheckInThroughCheckOut(t *testing.T) {
	timed := event("Stay at Inn Farcity", "1 Harbour Road, Farcity", "2025-03-10", "2025-03-12")
	if got, want := timed.Evidence(flyer), stay("2025-03-10", "2025-03-12", "Stay at Inn Farcity"); !reflect.DeepEqual(got, want) {
		t.Errorf("timed stay: %+v, want %+v", got, want)
	}
	allDay := Event{Title: "Stay at Lodge Bigcity", Start: date("2025-04-01"), End: date("2025-04-04"), AllDay: true}
	if got, want := allDay.Evidence(flyer), stay("2025-04-01", "2025-04-03", "Stay at Lodge Bigcity"); !reflect.DeepEqual(got, want) {
		t.Errorf("all-day stay: %+v, want %+v", got, want)
	}
}

func TestAStayNamingHomeAndOtherEventsAreNoEvidence(t *testing.T) {
	for _, e := range []Event{
		event("Stay at Old Town Guesthouse", "3 Temple Lane, Hometown", "2025-12-31", "2026-01-01"),
		event("Dentist", "Hometown Clinic", "2025-03-10", "2025-03-10"),
		event("Call with the bank", "", "2025-03-11", "2025-03-11"),
	} {
		if got := e.Evidence(flyer); len(got) > 0 {
			t.Errorf("%q: evidence %+v, want none", e.Title, got)
		}
	}
}
