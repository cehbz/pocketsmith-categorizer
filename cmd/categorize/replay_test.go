package main

import (
	"context"
	"reflect"
	"testing"

	"github.com/cehbz/pocketsmith-categorizer/ledger"
	"github.com/cehbz/pocketsmith-categorizer/trips"
)

func TestReplayInfersTripsFromTheStartOfThePeriodAndReportsOnlyThePeriods(t *testing.T) {
	card := ledger.Account{Name: "Card"}
	all := []ledger.Transaction{
		{ID: 1, Date: mustParse("2025-05-10"), Amount: -80, Payee: "INN FARCITY", Account: card},
		{ID: 2, Date: mustParse("2026-01-20"), Amount: -90, Payee: "LODGE FARCITY", Account: card},
		{ID: 3, Date: mustParse("2025-08-01"), Amount: -5, Payee: "GROCER", Account: card}, // at home between them
		{ID: 4, Date: mustParse("2025-09-02"), Amount: -6, Payee: "GROCER", Account: card},
	}
	recs := []trips.Record{
		{Account: "Card", Date: mustParse("2025-05-10"), Amount: 80, Name: "INN FARCITY", Merchant: "Inn", Code: "7011", InStore: true},
		{Account: "Card", Date: mustParse("2026-01-20"), Amount: 90, Name: "LODGE FARCITY", Merchant: "Lodge", Code: "7011", InStore: true},
	}
	s := &session{today: mustParse("2026-11-03"), card: &fakeCard{recs: recs}, accounts: []string{"Card"},
		trips: trips.Settings{Kinds: trips.Kinds{Codes: trips.DefaultCodes()}}}
	got, err := replayTrips(context.Background(), s, all, mustParse("2026-01-01"))
	if err != nil {
		t.Fatal(err)
	}
	want := []trips.Trip{{Name: "trip-2026-01-20", From: mustParse("2026-01-20"), To: mustParse("2026-01-20")}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("replayTrips = %+v, want %+v", got, want)
	}
}
