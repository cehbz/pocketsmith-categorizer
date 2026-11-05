package evidence

import (
	"reflect"
	"testing"
	"time"
)

var bangkok = time.FixedZone("ICT", 7*3600)

func TestALazadaCheckoutsOrdersAreOneReceiptPayingTheirSum(t *testing.T) {
	at := time.Date(2026, 3, 4, 14, 5, 0, 0, bangkok)
	got := Checkouts([]Receipt{
		{Merchant: "lazada", At: at, Parts: []Part{{Ref: "900000000000002", Kind: Order, THB: 120}}, Checkout: "c1"},
		{Merchant: "grab", At: at.Add(-time.Hour), Parts: []Part{{Ref: "A-TESTRIDE0001", Kind: Ride, THB: 78}}},
		{Merchant: "lazada", At: at, Parts: []Part{{Ref: "900000000000001", Kind: Order, THB: 340}}, Checkout: "c1"},
		{Merchant: "lazada", At: at.Add(time.Hour), Parts: []Part{{Ref: "900000000000003", Kind: Order, THB: 99}}, Checkout: "c2"},
	})
	want := []Receipt{
		{Merchant: "grab", At: at.Add(-time.Hour), Parts: []Part{{Ref: "A-TESTRIDE0001", Kind: Ride, THB: 78}}},
		{Merchant: "lazada", At: at, Parts: []Part{{Ref: "900000000000001", Kind: Order, THB: 340}, {Ref: "900000000000002", Kind: Order, THB: 120}}, Checkout: "c1"},
		{Merchant: "lazada", At: at.Add(time.Hour), Parts: []Part{{Ref: "900000000000003", Kind: Order, THB: 99}}, Checkout: "c2"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Checkouts = %+v\nwant %+v", got, want)
	}
	if got[1].THB() != 460 || got[1].Ref() != "900000000000001+900000000000002" || got[1].Kind() != Order {
		t.Errorf("checkout pays %v as %q of kind %q; want 460 as 900000000000001+900000000000002 of kind order", got[1].THB(), got[1].Ref(), got[1].Kind())
	}
}

func TestABillsChargePaysTheBillAndTheFee(t *testing.T) {
	r := Receipt{Parts: []Part{{Ref: "EZBI0000000001", Kind: Electricity, THB: 812.5}}, Fee: 5.35}
	if r.THB() != 817.85 || r.Amountless() {
		t.Errorf("THB() = %v, amountless %t; want 817.85 with an amount", r.THB(), r.Amountless())
	}
	if invoice := (Receipt{Parts: []Part{{Ref: "tax invoice", Kind: Bill}}}); !invoice.Amountless() {
		t.Error("a receipt whose parts have no amount isn't amountless")
	}
}

func TestReviewLabelsReadInPlainWords(t *testing.T) {
	for label, want := range map[string]string{
		NoReceipt("lazada"): "no Lazada receipt",
		NoPurchase("grab"):  "no Grab purchase to refund",
	} {
		if got, ok := Phrase(label); !ok || got != want {
			t.Errorf("Phrase(%q) = %q, %t; want %q", label, got, ok, want)
		}
	}
	if _, ok := Phrase("attention-new-payee"); ok {
		t.Error("Phrase read another label")
	}
}
