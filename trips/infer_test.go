package trips

import (
	"reflect"
	"testing"
)

var inferring = Settings{Kinds: Kinds{Codes: DefaultCodes()}, Places: []string{"HOME TOWN"}}

// store is an in-store charge from merchant with code.
func store(id int64, d, merchant, code string) Transaction {
	c := charge(id, d, -10, merchant)
	c.Evidence.Code, c.Evidence.InStore = code, true
	return c
}

func TestSignalDaysTwoApartAreOneLegThreeApartTwo(t *testing.T) {
	txs := []Transaction{store(1, "2026-03-10", "A", "5812"), store(2, "2026-03-12", "B", "5812"), store(3, "2026-03-15", "C", "5812")}
	got := Legs(txs, inferring, date("2026-03-01"))
	want := []Leg{{From: date("2026-03-10"), To: date("2026-03-12")}, {From: date("2026-03-15"), To: date("2026-03-15")}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Legs = %+v, want %+v", got, want)
	}
}

func TestOnlyNewInStoreMerchantsAndInStoreLodgingSignal(t *testing.T) {
	online := charge(4, "2026-03-11", -10, "ONLINE SHOP")
	hotelSeen := store(6, "2026-02-02", "HOTEL", "7011")
	hotelSeen.Evidence.InStore = false
	txs := []Transaction{
		store(1, "2026-02-01", "SEEN", "5812"), store(2, "2026-03-10", "SEEN", "5812"),
		store(3, "2026-02-15", "BEFORE START", "5812"),
		online,
		store(5, "2026-03-12", "CAFE HOME TOWN", "5812"),
		hotelSeen, store(7, "2026-03-20", "HOTEL", "7011"),
	}
	got := Legs(txs, inferring, date("2026-03-01"))
	want := []Leg{{From: date("2026-03-20"), To: date("2026-03-20"), Lodging: true}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Legs = %+v, want only the in-store lodging day", got)
	}
}

func TestADayTripIsHomeButANightWithLodgingIsATrip(t *testing.T) {
	if got := Infer([]Transaction{store(1, "2026-03-10", "NEARBY CAFE", "5812")}, inferring, date("2026-03-01")); len(got) != 0 {
		t.Errorf("Infer = %+v, want no trip for a day trip", got)
	}
	got := Infer([]Transaction{store(1, "2026-03-20", "HOTEL", "7011")}, inferring, date("2026-03-01"))
	want := []Trip{{Name: "trip-2026-03-20", From: date("2026-03-20"), To: date("2026-03-20")}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Infer = %+v, want %+v", got, want)
	}
}

// twoLegs are legs on 03-10..03-12 and 03-30..03-31 with a chain store charged before, on the
// first leg and between them.
func twoLegs() []Transaction {
	return []Transaction{
		charge(1, "2026-02-01", -3, "SEVEN"),
		store(2, "2026-03-10", "A", "5812"), charge(3, "2026-03-11", -3, "SEVEN"), store(4, "2026-03-12", "B", "5812"),
		charge(5, "2026-03-20", -3, "SEVEN"),
		store(6, "2026-03-30", "C", "5812"), store(7, "2026-03-31", "D", "5812"),
	}
}

func TestLegsWithoutHomeActivityBetweenThemAreOneTrip(t *testing.T) {
	got := Infer(twoLegs(), inferring, date("2026-03-01"))
	want := []Trip{{Name: "trip-2026-03-10", From: date("2026-03-10"), To: date("2026-03-31")}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Infer = %+v, want one trip", got)
	}
}

func TestHomeActivityBetweenLegsSplitsThem(t *testing.T) {
	txs := append(twoLegs(), charge(8, "2026-02-03", -4, "LOCAL"), charge(9, "2026-03-21", -4, "LOCAL"))
	got := Infer(txs, inferring, date("2026-03-01"))
	want := []Trip{
		{Name: "trip-2026-03-10", From: date("2026-03-10"), To: date("2026-03-12")},
		{Name: "trip-2026-03-30", From: date("2026-03-30"), To: date("2026-03-31")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Infer = %+v, want two trips", got)
	}
}

func TestAHomeCafeChargedOnALegSomeoneWasHomeForSplitsTheLegs(t *testing.T) {
	s := inferring
	s.Places = []string{"HOMETOWN"}
	txs := []Transaction{
		charge(1, "2026-02-01", -4, "CAFE"),
		store(2, "2026-03-10", "A", "5812"), charge(3, "2026-03-11", -4, "CAFE"), charge(4, "2026-03-11", -2, "MART HOMETOWN"), store(5, "2026-03-12", "B", "5812"),
		charge(6, "2026-03-20", -4, "CAFE"),
		store(7, "2026-03-30", "C", "5812"), store(8, "2026-03-31", "D", "5812"),
	}
	got := Infer(txs, s, date("2026-03-01"))
	want := []Trip{
		{Name: "trip-2026-03-10", From: date("2026-03-10"), To: date("2026-03-12")},
		{Name: "trip-2026-03-30", From: date("2026-03-30"), To: date("2026-03-31")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Infer = %+v, want the cafe's charge at home to split the legs", got)
	}
}

func TestAChainChargedAtHomeAndOnOneLegDoesntSplitTheTrip(t *testing.T) {
	txs := []Transaction{
		charge(1, "2026-02-01", -3, "CHAIN"),
		store(2, "2026-03-10", "A", "5812"), store(3, "2026-03-12", "B", "5812"),
		charge(4, "2026-03-20", -3, "CHAIN"),
		store(5, "2026-03-30", "C", "5812"), charge(6, "2026-03-30", -3, "CHAIN"), store(7, "2026-03-31", "D", "5812"),
	}
	got := Infer(txs, inferring, date("2026-03-01"))
	want := []Trip{{Name: "trip-2026-03-10", From: date("2026-03-10"), To: date("2026-03-31")}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Infer = %+v, want one trip: a chain at home and on one leg shows nobody home", got)
	}
}
