package attention

import (
	"math"
	"testing"

	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

var in = Income{Account: "Checking", Category: 1, Bill: 2, Rent: 3, Cash: 4, Target: 3000, Tolerance: 1500}

func btx(id int64, d string, amount, closing float64, cat ledger.CategoryID) ledger.Transaction {
	return ledger.Transaction{ID: id, Date: date(d), Amount: amount, ClosingBalance: closing, Payee: "P", Account: ledger.Account{Name: "Checking"}, State: ledger.State{Category: cat}}
}

func current(t ledger.Transaction) ledger.CategoryID { return t.State.Category }

func near(a, b float64) bool { return math.Abs(a-b) < 0.005 }

// march opens at 2000: SSA 1500 on the 3rd, the transfer on the 9th, rent on the 10th and
// the card bill on the 12th leave the account at 3000, so the transfer the month needed is
// exactly deposit.
func march(deposit float64) []ledger.Transaction {
	return []ledger.Transaction{
		btx(1, "2026-02-27", -10, 2000, 0),
		btx(2, "2026-03-03", 1500, 3500, 1),
		btx(3, "2026-03-09", deposit, 3500+deposit, 1),
		btx(4, "2026-03-10", -1700, 1800+deposit, 3),
		btx(5, "2026-03-12", -(deposit - 1200), 3000, 2),
		btx(6, "2026-04-09", 9999, 99999, 1),
	}
}

func TestCheckEstimatesTheMonth(t *testing.T) {
	m, ok := in.Check(march(6000), current, date("2026-03-01"))
	if !ok || !m.Ready || len(m.Credits) != 2 || !near(m.Actual, 7500) || !near(m.Expected, 7500) || m.Mismatch(1500) {
		t.Errorf("month = %+v, %v", m, ok)
	}
}

func TestMismatchBeyondTheTolerance(t *testing.T) {
	m := Month{Ready: true, Expected: 7500, Actual: 9000}
	if m.Mismatch(1500) {
		t.Error("1500 off is a mismatch")
	}
	m.Actual = 9000.01
	if !m.Mismatch(1500) {
		t.Error("1500.01 off isn't a mismatch")
	}
	m.Ready = false
	if m.Mismatch(1500) {
		t.Error("a month without its bill is a mismatch")
	}
}

func TestCheckWaitsForTheBill(t *testing.T) {
	txs := march(6000)[:4]
	m, ok := in.Check(txs, current, date("2026-03-01"))
	if !ok || m.Ready {
		t.Errorf("month = %+v, want unready", m)
	}
}

func TestCheckAddsSplitCredits(t *testing.T) {
	txs := march(6000)
	txs = append(txs[:3], append([]ledger.Transaction{btx(7, "2026-03-09", 1000, 10500, 1)}, txs[3:]...)...)
	txs[4] = btx(4, "2026-03-10", -1700, 8800, 3)
	txs[5] = btx(5, "2026-03-12", -4800, 4000, 2)
	m, _ := in.Check(txs, current, date("2026-03-01"))
	if len(m.Credits) != 3 || !near(m.Actual, 8500) || !near(m.Expected, 7500) {
		t.Errorf("month = %+v", m)
	}
}

func TestCheckUsesTheRunsCategories(t *testing.T) {
	txs := march(6000)
	txs[2].State.Category = 0
	decided := func(t ledger.Transaction) ledger.CategoryID {
		if t.ID == 3 {
			return 1
		}
		return t.State.Category
	}
	if m, _ := in.Check(txs, decided, date("2026-03-01")); len(m.Credits) != 2 {
		t.Errorf("credits = %+v, want the decided transfer counted", m.Credits)
	}
}

func TestCheckIgnoresOtherAccounts(t *testing.T) {
	txs := march(6000)
	other := btx(8, "2026-03-11", -5000, 0, 2)
	other.Account.Name = "Savings"
	if m, _ := in.Check(append(txs, other), current, date("2026-03-01")); !near(m.Expected, 7500) {
		t.Errorf("expected = %.2f, want 7500", m.Expected)
	}
}

func TestRentBeforeTheFirstCreditIsInTheBalance(t *testing.T) {
	txs := []ledger.Transaction{
		btx(1, "2026-03-01", -10, 3500, 0),
		btx(10, "2026-03-09", 6200, 8000, 1),
		btx(11, "2026-03-09", -1700, 1800, 3),
		btx(12, "2026-03-12", -5000, 3000, 2),
	}
	m, _ := in.Check(txs, current, date("2026-03-01"))
	if !near(m.Expected, 6200) {
		t.Errorf("expected = %.2f, want 6200: the rent posted before the transfer", m.Expected)
	}
}

func TestAMonthWithoutIncomeIsNotChecked(t *testing.T) {
	if _, ok := in.Check([]ledger.Transaction{btx(1, "2026-03-12", -5000, 3000, 2)}, current, date("2026-03-01")); ok {
		t.Error("a month without Monthly Income was checked")
	}
}

func TestInOrderFollowsTheBalances(t *testing.T) {
	got := InOrder([]ledger.Transaction{
		btx(3, "2026-03-09", 10, 120, 0),
		btx(1, "2026-03-09", -50, 110, 0),
		btx(2, "2026-03-09", 60, 160, 0),
		btx(9, "2026-03-08", 1, 100, 0),
	})
	if got[0].ID != 9 || got[1].ID != 2 || got[2].ID != 1 || got[3].ID != 3 {
		t.Errorf("order = %d %d %d %d, want 9 2 1 3", got[0].ID, got[1].ID, got[2].ID, got[3].ID)
	}
}

func TestInOrderKeepsIDOrderWhenBalancesDontChain(t *testing.T) {
	got := InOrder([]ledger.Transaction{
		btx(2, "2026-03-09", -5, 0, 0),
		btx(1, "2026-03-09", -5, 0, 0),
	})
	if got[0].ID != 1 || got[1].ID != 2 {
		t.Errorf("order = %d %d, want 1 2", got[0].ID, got[1].ID)
	}
}

func TestCheckWaitsForAKnownBalance(t *testing.T) {
	txs := march(6000)
	txs[1].ClosingBalance = 0
	m, ok := in.Check(txs, current, date("2026-03-01"))
	if !ok || m.Ready || m.Mismatch(1500) {
		t.Errorf("month = %+v, want unready without a closing balance", m)
	}
}

func TestCheckCountsCashAfterTheFirstCredit(t *testing.T) {
	txs := []ledger.Transaction{
		btx(1, "2026-02-27", -10, 2000, 0),
		btx(20, "2026-03-02", -500, 1500, 4),
		btx(2, "2026-03-03", 1500, 3000, 1),
		btx(21, "2026-03-05", -700, 2300, 4),
		btx(3, "2026-03-09", 6700, 9000, 1),
		btx(4, "2026-03-10", -1700, 7300, 3),
		btx(5, "2026-03-12", -4300, 3000, 2),
	}
	m, _ := in.Check(txs, current, date("2026-03-01"))
	if !near(m.Expected, 8200) || !near(m.Actual, 8200) {
		t.Errorf("month = %+v, want 8200 expected: the cash before the first credit is in the balance", m)
	}
}
