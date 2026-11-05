package grab

import (
	"reflect"
	"testing"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/evidence"
)

func TestTheReaderReadsEachReceiptAsAReceiptOfOnePart(t *testing.T) {
	received := time.Date(2026, 11, 2, 6, 19, 0, 0, time.UTC)
	for _, c := range []struct {
		name, text string
		part       evidence.Part
	}{
		{"english ride", rideEN, evidence.Part{Ref: "A-TESTRIDE0001", Kind: evidence.Ride, THB: 78}},
		{"english tip", tipEN, evidence.Part{Ref: "A-TESTTIP00002", Kind: evidence.Ride, THB: 60}},
		{"thai food order", foodTH, evidence.Part{Ref: "A-TESTFOOD0003", Kind: evidence.Food, THB: 387}},
		{"thai mart order", martTH, evidence.Part{Ref: "A-TESTMART0004", Kind: evidence.Mart, THB: 320}},
		{"thai mart tip", tipTH, evidence.Part{Ref: "A-TESTTIPTH005", Kind: evidence.Mart, THB: 50}},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := Reader{}.Read(evidence.Message{ID: "m1", Received: received, Subject: "Your Grab E-Receipt", Text: c.text})
			if err != nil {
				t.Fatal(err)
			}
			want := evidence.Evidence{Receipts: []evidence.Receipt{{At: received, Parts: []evidence.Part{c.part}}}}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Read = %+v; want %+v", got, want)
			}
		})
	}
}

func TestTheReaderSaysWhyAReceiptIsUnreadable(t *testing.T) {
	_, err := Reader{}.Read(evidence.Message{ID: "m1", Text: `Standard Hope you enjoyed your ride! Booking ID: A-TESTRIDE0001`})
	if err == nil || err.Error() != "no total" {
		t.Errorf("Read error = %v; want no total", err)
	}
}

func TestGrabIsAMerchantOfRidesAndDeliveries(t *testing.T) {
	m := Merchant()
	if m.Name != "grab" || m.Label != "grab-auto" || !reflect.DeepEqual(m.Queries, []string{Query}) || m.Tolerance != 0.02 || m.Lag != MaxPostingLag ||
		!reflect.DeepEqual(m.Kinds, []evidence.Kind{evidence.Ride, evidence.Food, evidence.Mart, evidence.Express}) || m.Reader == nil {
		t.Errorf("Merchant() = %+v", m)
	}
}
