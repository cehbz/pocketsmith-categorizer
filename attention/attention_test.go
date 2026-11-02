package attention

import (
	"math"
	"slices"
	"testing"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/internal/day"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

var size = Size{Absolute: 500, SD: 1}

func date(s string) time.Time {
	d, err := day.Parse(s)
	if err != nil {
		panic(err)
	}
	return d
}

func tx(id int64, d string, amount float64, payee string, cat ledger.CategoryID) ledger.Transaction {
	return ledger.Transaction{ID: id, Date: date(d), Amount: amount, Payee: payee, State: ledger.State{Category: cat}}
}

// reasons judges the last of txs against all of them.
func reasons(txs ...ledger.Transaction) []string {
	t := txs[len(txs)-1]
	return NewHistory(txs).Reasons(Case{Transaction: t, Category: t.State.Category}, size, 0)
}

func has(rs []string, r string) bool { return slices.Contains(rs, r) }

func TestAPayeeWithNoHistoryIsNew(t *testing.T) {
	if !has(reasons(tx(9, "2026-06-01", -5, "CAFE ONE 12", 0)), NewPayee) {
		t.Error("no new payee")
	}
}

func TestDormancyIs365Days(t *testing.T) {
	if has(reasons(tx(1, "2025-06-01", -5, "CAFE ONE 34", 0), tx(9, "2026-06-01", -5, "CAFE ONE 12", 0)), NewPayee) {
		t.Error("a payee seen 365 days earlier is new")
	}
	if !has(reasons(tx(1, "2025-05-31", -5, "CAFE ONE 34", 0), tx(9, "2026-06-01", -5, "CAFE ONE 12", 0)), NewPayee) {
		t.Error("a payee last seen 366 days earlier isn't new")
	}
}

func TestARefundFromAKnownPayeeIsNotNew(t *testing.T) {
	if has(reasons(tx(1, "2026-05-01", -50, "SHOP", 0), tx(9, "2026-06-01", 50, "SHOP", 0)), NewPayee) {
		t.Error("a refund from a known payee is new")
	}
}

func TestSameDayHistoryCountsByID(t *testing.T) {
	if has(reasons(tx(1, "2026-06-01", -5, "SHOP", 0), tx(9, "2026-06-01", -5, "SHOP", 0)), NewPayee) {
		t.Error("an earlier same-day charge didn't count")
	}
	if !has(reasons(tx(10, "2026-06-01", -5, "SHOP", 0), tx(9, "2026-06-01", -5, "SHOP", 0)), NewPayee) {
		t.Error("a later same-day charge counted")
	}
}

func TestPayeeWithoutLettersKeysOnThePayee(t *testing.T) {
	if has(reasons(tx(1, "2026-05-01", -5, "12345", 0), tx(9, "2026-06-01", -5, "12345", 0)), NewPayee) {
		t.Error("a repeated number payee is new")
	}
	if !has(reasons(tx(1, "2026-05-01", -5, "12345", 0), tx(9, "2026-06-01", -5, "67890", 0)), NewPayee) {
		t.Error("two number payees share a key")
	}
}

func TestLargeAtTheAbsoluteLimit(t *testing.T) {
	if has(reasons(tx(9, "2026-06-01", -499.99, "SHOP", 0)), Large) {
		t.Error("499.99 is large")
	}
	if !has(reasons(tx(9, "2026-06-01", -500, "SHOP", 0)), Large) {
		t.Error("500 isn't large")
	}
	if !has(reasons(tx(9, "2026-06-01", 500, "SHOP", 0)), Large) {
		t.Error("a 500 credit isn't large")
	}
}

// spread is a payee history whose log amounts are 1, 1, 2, 3, 3: mean 2, sample SD 1, so
// more than 1 SD above is above e^3 = 20.0855.
func spread(payee string, cat ledger.CategoryID, sign float64) []ledger.Transaction {
	var out []ledger.Transaction
	for i, l := range []float64{1, 1, 2, 3, 3} {
		out = append(out, tx(int64(i+1), "2026-01-0"+string(rune('1'+i)), sign*math.Exp(l), payee, cat))
	}
	return out
}

func TestLargeAboveOneSD(t *testing.T) {
	if has(reasons(append(spread("SHOP", 0, -1), tx(9, "2026-03-01", -20.08, "SHOP", 0))...), Large) {
		t.Error("20.08 is large")
	}
	if !has(reasons(append(spread("SHOP", 0, -1), tx(9, "2026-03-01", -20.09, "SHOP", 0))...), Large) {
		t.Error("20.09 isn't large")
	}
}

func TestSizeComparesTheSameDirection(t *testing.T) {
	if has(reasons(append(spread("SHOP", 0, 1), tx(9, "2026-03-01", -20.09, "SHOP", 0))...), Large) {
		t.Error("credits set the size of a debit")
	}
}

func TestFewPayeeChargesFallBackToTheCategory(t *testing.T) {
	txs := spread("OTHER", 7, -1)
	for i := range 4 {
		txs = append(txs, tx(int64(20+i), "2026-02-0"+string(rune('1'+i)), -100, "NEW SHOP", 8))
	}
	if !has(reasons(append(txs, tx(9, "2026-03-01", -20.09, "NEW SHOP", 7))...), Large) {
		t.Error("4 payee charges didn't fall back to the category")
	}
	txs = append(txs, tx(30, "2026-02-09", -100, "NEW SHOP", 8))
	if has(reasons(append(txs, tx(9, "2026-03-01", -20.09, "NEW SHOP", 7))...), Large) {
		t.Error("5 payee charges fell back to the category")
	}
}

func TestZeroAmountIsNeverLarge(t *testing.T) {
	if has(reasons(append(spread("SHOP", 0, -1), tx(9, "2026-03-01", 0, "SHOP", 0))...), Large) {
		t.Error("a zero amount is large")
	}
}

func TestDuplicateWithinThreeDaysAbove20(t *testing.T) {
	if !has(reasons(tx(1, "2026-06-01", -20.01, "SHOP", 0), tx(9, "2026-06-04", -20.01, "SHOP", 0)), Duplicate) {
		t.Error("3 days apart isn't a duplicate")
	}
	if has(reasons(tx(1, "2026-05-31", -20.01, "SHOP", 0), tx(9, "2026-06-04", -20.01, "SHOP", 0)), Duplicate) {
		t.Error("4 days apart is a duplicate")
	}
	if has(reasons(tx(1, "2026-06-01", -20, "SHOP", 0), tx(9, "2026-06-01", -20, "SHOP", 0)), Duplicate) {
		t.Error("20.00 is a duplicate")
	}
	if has(reasons(tx(1, "2026-06-01", -20.01, "SHOP ONE", 0), tx(9, "2026-06-01", -20.01, "STORE TWO", 0)), Duplicate) {
		t.Error("different payees are duplicates")
	}
	if !has(reasons(tx(1, "2026-06-01", -20.01, "CAFE ONE 12", 0), tx(9, "2026-06-02", -20.01, "CAFE ONE 34", 0)), Duplicate) {
		t.Error("payees sharing a key aren't duplicates")
	}
}

func TestUndecidedComesFromTheCase(t *testing.T) {
	t9 := tx(9, "2026-06-01", -5, "SHOP", 0)
	if !has(NewHistory([]ledger.Transaction{t9}).Reasons(Case{Transaction: t9, Undecided: true}, size, 0), Undecided) {
		t.Error("no undecided")
	}
}

func TestMonthlyIncomeIsACreditInTheCategory(t *testing.T) {
	h := NewHistory(nil)
	credit, debit := tx(9, "2026-06-01", 5, "SSA", 0), tx(8, "2026-06-01", -5, "SSA", 0)
	if !has(h.Reasons(Case{Transaction: credit, Category: 4}, size, 4), MonthlyIncome) {
		t.Error("a Monthly Income credit isn't flagged")
	}
	if has(h.Reasons(Case{Transaction: debit, Category: 4}, size, 4), MonthlyIncome) {
		t.Error("a debit is Monthly Income")
	}
	if has(h.Reasons(Case{Transaction: credit}, size, 0), MonthlyIncome) {
		t.Error("an uncategorized credit is Monthly Income")
	}
}
