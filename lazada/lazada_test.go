package lazada

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/evidence"
)

// Made-up messages in the layout of Lazada's and easyBills' mail, as text.
const (
	orderEN = `Hi Test Buyer, Your Order #900000000000001 placed on March 4, 2026 at 14:05 via Credit or Debit Card has been confirmed. Example Kettle ฿450.00 Subtotal ฿450.00 Shipping Fee ฿30.00 Voucher -฿20.00 Total (VAT included) ฿460.00`
	// orderTH is orderEN in the Thai template: ขอบคุณที่ช้อปกับลาซาด้า (khop khun thi chop kap
	// lasada, "thank you for shopping with Lazada"), สวัสดีคุณ (sawatdi khun, "hello"),
	// เราได้รับหมายเลขคำสั่งซื้อ (rao dai rap mai lek kham sang sue, "we have received order
	// number"), ของคุณเมื่อ (khong khun muea, "of yours on"), มีนาคม (minakhom, "March"), 2569 the
	// Buddhist-era year 2026, the time running into ผ่านช่องทาง (phan chongthang, "via
	// channel"), บัตรเครดิต/เดบิต (bat khredit/debit, "credit/debit card"), แล้ว (laeo,
	// "already"), ยอดรวมทั้งหมด(รวม VAT) (yot ruam thang mot (ruam VAT), "grand total (incl. VAT)").
	orderTH = `ขอบคุณที่ช้อปกับลาซาด้า สวัสดีคุณ Test Buyer, เราได้รับหมายเลขคำสั่งซื้อ 900000000000001 ของคุณเมื่อ 4 มีนาคม 2569 14:05ผ่านช่องทาง บัตรเครดิต/เดบิต แล้ว! Example Kettle THB 450.00 ยอดรวมทั้งหมด(รวม VAT): THB 460.00`
	// billText is an easyBills receipt, its biller left to fill in: ใบรับฝากเงิน (bai rap fak
	// ngoen, "payment receipt"), วันที่ (wan thi, "date", DD/MM/YYYY, Gregorian), เรียน ลูกค้า
	// (rian lukkha, "dear customer"), คุณได้ทำการชำระบิลสำเร็จแล้ว (khun dai tham kan chamra bin
	// samret laeo, "you have paid the bill"), บริการที่รับชำระ (borikan thi rap chamra, "service
	// paid"), หมายเลขอ้างอิง (mai lek angking, "reference number"), ชำระโดย (chamra doi, "paid
	// by"), จำนวนเงิน (บาท) (chamnuan ngoen (baht), "amount (baht)", a column's head), ยอดชำระ
	// (yot chamra, "amount paid"), รหัสตรวจสอบ (rahat truat sop, "verification code").
	billText = `Thank you for using EasyBills ใบรับฝากเงิน วันที่ 04/03/2026 14:05:31 เรียน ลูกค้า, คุณได้ทำการชำระบิลสำเร็จแล้ว บริการที่รับชำระ %s หมายเลขอ้างอิง EZBI000000000000001 ชำระโดย Lazada_utilPayment จำนวนเงิน (บาท) ยอดชำระ 1,812.50 รหัสตรวจสอบ EZBI000000000000002`
)

var received = time.Date(2026, 3, 4, 7, 6, 0, 0, time.UTC)

func read(t *testing.T, m evidence.Message) evidence.Evidence {
	t.Helper()
	if m.Received.IsZero() {
		m.Received = received
	}
	ev, err := Reader{}.Read(m)
	if err != nil {
		t.Fatalf("Read(%q): %v", m.Subject, err)
	}
	return ev
}

func TestAnOrderIsACheckoutOfOnePartPlacedInBangkok(t *testing.T) {
	got := read(t, evidence.Message{ID: "m1", From: "no-reply@example.lazada.co.th", Subject: "We have received your order No 900000000000001", Text: orderEN})
	at := time.Date(2026, 3, 4, 14, 5, 0, 0, bangkok)
	want := evidence.Evidence{Receipts: []evidence.Receipt{{At: at, Parts: []evidence.Part{{Ref: "900000000000001", Kind: evidence.Order, THB: 460}}, Checkout: "2026-03-04T07:05:00Z"}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Read = %+v\nwant %+v", got, want)
	}
}

func TestAThaiOrderWithABuddhistEraDateReadsAsTheEnglishOne(t *testing.T) {
	en := read(t, evidence.Message{ID: "m1", Subject: "We have received your order No 900000000000001", Text: orderEN})
	th := read(t, evidence.Message{ID: "m2", Subject: "ขอบคุณสำหรับคำสั่งซื้อ", Text: orderTH}) // khop khun sam rap kham sang sue: "thank you for your order"
	if !reflect.DeepEqual(th, en) {
		t.Errorf("Thai order = %+v\nwant %+v", th, en)
	}
}

func TestAnOrderTotalWithoutDecimalsOrSpacesReads(t *testing.T) {
	text := strings.Replace(orderEN, "฿460.00", "฿1,460", 1)
	got := read(t, evidence.Message{ID: "m1", Subject: "We have received your order No 900000000000001", Text: text})
	if len(got.Receipts) != 1 || got.Receipts[0].THB() != 1460 {
		t.Errorf("Read = %+v; want an order of 1,460 THB", got)
	}
}

func TestABillLazadaPaidIsItsUtilitysKindWithLazadasFee(t *testing.T) {
	for _, c := range []struct {
		biller string
		kind   evidence.Kind
	}{
		{"การไฟฟ้าส่วนภูมิภาค", evidence.Electricity}, // kan fai fa suan phumiphak: Provincial Electricity Authority
		{"การประปาส่วนภูมิภาค", evidence.Water},       // kan prapa suan phumiphak: Provincial Waterworks Authority
		{"Example Telecom", evidence.Bill},
	} {
		got := read(t, evidence.Message{ID: "m1", From: "easybills@2c2p.com", Subject: "easyBills", Text: fmt.Sprintf(billText, c.biller)})
		want := evidence.Evidence{Receipts: []evidence.Receipt{{At: received, Parts: []evidence.Part{{Ref: "EZBI000000000000001", Kind: c.kind, THB: 1812.5}}, Fee: 5.35}}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: Read = %+v\nwant %+v", c.biller, got, want)
		}
	}
}

func TestATaxInvoiceNamingNoOrderIsABillWithoutAnAmount(t *testing.T) {
	subject := "ใบกำกับภาษีอิเล็กทรอนิกส์ Lazada" // bai kam kap phasi elekthronik: "electronic tax invoice"
	got := read(t, evidence.Message{ID: "m1", Subject: subject, Text: "Lazada e-Tax Invoice"})
	want := evidence.Evidence{Receipts: []evidence.Receipt{{At: received, Parts: []evidence.Part{{Ref: "tax invoice", Kind: evidence.Bill}}, Unpriced: true}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Read = %+v\nwant %+v", got, want)
	}
}

// taxInvoiceDecomposed is ใบกำกับภาษีอิเล็กทรอนิกส์ (bai kam kap phasi elekthronik, "electronic
// tax invoice") as Lazada's mail spells it: the sara am of กำ (kam) as U+0E4D nikhahit then U+0E32
// sara aa, not the composed U+0E33; the two render alike.
const taxInvoiceDecomposed = "\u0e43\u0e1a\u0e01\u0e4d\u0e32\u0e01\u0e31\u0e1a\u0e20\u0e32\u0e29\u0e35\u0e2d\u0e34\u0e40\u0e25\u0e47\u0e01\u0e17\u0e23\u0e2d\u0e19\u0e34\u0e01\u0e2a\u0e4c"

func TestATaxInvoiceSpelledWithADecomposedSaraAmReadsAlike(t *testing.T) {
	want := evidence.Evidence{Receipts: []evidence.Receipt{{At: received, Parts: []evidence.Part{{Ref: "tax invoice", Kind: evidence.Bill}}, Unpriced: true}}}
	if got := read(t, evidence.Message{ID: "m1", Subject: taxInvoiceDecomposed + " Lazada", Text: "Lazada e-Tax Invoice"}); !reflect.DeepEqual(got, want) {
		t.Errorf("subject: Read = %+v\nwant %+v", got, want)
	}
	if got := read(t, evidence.Message{ID: "m1", Subject: taxInvoiceDecomposed + " Lazada", Text: "Order #900000000000001"}); !reflect.DeepEqual(got, evidence.Evidence{}) {
		t.Errorf("an order's invoice: Read = %+v; want nothing", got)
	}
}

func TestMessagesThatSayNothingOfAPurchaseReadAsNothing(t *testing.T) {
	for name, m := range map[string]evidence.Message{
		// ใบกำกับภาษีอิเล็กทรอนิกส์: bai kam kap phasi elekthronik, "electronic tax invoice"
		"a tax invoice for an order": {Subject: "ใบกำกับภาษีอิเล็กทรอนิกส์ Lazada", Text: "Order #900000000000001"},
		// การไฟฟ้าส่วนภูมิภาค: kan fai fa suan phumiphak, "Provincial Electricity Authority"
		"a bill Lazada didn't pay": {From: "easybills@2c2p.com", Text: fmt.Sprintf(strings.Replace(billText, "Lazada_utilPayment", "CounterService", 1), "การไฟฟ้าส่วนภูมิภาค")},
	} {
		if got := read(t, m); !reflect.DeepEqual(got, evidence.Evidence{}) {
			t.Errorf("%s: Read = %+v; want nothing", name, got)
		}
	}
}

func TestCancellationsAndDeliveriesNameTheirOrder(t *testing.T) {
	for _, c := range []struct {
		subject, text string
		want          evidence.Evidence
	}{
		{"[Cancellation] Your Order #900000000000002 has been cancelled", "", cancellation("900000000000002")},
		{"Item(s) in Order #900000000000002 has been cancelled", "", cancellation("900000000000002")},
		{"Order Cancellation Notification", "We are sorry, Order #900000000000002 was cancelled.", cancellation("900000000000002")},
		{"Apologies for Cancelling Your Choice Order", "Order number 900000000000002", cancellation("900000000000002")},
		{"Order number 900000000000003 has been successfully shipped", "", delivery("900000000000003")},
		{"Your order has been delivered", "Order #900000000000003 is at your door.", delivery("900000000000003")},
	} {
		if got := read(t, evidence.Message{ID: "m1", Subject: c.subject, Text: c.text}); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: Read = %+v; want %+v", c.subject, got, c.want)
		}
	}
}

func cancellation(ref string) evidence.Evidence {
	return evidence.Evidence{Cancellations: []evidence.Cancellation{{Ref: ref, At: received}}}
}

func delivery(ref string) evidence.Evidence {
	return evidence.Evidence{Deliveries: []evidence.Delivery{{Ref: ref, At: received}}}
}

func TestAnUnreadableMessageSaysWhy(t *testing.T) {
	for _, c := range []struct {
		m   evidence.Message
		why string
	}{
		{evidence.Message{Subject: "We have received your order", Text: "Your Order placed on March 4, 2026 at 14:05 via Credit or Debit Card has been confirmed. Total (VAT included) ฿460.00"}, "no order number"},
		{evidence.Message{Subject: "We have received your order No 900000000000001", Text: "Your Order #900000000000001 has been confirmed. Total (VAT included) ฿460.00"}, "no order time"},
		{evidence.Message{Subject: "We have received your order No 900000000000001", Text: "Your Order #900000000000001 placed on March 4, 2026 at 14:05 via Credit or Debit Card has been confirmed."}, "no order total"},
		{evidence.Message{Subject: "Order Cancellation Notification", Text: "Your order was cancelled."}, "no order number"},
		{evidence.Message{From: "easybills@2c2p.com", Text: strings.Replace(fmt.Sprintf(billText, "การไฟฟ้าส่วนภูมิภาค"), "ยอดชำระ 1,812.50", "ยอดชำระ", 1)}, "no bill amount"},
	} {
		if _, err := (Reader{}).Read(c.m); err == nil || err.Error() != c.why {
			t.Errorf("Read(%q, %q) error = %v; want %s", c.m.Subject, c.m.Text, err, c.why)
		}
	}
}

func TestLazadaIsAMerchantOfOrdersAndBills(t *testing.T) {
	m := Merchant()
	if m.Name != "lazada" || m.Label != "auto-lazada" || !reflect.DeepEqual(m.Queries, Queries) || m.Tolerance != 0.01 || m.Lag != 1 ||
		!reflect.DeepEqual(m.Kinds, []evidence.Kind{evidence.Order, evidence.Electricity, evidence.Water, evidence.Bill}) || m.Reader == nil {
		t.Errorf("Merchant() = %+v", m)
	}
}
