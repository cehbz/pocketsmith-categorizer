package grab

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	// total is a receipt's total in baht after its label: Total Paid, รวมทั้งหมด (ruam thang
	// mot, "grand total"), ทั้งหมด (thang mot, "in all") or รวม (ruam, "total").
	total   = regexp.MustCompile(`(?:Total Paid|รวมทั้งหมด|ทั้งหมด|รวม)[\s|]*(?:฿|THB)\s*([\d,]+(?:\.\d+)?)`)
	booking = regexp.MustCompile(`\bA-[A-Z0-9]{8,}\b`)
)

// ReadReceipt reads the payment a Grab e-receipt's text records: the service it names first,
// the first total after a total label, and its booking. at is when the receipt arrived and
// account the Grab account it was sent to.
func ReadReceipt(text string, at time.Time, account string) (Payment, error) {
	s, ok := firstService(text)
	if !ok {
		return Payment{}, errors.New("no Grab service")
	}
	m := total.FindStringSubmatch(text)
	if m == nil {
		return Payment{}, errors.New("no total")
	}
	thb, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64)
	if err != nil {
		return Payment{}, fmt.Errorf("total %q: %w", m[1], err)
	}
	return Payment{Booking: booking.FindString(text), Service: s, THB: thb, At: at, Account: account}, nil
}

// firstService is the service label earliest in text; of two at the same place, the longer.
func firstService(text string) (Service, bool) {
	best, at := Service(""), -1
	for _, l := range Labels() { // longest first
		if i := strings.Index(text, l); i >= 0 && (at < 0 || i < at) {
			best, at = Service(l), i
		}
	}
	return best, at >= 0
}
