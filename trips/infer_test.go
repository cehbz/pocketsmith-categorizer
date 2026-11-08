package trips

import (
	"reflect"
	"testing"

	"github.com/cehbz/pocketsmith-categorizer/internal/day"
)

// cal is a piece of calendar evidence.
func cal(d string, s Sign, what string) Evidence {
	return Evidence{Day: date(d), Source: FromCalendar, Sign: s, What: what}
}

// stay is a calendar stay on each day from from to to.
func stay(from, to, what string) []Evidence {
	var out []Evidence
	for d := date(from); !d.After(date(to)); d = d.AddDate(0, 0, 1) {
		out = append(out, cal(day.Format(d), Stay, what))
	}
	return out
}

// infer infers the trips from txs and calendar for locals, with no trips listed.
func infer(txs []Transaction, calendar ...Evidence) []Trip {
	return Trips(Infer(txs, calendar, nil, locals, date("2025-01-01")))
}

func trip(from, to string) Trip { return Trip{Name: "trip-" + from, From: date(from), To: date(to)} }

func wantTrips(t *testing.T, got []Trip, want ...Trip) {
	t.Helper()
	if !reflect.DeepEqual(got, want) && len(got)+len(want) > 0 {
		t.Errorf("trips = %+v, want %+v", got, want)
	}
}

func TestACalendarFlightOrStayMakesATripThatKeepsItsEvidence(t *testing.T) {
	flights := []Evidence{cal("2025-03-10", FlightOut, "HMT-FAR ZZ101"), cal("2025-03-12", Flight, "FAR-BIG ZZ7"), cal("2025-03-15", FlightBack, "BIG-HMT ZZ8")}
	got := Infer(nil, flights, nil, locals, date("2025-01-01"))
	if len(got) != 1 || got[0].Trip != trip("2025-03-10", "2025-03-15") || !reflect.DeepEqual(got[0].Evidence, flights) {
		t.Errorf("Infer = %+v, want trip-2025-03-10 to 03-15 with its three flights", got)
	}
	wantTrips(t, infer(nil, stay("2025-04-01", "2025-04-03", "Stay at Inn Farcity")...), trip("2025-04-01", "2025-04-03"))
}

func TestCardEvidenceMakesATripOnlyAcrossANightWithNewPlacesAndGroundTransport(t *testing.T) {
	for name, c := range map[string]struct {
		txs  []Transaction
		want []Trip
	}{
		"new places and ground transport": {[]Transaction{inStore(1, "2025-05-10", -30, "RAIL FARCITY", "4111"), inStore(2, "2025-05-11", -9, "NOODLES FARCITY", "5812")},
			[]Trip{trip("2025-05-10", "2025-05-11")}},
		"new places and a booking":           {[]Transaction{inStore(1, "2025-05-10", -80, "INN FARCITY", "7011"), inStore(2, "2025-05-11", -9, "NOODLES FARCITY", "5812")}, nil},
		"a night's booking":                  {[]Transaction{inStore(1, "2025-05-10", -80, "INN FARCITY", "7011")}, nil},
		"new places only":                    {[]Transaction{inStore(1, "2025-05-10", -9, "CAFE FARCITY", "5812"), inStore(2, "2025-05-11", -9, "NOODLES FARCITY", "5812")}, nil},
		"a single day with ground transport": {[]Transaction{inStore(1, "2025-05-10", -30, "RAIL FARCITY", "4111"), inStore(2, "2025-05-10", -9, "NOODLES FARCITY", "5812")}, nil},
	} {
		if got := infer(c.txs); !reflect.DeepEqual(got, c.want) && len(got)+len(c.want) > 0 {
			t.Errorf("%s: trips = %+v, want %+v", name, got, c.want)
		}
	}
}

func TestASingleDayWithoutAStayOrLodgingIsHomeAFlightOutAndBackIncluded(t *testing.T) {
	wantTrips(t, infer([]Transaction{inStore(1, "2025-03-10", -9, "NOODLES FARCITY", "5812")},
		cal("2025-03-10", FlightOut, "HMT-FAR ZZ101"), cal("2025-03-10", FlightBack, "FAR-HMT ZZ102")))
	wantTrips(t, infer(nil, cal("2025-04-10", Flight, "Flight to Farcity (ZZ 912)")))
}

func TestRunsThreeDaysApartJoinWithoutHomeActivityBetweenAndSplitWithIt(t *testing.T) {
	calendar := append(stay("2025-03-10", "2025-03-11", "Stay at Inn Farcity"), stay("2025-03-14", "2025-03-15", "Stay at Lodge Bigcity")...)
	wantTrips(t, infer(nil, calendar...), trip("2025-03-10", "2025-03-15"))
	cafe := visits(1, "CAFE ONE", "2025-01-10", "2025-02-10", "2025-04-10", "2025-03-12") // settled, and charged between the runs
	wantTrips(t, infer(cafe, calendar...), trip("2025-03-10", "2025-03-11"), trip("2025-03-14", "2025-03-15"))
}

func TestAReturnHomeEndsATrip(t *testing.T) {
	txs := []Transaction{inStore(1, "2025-03-13", -30, "RAIL BIGCITY", "4111"), inStore(2, "2025-03-14", -9, "NOODLES BIGCITY", "5812")}
	wantTrips(t, infer(txs, cal("2025-03-10", FlightOut, "HMT-FAR ZZ101"), cal("2025-03-12", FlightBack, "FAR-HMT ZZ102")),
		trip("2025-03-10", "2025-03-12"), trip("2025-03-13", "2025-03-14"))
}

func TestATripStartsOnItsFlightOutNotOnNewPlacesBeforeIt(t *testing.T) {
	txs := []Transaction{inStore(1, "2025-03-09", -30, "RAIL NEWLY", "4111"), inStore(2, "2025-03-09", -9, "BAKERY NEWLY", "5812")}
	calendar := append(stay("2025-03-10", "2025-03-12", "Stay at Inn Farcity"), cal("2025-03-10", FlightOut, "HMT-FAR ZZ101"), cal("2025-03-13", FlightBack, "FAR-HMT ZZ102"))
	got := infer(txs, calendar...)
	if len(got) == 0 || got[len(got)-1] != trip("2025-03-10", "2025-03-13") {
		t.Errorf("trips = %+v, want the last to be trip-2025-03-10 to 03-13", got)
	}
}

func TestCardEvidenceAfterTheLastCalendarEvidenceExtendsTheTrip(t *testing.T) {
	txs := []Transaction{inStore(1, "2025-03-16", -9, "NOODLES BIGCITY", "5812"), inStore(2, "2025-03-17", -30, "RAIL BIGCITY", "4111")}
	calendar := append(stay("2025-03-10", "2025-03-12", "Stay at Inn Farcity"), cal("2025-03-10", FlightOut, "HMT-FAR ZZ101"))
	wantTrips(t, infer(txs, calendar...), trip("2025-03-10", "2025-03-17"))
}

func TestEvidenceBeforeTheHomeStartMakesNoTrip(t *testing.T) {
	txs := []Transaction{inStore(1, "2024-12-20", -30, "RAIL FARCITY", "4111"), inStore(2, "2024-12-21", -9, "NOODLES FARCITY", "5812")}
	// Read from before the home start, so only the home start excludes the card's evidence.
	wantTrips(t, Trips(Infer(txs, stay("2024-12-10", "2024-12-12", "Stay at Inn Farcity"), nil, locals, date("2024-12-01"))))
}

func TestAPlaceChargedOnTripsTheCalendarAloneShowsIsntSettledByThem(t *testing.T) {
	// The ramen shop is charged in three months, each time on a trip only the calendar shows,
	// and in the gap between the stays of a fourth: against the listed trips alone it would be
	// settled, home activity, and split the fourth trip. The café at home between the trips
	// keeps them apart.
	var calendar []Evidence
	for _, m := range []string{"02", "03", "04"} {
		calendar = append(calendar, stay("2025-"+m+"-10", "2025-"+m+"-12", "Stay at Inn Farcity")...)
	}
	calendar = append(append(calendar, stay("2025-06-01", "2025-06-03", "Stay at Inn Farcity")...), stay("2025-06-10", "2025-06-12", "Stay at Lodge Farcity")...)
	ramen := visits(1, "RAMEN FARCITY", "2025-02-11", "2025-03-11", "2025-04-11", "2025-06-07")
	cafe := visits(10, "CAFE ONE", "2025-01-20", "2025-02-20", "2025-03-20", "2025-05-20")
	got := infer(append(ramen, cafe...), calendar...)
	if want := trip("2025-06-01", "2025-06-12"); len(got) != 4 || got[3] != want {
		t.Errorf("trips = %+v; want four, the last %+v", got, want)
	}
}

func TestTheSameFlightsInTheCalendarTwiceMakeOneTrip(t *testing.T) {
	// Typed from the booking page and added from Gmail's confirmation.
	calendar := []Evidence{
		cal("2025-03-10", FlightOut, "HMT-FAR ZZ101"), cal("2025-03-10", FlightOut, "ZZ 101 HMT-FAR"),
		cal("2025-03-11", Flight, "Flight to Bigcity (ZZ 7)"),
		cal("2025-03-14", FlightBack, "FAR-HMT ZZ102"), cal("2025-03-14", FlightBack, "ZZ 102 FAR-HMT"),
	}
	wantTrips(t, infer(nil, calendar...), trip("2025-03-10", "2025-03-14"))
}
