package trips

import (
	"cmp"
	"math"
	"slices"
	"strings"
	"unicode"

	"github.com/cehbz/pocketsmith-categorizer/learn"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

// Transaction is a ledger transaction with its card evidence and the state the run's other
// layers leave it in.
type Transaction struct {
	ledger.Transaction
	Evidence Evidence     // zero when no record matched
	Home     ledger.State // its state once the other layers have decided
}

// Charge reports whether t is spending: a debit that isn't a transfer.
func (t Transaction) Charge() bool { return t.Amount < 0 && !t.Transfer }

// Credit reports whether t is money back: a credit that isn't a transfer.
func (t Transaction) Credit() bool { return t.Amount > 0 && !t.Transfer }

// Merchant is who t paid or was paid by: the record's merchant name, else the key of the
// payee's words with fewer than three digits, else the payee's key, else the payee. Skipping
// the words with three or more keeps a terminal id or ticket number (`xx20000Q9C7777 ALPHA
// PLAZA`) out of the key and names like 7-ELEVEN and 12GO in it.
func (t Transaction) Merchant() string {
	if t.Evidence.Merchant != "" {
		return strings.ToUpper(t.Evidence.Merchant)
	}
	words := slices.DeleteFunc(strings.Fields(t.Payee), func(w string) bool { return countDigits(w) >= 3 })
	if k := learn.PayeeKey(strings.Join(words, " ")); k != "" {
		return k
	}
	if k := learn.PayeeKey(t.Payee); k != "" {
		return k
	}
	return t.Payee
}

// countDigits is the number of digits in w.
func countDigits(w string) int {
	n := 0
	for _, r := range w {
		if unicode.IsDigit(r) {
			n++
		}
	}
	return n
}

// closeShare is how far apart two amounts may be, as a share of the earlier one, and still be
// close: a refund's and its charge's, a subscription's and its last month's.
const closeShare = 0.03

// near reports whether amount a is within closeShare of b.
func near(a, b float64) bool { return math.Abs(math.Abs(a)-math.Abs(b)) <= closeShare*math.Abs(b) }

func byDate(a, b Transaction) int {
	if c := a.Date.Compare(b.Date); c != 0 {
		return c
	}
	return cmp.Compare(a.ID, b.ID)
}
