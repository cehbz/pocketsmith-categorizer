package evidence

import (
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/internal/day"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

var ny = mustLoad("America/New_York")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

func date(s string) time.Time {
	d, err := day.Parse(s)
	if err != nil {
		panic(err)
	}
	return d
}

// flat is a rate of thb per USD on every date.
func flat(thb float64) Rates { return func(time.Time) (float64, error) { return thb, nil } }

// lazada is a merchant like Lazada: a 1% tolerance and a day's lag.
var lazada = Merchant{Name: "lazada", Tolerance: 0.01, Lag: 1, Payees: []string{"LAZADA PAY"}, Start: date("2024-12-10")}

// order is a Lazada order's receipt placed at 14:00 Bangkok time on d, early that day in New
// York, the ledger's time zone.
func order(d string, ref string, thb float64) Receipt {
	return Receipt{Merchant: "lazada", At: date(d).Add(7 * time.Hour), Parts: []Part{{Ref: ref, Kind: Order, THB: thb}}}
}

func charge(id int64, d string, usd float64) ledger.Transaction {
	return ledger.Transaction{ID: id, Date: date(d), Amount: -usd, Payee: "LAZADA PAY"}
}

func credit(id int64, d string, usd float64) ledger.Transaction {
	return ledger.Transaction{ID: id, Date: date(d), Amount: usd, Payee: "LAZADA PAY"}
}

func match(t *testing.T, m Merchant, ev Evidence, txs ...ledger.Transaction) Matching {
	t.Helper()
	got, err := m.Match(ev, txs, flat(35), ny)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// paid maps each charge paid to the receipt that it paid.
func paid(m Matching) map[int64]string {
	out := map[int64]string{}
	for _, p := range m.Paid {
		out[p.Charge.ID] = p.Receipt.Ref()
	}
	return out
}

func ids(txs []ledger.Transaction) []int64 {
	var out []int64
	for _, t := range txs {
		out = append(out, t.ID)
	}
	slices.Sort(out)
	return out
}

func TestALazadaCheckoutOfSeveralOrdersPaysOneCharge(t *testing.T) {
	// 340 + 120 THB at 35 with the markup is 13.18 USD.
	a, b := order("2026-03-04", "900000000000001", 340), order("2026-03-04", "900000000000002", 120)
	a.Checkout, b.Checkout = "2026-03-04T07:00:00Z", "2026-03-04T07:00:00Z"
	m := match(t, lazada, Evidence{Receipts: []Receipt{b, a}}, charge(1, "2026-03-04", 13.18))
	if want := map[int64]string{1: "900000000000001+900000000000002"}; !reflect.DeepEqual(paid(m), want) || len(m.Unmatched) != 0 {
		t.Errorf("paid %v, unmatched %+v; want %v", paid(m), m.Unmatched, want)
	}
}

func TestABillAndItsFeePayOneCharge(t *testing.T) {
	// 150 + 5.35 THB at 35 with the markup is 4.45 USD; 150 alone would be 4.30.
	bill := Receipt{Merchant: "lazada", At: date("2026-03-04").Add(7 * time.Hour), Parts: []Part{{Ref: "EZBI0000000001", Kind: Water, THB: 150}}, Fee: 5.35}
	m := match(t, lazada, Evidence{Receipts: []Receipt{bill}}, charge(1, "2026-03-04", 4.45))
	if want := map[int64]string{1: "EZBI0000000001"}; !reflect.DeepEqual(paid(m), want) {
		t.Errorf("paid %v; want %v", paid(m), want)
	}
}

func TestATaxInvoiceExplainsAChargeWithinADayThatNothingElseExplains(t *testing.T) {
	invoice := Receipt{Merchant: "lazada", At: date("2026-03-10").Add(3 * time.Hour), Parts: []Part{{Ref: "tax invoice", Kind: Bill}}, Unpriced: true} // 23:00 on 03-09 in New York
	far := Receipt{Merchant: "lazada", At: date("2026-03-20").Add(3 * time.Hour), Parts: []Part{{Ref: "tax invoice", Kind: Bill}}, Unpriced: true}
	m := match(t, lazada, Evidence{Receipts: []Receipt{order("2026-03-10", "900000000000001", 350), invoice, far}},
		charge(1, "2026-03-10", 10.03), // the order's
		charge(2, "2026-03-10", 7.00),  // the invoice's bill
		charge(3, "2026-03-23", 9.00))  // three days after the far invoice
	if want := map[int64]string{1: "900000000000001", 2: "tax invoice"}; !reflect.DeepEqual(paid(m), want) {
		t.Errorf("paid %v; want %v", paid(m), want)
	}
	if !slices.Equal(ids(m.Unexplained), []int64{3}) || len(m.Unmatched) != 1 || !m.Unmatched[0].At.Equal(far.At) {
		t.Errorf("unexplained %v, unmatched %+v; want charge 3 and the far invoice", ids(m.Unexplained), m.Unmatched)
	}
}

func TestTwoSmallChargesAFewCentsApartMatchTheirReceiptsTogether(t *testing.T) {
	// At 35 with the markup, 100 THB is 2.8654 USD and 101 THB 2.8941. Greedily, 100 THB
	// takes the nearer 2.875 and leaves 101 THB 2.85, 1.5% off; together, both are within 1%.
	m := match(t, lazada, Evidence{Receipts: []Receipt{order("2026-03-04", "900000000000001", 100), order("2026-03-04", "900000000000002", 101)}}, charge(1, "2026-03-04", 2.875), charge(2, "2026-03-04", 2.85))
	if want := map[int64]string{1: "900000000000002", 2: "900000000000001"}; !reflect.DeepEqual(paid(m), want) {
		t.Errorf("paid %v; want %v", paid(m), want)
	}
}

func TestAChargeAtTheRateOfTheDayBeforeOrAfterIsPaid(t *testing.T) {
	byDay := map[string]float64{"2026-03-03": 35, "2026-03-04": 36, "2026-03-05": 37}
	rates := func(d time.Time) (float64, error) { return byDay[day.Format(d)], nil }
	// 350 THB at 35 is 10.03 USD; 700 THB at 37 is 18.97. At the day's own 36 both are 2.7% off.
	ev := Evidence{Receipts: []Receipt{order("2026-03-04", "900000000000001", 350), order("2026-03-04", "900000000000002", 700)}}
	m, err := lazada.Match(ev, []ledger.Transaction{charge(1, "2026-03-04", 10.03), charge(2, "2026-03-04", 18.97)}, rates, ny)
	if err != nil {
		t.Fatal(err)
	}
	if want := map[int64]string{1: "900000000000001", 2: "900000000000002"}; !reflect.DeepEqual(paid(m), want) {
		t.Errorf("paid %v; want %v", paid(m), want)
	}
}

func TestAChargeOutsideTheToleranceOrTheLagIsntPaid(t *testing.T) {
	for _, c := range []struct {
		name   string
		charge ledger.Transaction
		paid   bool
	}{
		{"within 1%", charge(1, "2026-03-04", 10.12), true},
		{"over 1%", charge(1, "2026-03-04", 10.14), false},
		{"a day later", charge(1, "2026-03-05", 10.03), true},
		{"two days later", charge(1, "2026-03-06", 10.03), false},
		{"the day before", charge(1, "2026-03-03", 10.03), false},
	} {
		m := match(t, lazada, Evidence{Receipts: []Receipt{order("2026-03-04", "900000000000001", 350)}}, c.charge)
		if got := len(m.Paid) == 1; got != c.paid {
			t.Errorf("%s: paid %t; want %t", c.name, got, c.paid)
		}
	}
}

func TestOnlyTheMerchantsChargesFromItsStartAreMatched(t *testing.T) {
	other := charge(4, "2026-03-04", 10.03)
	other.Payee = "OTHER SHOP"
	grabs := order("2026-03-04", "A-TESTRIDE0001", 350)
	grabs.Merchant = "grab"
	m := match(t, lazada, Evidence{Receipts: []Receipt{grabs}},
		charge(1, "2024-12-09", 5), // before the start: left to the rules
		charge(2, "2024-12-10", 5),
		credit(3, "2026-03-04", 10.03),
		other)
	if len(m.Paid) != 0 || len(m.Unmatched) != 0 || !slices.Equal(ids(m.Unexplained), []int64{2}) {
		t.Errorf("paid %v, unmatched %+v, unexplained %v; want only charge 2 unexplained", paid(m), m.Unmatched, ids(m.Unexplained))
	}
}

func TestARateErrorFailsTheMatch(t *testing.T) {
	down := errors.New("no rate")
	_, err := lazada.Match(Evidence{Receipts: []Receipt{order("2026-03-04", "900000000000001", 350)}}, []ledger.Transaction{charge(1, "2026-03-04", 10.03)},
		func(time.Time) (float64, error) { return 0, down }, ny)
	if !errors.Is(err, down) {
		t.Errorf("err = %v; want the rate's", err)
	}
}

func TestAnOrderPlacedInTheBangkokMorningPaysAChargeDatedTheDayBeforeInNewYork(t *testing.T) {
	early := order("2026-03-04", "900000000000001", 350)
	early.At = time.Date(2026, 3, 4, 9, 30, 0, 0, time.FixedZone("ICT", 7*3600)) // 2026-03-03 21:30 in New York
	m := match(t, lazada, Evidence{Receipts: []Receipt{early}}, charge(1, "2026-03-03", 10.03))
	if want := map[int64]string{1: "900000000000001"}; !reflect.DeepEqual(paid(m), want) {
		t.Errorf("paid %v; want %v", paid(m), want)
	}
}

func TestAFridayChargePricedAtMondaysRateIsPaid(t *testing.T) {
	// 2026-03-06 is a Friday. The table carries Friday's rate over the weekend; Monday's differs.
	byDay := map[string]float64{"2026-03-05": 35, "2026-03-06": 35, "2026-03-07": 35, "2026-03-08": 35, "2026-03-09": 37}
	rates := func(d time.Time) (float64, error) { return byDay[day.Format(d)], nil }
	// 350 THB at Monday's 37 is 9.49 USD; at Friday's 35 it is 10.03.
	ev := Evidence{Receipts: []Receipt{order("2026-03-06", "900000000000001", 350)}}
	m, err := lazada.Match(ev, []ledger.Transaction{charge(1, "2026-03-06", 9.49)}, rates, ny)
	if err != nil {
		t.Fatal(err)
	}
	if want := map[int64]string{1: "900000000000001"}; !reflect.DeepEqual(paid(m), want) {
		t.Errorf("paid %v; want %v", paid(m), want)
	}
}

func TestAReceiptNettedToZeroExplainsNoCharge(t *testing.T) {
	free := Receipt{Merchant: "lazada", At: date("2026-03-04").Add(7 * time.Hour), Parts: []Part{{Ref: "900000000000001", Kind: Order}}}
	m := match(t, lazada, Evidence{Receipts: []Receipt{free}}, charge(1, "2026-03-04", 7.00))
	if len(m.Paid) != 0 || !slices.Equal(ids(m.Unexplained), []int64{1}) {
		t.Errorf("paid %v, unexplained %v; want charge 1 unexplained", paid(m), ids(m.Unexplained))
	}
}

func TestATaxInvoiceDuplicatingAPricedBillExplainsNoOtherCharge(t *testing.T) {
	bill := Receipt{Merchant: "lazada", At: date("2026-03-04").Add(7 * time.Hour), Parts: []Part{{Ref: "EZBI0000000001", Kind: Electricity, THB: 150}}, Fee: 5.35}
	invoice := Receipt{Merchant: "lazada", At: bill.At.Add(time.Minute), Parts: []Part{{Ref: "tax invoice", Kind: Bill}}, Unpriced: true}
	m := match(t, lazada, Evidence{Receipts: []Receipt{invoice, bill}}, charge(1, "2026-03-04", 4.45), charge(2, "2026-03-04", 7.00))
	if want := map[int64]string{1: "EZBI0000000001"}; !reflect.DeepEqual(paid(m), want) {
		t.Errorf("paid %v; want %v", paid(m), want)
	}
	if !slices.Equal(ids(m.Unexplained), []int64{2}) || len(m.Unmatched) != 0 {
		t.Errorf("unexplained %v, unmatched %+v; want charge 2 and no unmatched receipt", ids(m.Unexplained), m.Unmatched)
	}
}
