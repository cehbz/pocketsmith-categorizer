// Package history reads Grab history statements (the cleanup's payment source).
package history

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/grab"
)

var (
	headerRE  = regexp.MustCompile(`Total Trips:\s*(\d+)\s+Amount:\s*THB\s*([\d,]+\.\d{2})`)
	tripRE    = regexp.MustCompile(`^\s{0,3}(\d{2} [A-Z][a-z]{2} \d{4}),`)
	clockRE   = regexp.MustCompile(`\d{2}:\d{2}[AP]M`)
	amountRE  = regexp.MustCompile(`THB\s+([\d,]+\.\d{2})\s*$`)
	bookingRE = regexp.MustCompile(`\b(A-[A-Z0-9]{8,})`)
	wrappedRE = regexp.MustCompile(`\b([A-Z0-9]{12,}AV)\b`) // booking continuation after a bare "A-"
)

// Text runs pdftotext in layout mode on a statement PDF.
func Text(ctx context.Context, pdftotext, pdf string) (string, error) {
	out, err := exec.CommandContext(ctx, pdftotext, "-layout", pdf, "-").Output()
	if err != nil {
		return "", fmt.Errorf("pdftotext %s: %w", pdf, err)
	}
	return string(out), nil
}

// Parse reads a statement into payments for account, checking it against the statement header.
// A trip with a zero total puts no charge on the card and is not a payment.
func Parse(text, account string, bkk *time.Location) ([]grab.Payment, error) {
	h := headerRE.FindStringSubmatch(text)
	if h == nil {
		return nil, errors.New("no statement header (Total Trips, Amount)")
	}
	wantTrips, err := strconv.Atoi(h[1])
	if err != nil {
		return nil, err
	}
	wantTHB, err := parseTHB(h[2])
	if err != nil {
		return nil, err
	}
	var payments []grab.Payment
	trips, total := 0, 0.0
	for _, b := range blocks(text) {
		p, err := trip(b, account, bkk)
		if err != nil {
			return nil, err
		}
		trips++
		total += p.THB
		if p.THB > 0 {
			payments = append(payments, p)
		}
	}
	if trips != wantTrips || math.Abs(total-wantTHB) > 0.005 {
		return nil, fmt.Errorf("statement header says %d trips, THB %.2f; read %d trips, THB %.2f", wantTrips, wantTHB, trips, total)
	}
	return payments, nil
}

// Merge combines statements, keeping the first payment for each booking.
func Merge(statements ...[]grab.Payment) []grab.Payment {
	seen := map[string]bool{}
	var out []grab.Payment
	for _, s := range statements {
		for _, p := range s {
			if seen[p.Booking] {
				continue
			}
			seen[p.Booking] = true
			out = append(out, p)
		}
	}
	return out
}

// blocks splits statement text into one block of lines per trip.
func blocks(text string) [][]string {
	var out [][]string
	var cur []string
	for _, ln := range strings.Split(text, "\n") {
		switch {
		case tripRE.MatchString(ln):
			if cur != nil {
				out = append(out, cur)
			}
			cur = []string{ln}
		case cur == nil:
		case strings.Contains(ln, "Date/Time") || strings.Contains(ln, "Member Statement"):
			out = append(out, cur)
			cur = nil
		default:
			cur = append(cur, ln)
		}
	}
	if cur != nil {
		out = append(out, cur)
	}
	return out
}

func trip(lines []string, account string, bkk *time.Location) (grab.Payment, error) {
	text := strings.Join(lines, "\n")
	date := tripRE.FindStringSubmatch(lines[0])[1]
	clock := clockRE.FindString(text)
	if clock == "" {
		return grab.Payment{}, fmt.Errorf("trip on %s has no time", date)
	}
	at, err := time.ParseInLocation("02 Jan 2006 03:04PM", date+" "+clock, bkk)
	if err != nil {
		return grab.Payment{}, fmt.Errorf("trip on %s: %w", date, err)
	}
	var amountLine string
	var thb float64
	for _, ln := range lines {
		if m := amountRE.FindStringSubmatch(ln); m != nil {
			if thb, err = parseTHB(m[1]); err != nil {
				return grab.Payment{}, err
			}
			amountLine = ln
			break
		}
	}
	if amountLine == "" {
		return grab.Payment{}, fmt.Errorf("trip on %s %s has no THB amount", date, clock)
	}
	var booking string
	if m := bookingRE.FindStringSubmatch(text); m != nil {
		booking = m[1]
	} else if m := wrappedRE.FindStringSubmatch(text); m != nil {
		booking = "A-" + m[1]
	} else {
		return grab.Payment{}, fmt.Errorf("trip on %s %s has no booking", date, clock)
	}
	svc, err := service(amountLine, text)
	if err != nil {
		return grab.Payment{}, fmt.Errorf("trip %s: %w", booking, err)
	}
	return grab.Payment{Booking: booking, Service: svc, THB: thb, At: at, Account: account}, nil
}

// service finds the trip's service label: at the end of the amount line before "THB", or,
// when the statement wraps it onto other lines, as a whitespace-delimited label anywhere in the trip.
func service(amountLine, text string) (grab.Service, error) {
	prefix := strings.TrimRight(amountLine[:strings.LastIndex(amountLine, "THB")], " ")
	for _, l := range grab.Labels() {
		if strings.HasSuffix(prefix, " "+l) {
			return grab.ParseService(l)
		}
	}
	for _, l := range grab.Labels() {
		if regexp.MustCompile(`(?:^|\s)` + regexp.QuoteMeta(l) + `(?:\s|$)`).MatchString(text) {
			return grab.ParseService(l)
		}
	}
	return "", fmt.Errorf("no Grab service on %q", strings.TrimSpace(amountLine))
}

func parseTHB(s string) (float64, error) {
	return strconv.ParseFloat(strings.ReplaceAll(s, ",", ""), 64)
}
