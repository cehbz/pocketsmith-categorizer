// Package lazada reads Lazada's mail: order confirmations, cancellations and deliveries, and
// the bills Lazada Pay pays through 2C2P's easyBills.
package lazada

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/evidence"
)

// Queries find Lazada's order confirmations in English and in Thai, its cancellations, its
// deliveries, its tax invoices and easyBills' bill receipts.
var Queries = []string{
	`from:lazada subject:"We have received your order"`,
	`from:lazada "ยอดรวมทั้งหมด"`, // yot ruam thang mot: "grand total", the Thai template's
	`from:lazada subject:(cancellation OR cancelled OR cancelling)`,
	`from:lazada subject:("successfully shipped" OR "has been delivered")`,
	// bai kam kap phasi elekthronik: "electronic tax invoice", spelled with the composed sara am
	// and with the decomposed one Lazada's mail uses (they render alike)
	`from:lazada subject:("ใบกำกับภาษีอิเล็กทรอนิกส์" OR "ใบกํากับภาษีอิเล็กทรอนิกส์")`,
	`from:easybills@2c2p.com`,
}

// Label marks the charges Lazada's receipts decided.
const Label = "auto-lazada"

// Fee is what Lazada Pay adds to a bill it pays, in THB.
const Fee = 5.35

// Merchant is Lazada as the receipt layer knows it, before the user's config gives its
// payees, its receipts' start and its categories. Its charges carry the purchase's date, a
// day's slack allowed, within 1% of the receipt's amount.
func Merchant() evidence.Merchant {
	return evidence.Merchant{Name: "lazada", Label: Label, Queries: Queries,
		Kinds:     []evidence.Kind{evidence.Order, evidence.Electricity, evidence.Water, evidence.Bill},
		Tolerance: 0.01, Lag: 1, Reader: Reader{}}
}

var bangkok = mustLoad("Asia/Bangkok")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

var (
	orderID = regexp.MustCompile(`\b\d{15,16}\b`)
	ordered = regexp.MustCompile(`#\s?(\d{15,16})\b`) // "Order #900000000000001"
	// placedEN is an English order's time: "placed on March 4, 2026 at 14:05".
	placedEN = regexp.MustCompile(`placed on ([A-Z][a-z]+ \d{1,2}, \d{4}) at (\d{1,2}:\d{2})`)
	// placedTH is a Thai order's time: a day, a Thai month and a Buddhist-era year, then the
	// time, as "4 มีนาคม 2569 14:05" (4 March 2026, 14:05), the time running into the next word.
	placedTH = regexp.MustCompile(`(\d{1,2})\s*(` + strings.Join(monthNames(), "|") + `)\s*(\d{4})\s+(\d{1,2}):(\d{2})`)
	// total is an order's total after its label: "Total (VAT included)", or the Thai
	// ยอดรวมทั้งหมด(รวม VAT) (yot ruam thang mot (ruam VAT): "grand total (incl. VAT)").
	total = regexp.MustCompile(`(?:Total \(VAT included\)|ยอดรวมทั้งหมด\s*\(รวม VAT\))[\s|:]*(?:฿|THB)?\s*([\d,]+(?:\.\d+)?)`)
	// billAmount is a bill's amount after ยอดชำระ (yot chamra, "amount paid").
	billAmount = regexp.MustCompile(`ยอดชำระ\s*([\d,]+\.\d{2})`)
	// billRef is a bill's reference after หมายเลขอ้างอิง (mai lek angking, "reference number").
	billRef   = regexp.MustCompile(`หมายเลขอ้างอิง\s*(EZBI[0-9A-Z]+)`)
	cancelled = regexp.MustCompile(`(?i)cancel`)
	delivered = regexp.MustCompile(`(?i)successfully shipped|has been delivered`)
)

// thaiMonths are the Thai months' names, full and abbreviated, by month.
var thaiMonths = map[string]time.Month{
	"มกราคม":     time.January,   // makarakhom
	"กุมภาพันธ์": time.February,  // kumphaphan
	"มีนาคม":     time.March,     // minakhom
	"เมษายน":     time.April,     // mesayon
	"พฤษภาคม":    time.May,       // phruetsaphakhom
	"มิถุนายน":   time.June,      // mithunayon
	"กรกฎาคม":    time.July,      // karakadakhom
	"สิงหาคม":    time.August,    // singhakhom
	"กันยายน":    time.September, // kanyayon
	"ตุลาคม":     time.October,   // tulakhom
	"พฤศจิกายน":  time.November,  // phruetsachikayon
	"ธันวาคม":    time.December,  // thanwakhom
	"ม.ค.":       time.January,   // ma.kh., abbreviated
	"ก.พ.":       time.February,  // kum.
	"มี.ค.":      time.March,     // mi.kh.
	"เม.ย.":      time.April,     // me.sa.
	"พ.ค.":       time.May,       // phr.kh.
	"มิ.ย.":      time.June,      // mi.thu.
	"ก.ค.":       time.July,      // ka.kha.
	"ส.ค.":       time.August,    // si.kh.
	"ก.ย.":       time.September, // kan.
	"ต.ค.":       time.October,   // tu.kh.
	"พ.ย.":       time.November,  // phr.ya.
	"ธ.ค.":       time.December,  // than.
}

// monthNames are the Thai months' names as regular expressions, longest first.
func monthNames() []string {
	var out []string
	for n := range thaiMonths {
		out = append(out, regexp.QuoteMeta(n))
	}
	slices.SortFunc(out, func(a, b string) int { return len(b) - len(a) })
	return out
}

// billers are the utilities whose names say which kind of bill they send, in the order tried.
var billers = []struct {
	name string
	kind evidence.Kind
}{
	{"การไฟฟ้าส่วนภูมิภาค", evidence.Electricity}, // kan fai fa suan phumiphak: Provincial Electricity Authority
	{"การประปาส่วนภูมิภาค", evidence.Water},       // kan prapa suan phumiphak: Provincial Waterworks Authority
}

// taxInvoice is ใบกำกับภาษีอิเล็กทรอนิกส์ (bai kam kap phasi elekthronik, "electronic tax
// invoice") with the sara am of กำ (kam) composed, U+0E33. Lazada's mail spells it decomposed,
// U+0E4D nikhahit then U+0E32 sara aa; the two render alike, so Read folds the decomposed
// spelling into the composed one.
const taxInvoice = "ใบกำกับภาษีอิเล็กทรอนิกส์"

// composeSaraAm folds the decomposed sara am (U+0E4D U+0E32) into the composed one (U+0E33).
func composeSaraAm(s string) string { return strings.ReplaceAll(s, "\u0e4d\u0e32", "\u0e33") }

// Reader reads Lazada's mail.
type Reader struct{}

// Read reads a message by what it is: an easyBills receipt for a bill Lazada paid, a tax
// invoice naming no order (a bill without an amount), a cancellation, a delivery, else an
// order confirmation. An easyBills receipt Lazada didn't pay and a tax invoice for an order
// say nothing.
func (Reader) Read(m evidence.Message) (evidence.Evidence, error) {
	m.Subject, m.Text = composeSaraAm(m.Subject), composeSaraAm(m.Text)
	switch {
	case strings.EqualFold(m.From, "easybills@2c2p.com"):
		return bill(m)
	case strings.Contains(m.Subject, taxInvoice):
		if orderID.MatchString(m.Subject + " " + m.Text) {
			return evidence.Evidence{}, nil
		}
		invoice := evidence.Receipt{At: m.Received, Parts: []evidence.Part{{Ref: "tax invoice", Kind: evidence.Bill}}, Unpriced: true}
		return evidence.Evidence{Receipts: []evidence.Receipt{invoice}}, nil
	case cancelled.MatchString(m.Subject):
		id, err := ref(m)
		if err != nil {
			return evidence.Evidence{}, err
		}
		return evidence.Evidence{Cancellations: []evidence.Cancellation{{Ref: id, At: m.Received}}}, nil
	case delivered.MatchString(m.Subject):
		id, err := ref(m)
		if err != nil {
			return evidence.Evidence{}, err
		}
		return evidence.Evidence{Deliveries: []evidence.Delivery{{Ref: id, At: m.Received}}}, nil
	}
	return order(m)
}

// ref is the order a message names: in its subject, else after a # in its text, else
// anywhere in its text.
func ref(m evidence.Message) (string, error) {
	if id := orderID.FindString(m.Subject); id != "" {
		return id, nil
	}
	if id := ordered.FindStringSubmatch(m.Text); id != nil {
		return id[1], nil
	}
	if id := orderID.FindString(m.Text); id != "" {
		return id, nil
	}
	return "", errors.New("no order number")
}

// order reads an order confirmation: the order, its total and when it was placed, Bangkok
// time. The orders placed in one minute are one checkout.
func order(m evidence.Message) (evidence.Evidence, error) {
	id, err := ref(m)
	if err != nil {
		return evidence.Evidence{}, err
	}
	at, err := placed(m.Text)
	if err != nil {
		return evidence.Evidence{}, err
	}
	t := total.FindStringSubmatch(m.Text)
	if t == nil {
		return evidence.Evidence{}, errors.New("no order total")
	}
	thb, err := amount(t[1])
	if err != nil {
		return evidence.Evidence{}, err
	}
	r := evidence.Receipt{At: at, Parts: []evidence.Part{{Ref: id, Kind: evidence.Order, THB: thb}}, Checkout: at.UTC().Format(time.RFC3339)}
	return evidence.Evidence{Receipts: []evidence.Receipt{r}}, nil
}

// placed is when an order was placed, from the English template or the Thai one, whose year
// is Buddhist-era (2567 is 2024).
func placed(text string) (time.Time, error) {
	if p := placedEN.FindStringSubmatch(text); p != nil {
		for _, layout := range []string{"January 2, 2006 15:04", "Jan 2, 2006 15:04"} {
			if at, err := time.ParseInLocation(layout, p[1]+" "+p[2], bangkok); err == nil {
				return at, nil
			}
		}
		return time.Time{}, fmt.Errorf("order time %q", p[1]+" "+p[2])
	}
	if p := placedTH.FindStringSubmatch(text); p != nil {
		d, _ := strconv.Atoi(p[1])
		y, _ := strconv.Atoi(p[3])
		h, _ := strconv.Atoi(p[4])
		mi, _ := strconv.Atoi(p[5])
		if y > 2400 {
			y -= 543
		}
		return time.Date(y, thaiMonths[p[2]], d, h, mi, 0, 0, bangkok), nil
	}
	return time.Time{}, errors.New("no order time")
}

// bill reads an easyBills receipt for a bill Lazada Pay paid: the biller's kind, the amount
// and the reference, with Lazada's fee on top. The receipt is mailed as the bill is paid, so
// it is paid when it arrived; the slip's own time is left unread.
func bill(m evidence.Message) (evidence.Evidence, error) {
	if !strings.Contains(m.Text, "Lazada_utilPayment") {
		return evidence.Evidence{}, nil
	}
	kind := evidence.Bill
	for _, b := range billers {
		if strings.Contains(m.Text, b.name) {
			kind = b.kind
			break
		}
	}
	a := billAmount.FindStringSubmatch(m.Text)
	if a == nil {
		return evidence.Evidence{}, errors.New("no bill amount")
	}
	thb, err := amount(a[1])
	if err != nil {
		return evidence.Evidence{}, err
	}
	id := billRef.FindStringSubmatch(m.Text)
	if id == nil {
		return evidence.Evidence{}, errors.New("no bill reference")
	}
	r := evidence.Receipt{At: m.Received, Parts: []evidence.Part{{Ref: id[1], Kind: kind, THB: thb}}, Fee: Fee}
	return evidence.Evidence{Receipts: []evidence.Receipt{r}}, nil
}

// amount is a THB amount with thousands separators.
func amount(s string) (float64, error) {
	v, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", ""), 64)
	if err != nil {
		return 0, fmt.Errorf("amount %q: %w", s, err)
	}
	return v, nil
}
