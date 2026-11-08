package trips

import (
	"slices"
	"testing"
)

// cardSigns are the signs of the card evidence at place on day d.
func cardSigns(evidence []Evidence, place, d string) []Sign {
	var out []Sign
	for _, e := range evidence {
		if e.What == place && e.Day.Equal(date(d)) && e.Source == FromCard {
			out = append(out, e.Sign)
		}
	}
	slices.Sort(out)
	return out
}

// settledCafe is a café charged in three months outside trips, and on 2025-05-10.
func settledCafe() []Transaction {
	return visits(1, "CAFE ONE", "2025-01-10", "2025-02-10", "2025-03-10", "2025-05-10")
}

func TestABookingIsNoEvidence(t *testing.T) {
	txs := []Transaction{inStore(10, "2025-05-10", -95, "INN FARCITY", "7011"), inStore(11, "2025-05-10", -60, "RAIL FARCITY", "4112"),
		inStore(12, "2025-05-10", -400, "2C2P THA", "3077")}
	if got := NewPlaces(txs, locals).Haunts(nil).Evidence(date("2025-01-01")); len(got) > 0 {
		t.Errorf("evidence = %+v, want none", got)
	}
}

func TestGroundTransportAtAPlaceIsEvidenceButGrabIsnt(t *testing.T) {
	txs := append(settledCafe(), inStore(10, "2025-05-10", -30, "RAIL FARCITY", "4111"), inStore(11, "2025-05-10", -3, "GRAB FARCITY", "4121"),
		inStore(12, "2025-05-10", -20, "BUS HOMETOWN", "4131"))
	got := NewPlaces(txs, locals).Haunts(nil).Evidence(date("2025-01-01"))
	if s := cardSigns(got, "RAIL FARCITY", "2025-05-10"); !slices.Equal(s, []Sign{GroundTransport, NewPlace}) {
		t.Errorf("rail: %v, want ground transport and a new place", s)
	}
	if s := cardSigns(got, "BUS HOMETOWN", "2025-05-10"); !slices.Equal(s, []Sign{GroundTransport}) {
		t.Errorf("bus at home: %v, want ground transport only", s)
	}
	if s := cardSigns(got, "GRAB FARCITY", "2025-05-10"); len(s) > 0 {
		t.Errorf("grab: %v, want no evidence", s)
	}
}

func TestAPlaceChargedForTheFirstTimeOrAFavouriteIsEvidenceButASettledOneIsnt(t *testing.T) {
	txs := append(visits(1, "NOODLES FARCITY", "2025-05-10", "2025-05-11"), visits(10, "CAFE ONE", "2025-01-10", "2025-02-10", "2025-03-10", "2025-05-10")...)
	txs = append(txs, visits(20, "RAMEN FARCITY", "2025-06-05", "2025-06-06", "2025-09-05", "2025-09-06", "2025-11-10")...)
	txs = append(txs, visits(30, "OLD CAFE", "2025-04-01", "2025-05-10")...)
	got := NewPlaces(txs, locals).Haunts(List{june, sept}).Evidence(date("2025-05-01"))
	for _, c := range []struct {
		place, day string
		want       []Sign
	}{
		{"NOODLES FARCITY", "2025-05-10", []Sign{NewPlace}},
		{"NOODLES FARCITY", "2025-05-11", nil},
		{"CAFE ONE", "2025-05-10", nil},
		{"RAMEN FARCITY", "2025-11-10", []Sign{FavouritePlace}},
		{"OLD CAFE", "2025-05-10", nil}, // first charged before evidence counts
	} {
		if s := cardSigns(got, c.place, c.day); !slices.Equal(s, c.want) {
			t.Errorf("%s on %s: %v, want %v", c.place, c.day, s, c.want)
		}
	}
}
