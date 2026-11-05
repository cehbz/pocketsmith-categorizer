// Package evidence models what merchants' mail says the user bought, and pairs it with the
// card's charges and credits: a receipt for each payment, the parts it pays for, and the
// parts cancelled or delivered.
package evidence

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

// Kind is what a part is.
type Kind string

const (
	Ride        Kind = "ride"
	Food        Kind = "food"
	Mart        Kind = "mart"
	Express     Kind = "express"
	Order       Kind = "order"
	Electricity Kind = "electricity"
	Water       Kind = "water"
	Bill        Kind = "bill" // a bill whose utility the evidence doesn't name
)

// Part is one thing a receipt pays for: its reference (a Grab booking, a Lazada order, a
// bill's reference), its kind and its THB amount with vouchers and discounts netted, zero when
// the receipt gives none.
type Part struct {
	Ref  string  `json:"ref"`
	Kind Kind    `json:"kind"`
	THB  float64 `json:"thb"`
}

// Receipt is what a merchant says the user paid at one time. A card charge pays one receipt.
type Receipt struct {
	Merchant string    `json:"-"` // set from the read that holds it
	At       time.Time `json:"at"`
	Parts    []Part    `json:"parts"`
	Fee      float64   `json:"fee,omitempty"`      // the merchant's fee on top of the parts, THB
	Checkout string    `json:"checkout,omitempty"` // a merchant's receipts sharing one are one checkout
	Unpriced bool      `json:"unpriced,omitempty"` // the merchant gave no amount, as a Lazada tax invoice doesn't
}

// THB is what the receipt's charge pays: its parts and its fee.
func (r Receipt) THB() float64 {
	sum := r.Fee
	for _, p := range r.Parts {
		sum += p.THB
	}
	return sum
}

// Amountless reports whether the merchant gave no amount for the receipt, as opposed to an
// amount of zero.
func (r Receipt) Amountless() bool { return r.Unpriced }

// Ref names the receipt by its parts' references, joined by "+".
func (r Receipt) Ref() string {
	refs := make([]string, len(r.Parts))
	for i, p := range r.Parts {
		refs[i] = p.Ref
	}
	return strings.Join(refs, "+")
}

// Kind is the kind of the receipt's first part; a checkout's parts share one.
func (r Receipt) Kind() Kind {
	if len(r.Parts) == 0 {
		return ""
	}
	return r.Parts[0].Kind
}

// Cancellation is a part the merchant won't deliver, and when it said so.
type Cancellation struct {
	Merchant string    `json:"-"`
	Ref      string    `json:"ref"`
	At       time.Time `json:"at"`
}

// Delivery is a part the merchant delivered, and when it said so.
type Delivery struct {
	Merchant string    `json:"-"`
	Ref      string    `json:"ref"`
	At       time.Time `json:"at"`
}

// Evidence is what messages say: receipts, cancellations and deliveries.
type Evidence struct {
	Receipts      []Receipt      `json:"receipts,omitempty"`
	Cancellations []Cancellation `json:"cancellations,omitempty"`
	Deliveries    []Delivery     `json:"deliveries,omitempty"`
}

// Message is a mail message as a reader sees it.
type Message struct {
	ID       string
	Received time.Time
	From     string // the sender's bare address
	Subject  string
	Text     string
}

// Reader reads a merchant's evidence out of a message: what it says, nothing when it says
// nothing of the user's purchases, or an error saying why it can't be read.
type Reader interface {
	Read(m Message) (Evidence, error)
}

// Merchant is a merchant whose mail explains its card charges and credits.
type Merchant struct {
	Name      string   // grab, lazada: its config table, and what its decisions' sources start with
	Label     string   // the label its decisions add
	Queries   []string // the Gmail searches finding its mail
	Kinds     []Kind   // the kinds of parts it sells
	Tolerance float64  // largest relative difference between a charge and its receipt
	Lag       int      // most days a charge's ledger date follows its receipt's day
	Reader    Reader

	// From the user's config:
	Payees     []string                   // the payees its card charges and credits carry
	Start      time.Time                  // the first day its receipts reach the user's mail
	Categories map[Kind]ledger.CategoryID // the category of each kind of part
	StayHome   bool                       // its charges are never trip spending
}

// Title is a merchant's name as reports show it: lazada is Lazada.
func Title(name string) string {
	if name == "" {
		return ""
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

// Checkouts is receipts with each merchant's receipts sharing a checkout joined into one: the
// earliest one's time, every part in reference order, the fees summed. Receipts without a
// checkout stay as they are. The result is in time order.
func Checkouts(receipts []Receipt) []Receipt {
	var out []Receipt
	at := map[[2]string]int{}
	for _, r := range receipts {
		r.Parts = slices.Clone(r.Parts)
		if r.Checkout == "" {
			out = append(out, r)
			continue
		}
		k := [2]string{r.Merchant, r.Checkout}
		i, ok := at[k]
		if !ok {
			at[k] = len(out)
			out = append(out, r)
			continue
		}
		c := &out[i]
		c.Parts = append(c.Parts, r.Parts...)
		slices.SortFunc(c.Parts, func(a, b Part) int { return cmp.Compare(a.Ref, b.Ref) })
		c.Fee += r.Fee
		c.Unpriced = c.Unpriced && r.Unpriced
		if r.At.Before(c.At) {
			c.At = r.At
		}
	}
	slices.SortStableFunc(out, func(a, b Receipt) int { return a.At.Compare(b.At) })
	return out
}

// Bills reports whether every part of the receipt is a bill of some utility.
func (r Receipt) Bills() bool {
	return len(r.Parts) > 0 && !slices.ContainsFunc(r.Parts, func(p Part) bool {
		return p.Kind != Electricity && p.Kind != Water && p.Kind != Bill
	})
}

// NoReceipt is the label of a charge of the merchant named name that no receipt explains.
func NoReceipt(name string) string { return "attention-no-" + name + "-receipt" }

// NoPurchase is the label of a credit of the merchant named name with no purchase to refund.
func NoPurchase(name string) string { return "attention-no-" + name + "-purchase" }

// Phrase is a NoReceipt or NoPurchase label in plain words; ok is false for any other label.
func Phrase(label string) (phrase string, ok bool) {
	rest, ok := strings.CutPrefix(label, "attention-no-")
	if !ok {
		return "", false
	}
	if name, ok := strings.CutSuffix(rest, "-receipt"); ok {
		return "no " + Title(name) + " receipt", true
	}
	if name, ok := strings.CutSuffix(rest, "-purchase"); ok {
		return "no " + Title(name) + " purchase to refund", true
	}
	return "", false
}
