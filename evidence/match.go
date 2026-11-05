package evidence

import (
	"math"
	"slices"
	"time"

	"github.com/cehbz/munkres"

	"github.com/cehbz/pocketsmith-categorizer/internal/day"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

// Rates gives the THB per USD reference rate on a date.
type Rates func(date time.Time) (float64, error)

// Markup is how much more USD the card charges than the reference rate gives.
const Markup = 0.0029

const (
	refundAge  = 60     // most days a credit follows the purchase of the part it refunds
	refundPost = 14     // most days after its date a credit's amount can have been converted
	lagCost    = 0.001  // per day a charge follows its receipt
	ageCost    = 0.0001 // per day a credit follows the part it refunds
	statusCost = 0.1    // per step from cancelled to undelivered to delivered; outweighs any amount's difference
	impossible = 1e3    // finite stand-in for a forbidden pair; munkres rejects infinities
)

// A part's status, in the order a credit refunds it.
const (
	cancelled = iota
	undelivered
	delivered
)

// Paid is a charge and the receipt it paid.
type Paid struct {
	Charge  ledger.Transaction
	Receipt Receipt
}

// Refund is a credit and the part it refunds.
type Refund struct {
	Credit ledger.Transaction
	Part   Part
}

// Matching is a merchant's evidence paired with its charges and credits.
type Matching struct {
	Paid        []Paid
	Refunds     []Refund
	Unmatched   []Receipt            // receipts no charge paid
	Unexplained []ledger.Transaction // charges no receipt explains
	Unrefunded  []ledger.Transaction // credits with no part to refund
}

// Match pairs m's charges and credits among txs, those carrying its payees dated from its
// start, with its evidence in ev, its checkouts joined; loc is the ledger's time zone. Charges
// are matched together at the least total cost, and so are credits. A receipt with an amount
// pays a charge whose USD is within m's tolerance of the receipt's THB at the reference rate
// of a trading day: the receipt's, the one before it or the one after it, with the card's
// markup, dated from the receipt's day to m's lag later. A receipt without an amount then
// explains a charge within a day of it that no other receipt explains. A credit refunds a part
// with an amount bought up to 60 days before it, cancelled ahead of undelivered ahead of
// delivered, whose THB is within m's tolerance of the credit's USD at a rate from the credit's
// date to 14 days after it.
func (m Merchant) Match(ev Evidence, txs []ledger.Transaction, rates Rates, loc *time.Location) (Matching, error) {
	var charges, credits []ledger.Transaction
	for _, t := range txs {
		if t.Transfer || t.Date.Before(m.Start) || !slices.Contains(m.Payees, t.Payee) {
			continue
		}
		switch {
		case t.Amount < 0:
			charges = append(charges, t)
		case t.Amount > 0:
			credits = append(credits, t)
		}
	}
	var receipts []Receipt
	for _, r := range ev.Receipts {
		if r.Merchant == m.Name {
			receipts = append(receipts, r)
		}
	}
	receipts = Checkouts(receipts)
	var out Matching
	if err := m.charges(receipts, charges, rates, loc, &out); err != nil {
		return Matching{}, err
	}
	if err := m.credits(ev, receipts, credits, rates, loc, &out); err != nil {
		return Matching{}, err
	}
	return out, nil
}

// charges pairs receipts with charges into out: those with an amount first, then those
// without one with the charges left. An amountless receipt within a day of a priced bill
// is that bill's duplicate and is dropped.
func (m Merchant) charges(receipts []Receipt, charges []ledger.Transaction, rates Rates, loc *time.Location, out *Matching) error {
	var priced, amountless []Receipt
	for _, r := range receipts {
		if r.Amountless() {
			amountless = append(amountless, r)
		} else {
			priced = append(priced, r)
		}
	}
	// Lazada sends a bill's tax invoice a minute after the bill's own receipt: the duplicate
	// explains nothing.
	amountless = slices.DeleteFunc(amountless, func(r Receipt) bool {
		return slices.ContainsFunc(priced, func(p Receipt) bool {
			return p.Bills() && math.Abs(p.At.Sub(r.At).Hours()) <= 24
		})
	})
	cost := make([][]float64, len(priced))
	for i, r := range priced {
		d := day.Of(r.At, loc)
		window, err := tradingRates(rates, d)
		if err != nil {
			return err
		}
		var want []float64
		for _, rate := range window {
			want = append(want, r.THB()/rate*(1+Markup))
		}
		cost[i] = make([]float64, len(charges))
		for j, c := range charges {
			lag := day.Between(d, c.Date)
			e := closest(-c.Amount, want)
			if lag < 0 || lag > m.Lag || e > m.Tolerance {
				cost[i][j] = impossible
				continue
			}
			cost[i][j] = e + lagCost*float64(lag)
		}
	}
	paid, err := assign(cost)
	if err != nil {
		return err
	}
	claimed := make([]bool, len(charges))
	for i, j := range paid {
		if j < 0 {
			out.Unmatched = append(out.Unmatched, priced[i])
			continue
		}
		claimed[j] = true
		out.Paid = append(out.Paid, Paid{Charge: charges[j], Receipt: priced[i]})
	}
	var left []int
	for j := range charges {
		if !claimed[j] {
			left = append(left, j)
		}
	}
	cost = make([][]float64, len(amountless))
	for i, r := range amountless {
		d := day.Of(r.At, loc)
		cost[i] = make([]float64, len(left))
		for k, j := range left {
			gap := day.Between(d, charges[j].Date)
			if gap < -1 || gap > 1 {
				cost[i][k] = impossible
				continue
			}
			cost[i][k] = lagCost * math.Abs(float64(gap))
		}
	}
	paid, err = assign(cost)
	if err != nil {
		return err
	}
	for i, k := range paid {
		if k < 0 {
			out.Unmatched = append(out.Unmatched, amountless[i])
			continue
		}
		claimed[left[k]] = true
		out.Paid = append(out.Paid, Paid{Charge: charges[left[k]], Receipt: amountless[i]})
	}
	for j, c := range charges {
		if !claimed[j] {
			out.Unexplained = append(out.Unexplained, c)
		}
	}
	return nil
}

// credits pairs credits with the parts of receipts they refund into out.
func (m Merchant) credits(ev Evidence, receipts []Receipt, credits []ledger.Transaction, rates Rates, loc *time.Location, out *Matching) error {
	status := map[string]int{}
	for _, d := range ev.Deliveries {
		if d.Merchant == m.Name {
			status[d.Ref] = delivered
		}
	}
	for _, c := range ev.Cancellations {
		if c.Merchant == m.Name {
			status[c.Ref] = cancelled
		}
	}
	type bought struct {
		part Part
		day  time.Time
	}
	var parts []bought
	for _, r := range receipts {
		for _, p := range r.Parts {
			if p.THB > 0 {
				parts = append(parts, bought{p, day.Of(r.At, loc)})
			}
		}
	}
	cost := make([][]float64, len(credits))
	for i, c := range credits {
		cost[i] = make([]float64, len(parts))
		if len(parts) == 0 {
			continue
		}
		var rs []float64
		for k := 0; k <= refundPost; k++ {
			rate, err := rates(c.Date.AddDate(0, 0, k))
			if err != nil {
				return err
			}
			rs = append(rs, rate)
		}
		for j, b := range parts {
			want := make([]float64, len(rs))
			for k, rate := range rs {
				want[k] = b.part.THB / rate * (1 + Markup)
			}
			age := day.Between(b.day, c.Date)
			e := closest(c.Amount, want)
			if age < 0 || age > refundAge || e > m.Tolerance {
				cost[i][j] = impossible
				continue
			}
			st, ok := status[b.part.Ref]
			if !ok {
				st = undelivered
			}
			cost[i][j] = statusCost*float64(st) + e + ageCost*float64(age)
		}
	}
	refunds, err := assign(cost)
	if err != nil {
		return err
	}
	for i, j := range refunds {
		if j < 0 {
			out.Unrefunded = append(out.Unrefunded, credits[i])
			continue
		}
		out.Refunds = append(out.Refunds, Refund{Credit: credits[i], Part: parts[j].part})
	}
	return nil
}

// closest is the smallest relative difference between usd and any of want.
func closest(usd float64, want []float64) float64 {
	best := math.Inf(1)
	for _, w := range want {
		best = min(best, math.Abs(usd-w)/usd)
	}
	return best
}

// assign is the least-cost assignment of rows to columns: each row's column, or -1 for a row
// given none or only a forbidden one.
func assign(cost [][]float64) ([]int, error) {
	out := make([]int, len(cost))
	if len(cost) == 0 || len(cost[0]) == 0 {
		for i := range out {
			out[i] = -1
		}
		return out, nil
	}
	h, err := munkres.NewHungarianAlgorithm(cost)
	if err != nil {
		return nil, err
	}
	for i, j := range h.Execute() {
		out[i] = j
		if j >= 0 && cost[i][j] >= impossible {
			out[i] = -1
		}
	}
	return out, nil
}

// tradingRates gives the reference rates of the trading day before d, of d and of the trading
// day after it. rates answers a non-trading day with the rate of the trading day before it, so
// the day before d and d itself give the previous and the same trading day's rates, and the
// next trading day is the first of the following four days whose rate differs from d's.
func tradingRates(rates Rates, d time.Time) ([]float64, error) {
	before, err := rates(d.AddDate(0, 0, -1))
	if err != nil {
		return nil, err
	}
	same, err := rates(d)
	if err != nil {
		return nil, err
	}
	out := []float64{before, same, same}
	for i := 1; i <= 4; i++ {
		next, err := rates(d.AddDate(0, 0, i))
		if err != nil {
			return nil, err
		}
		if next != same {
			out[2] = next
			break
		}
	}
	return out, nil
}
