package attention

import (
	"math"
	"testing"

	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

var in = Income{Account: "Checking", Category: 1, Bill: 2, Rent: 3, Cash: []ledger.CategoryID{4}, Target: 3000, Tolerance: 1500}

func btx(id int64, d string, amount, closing float64, cat ledger.CategoryID) ledger.Transaction {
	return ledger.Transaction{ID: id, Date: date(d), Amount: amount, ClosingBalance: closing, Payee: "P", Account: ledger.Account{Name: "Checking"}, State: ledger.State{Category: cat}}
}

func current(t ledger.Transaction) ledger.CategoryID { return t.State.Category }

func near(a, b float64) bool { return math.Abs(a-b) < 0.005 }

// march opens at 2000: SSA 1500 on the 3rd, the transfer on the 9th, rent on the 10th and
// the card bill on the 12th leave the account at 3000, so the transfer the cycle needed is
// exactly deposit. April's transfer opens the next cycle.
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

// cycle returns the cycle named month among cs.
func cycle(t *testing.T, cs []Cycle, month string) Cycle {
	t.Helper()
	for _, c := range cs {
		if c.Month.Equal(date(month)) {
			return c
		}
	}
	t.Fatalf("no cycle %s in %+v", month, cs)
	return Cycle{}
}

func TestCyclesEstimateTheCycle(t *testing.T) {
	c := cycle(t, in.Cycles(march(6000), current), "2026-03-01")
	if !c.Ready || len(c.Credits) != 2 || !near(c.Actual, 7500) || !near(c.Expected, 7500) || c.Mismatch(1500) {
		t.Errorf("cycle = %+v", c)
	}
}

func TestMismatchBeyondTheTolerance(t *testing.T) {
	c := Cycle{Ready: true, Expected: 7500, Actual: 9000}
	if c.Mismatch(1500) {
		t.Error("1500 off is a mismatch")
	}
	c.Actual = 9000.01
	if !c.Mismatch(1500) {
		t.Error("1500.01 off isn't a mismatch")
	}
	c.Ready = false
	if c.Mismatch(1500) {
		t.Error("a cycle without its bill is a mismatch")
	}
}

func TestACycleWaitsForItsBill(t *testing.T) {
	cs := in.Cycles(march(6000)[:4], current)
	if len(cs) != 1 || cs[0].Ready || !cs[0].Month.Equal(date("2026-03-01")) {
		t.Errorf("cycles = %+v, want March unready", cs)
	}
}

func TestACycleWaitsForItsRent(t *testing.T) {
	txs := march(6000)
	txs[3] = btx(4, "2026-03-10", -0.01, 3500+6000-0.01, 0)
	c := cycle(t, in.Cycles(txs[:5], current), "2026-03-01")
	if c.Ready {
		t.Errorf("cycle = %+v, want unready without rent", c)
	}
}

func TestCyclesAddSplitCredits(t *testing.T) {
	txs := march(6000)
	txs = append(txs[:3], append([]ledger.Transaction{btx(7, "2026-03-09", 1000, 10500, 1)}, txs[3:]...)...)
	txs[4] = btx(4, "2026-03-10", -1700, 8800, 3)
	txs[5] = btx(5, "2026-03-12", -4800, 4000, 2)
	c := cycle(t, in.Cycles(txs, current), "2026-03-01")
	if len(c.Credits) != 3 || !near(c.Actual, 8500) || !near(c.Expected, 7500) {
		t.Errorf("cycle = %+v", c)
	}
}

func TestCyclesUseTheRunsCategories(t *testing.T) {
	txs := march(6000)
	txs[2].State.Category = 0
	decided := func(t ledger.Transaction) ledger.CategoryID {
		if t.ID == 3 {
			return 1
		}
		return t.State.Category
	}
	if c := cycle(t, in.Cycles(txs, decided), "2026-03-01"); len(c.Credits) != 2 {
		t.Errorf("credits = %+v, want the decided transfer counted", c.Credits)
	}
}

func TestCyclesIgnoreOtherAccounts(t *testing.T) {
	other := btx(8, "2026-03-11", -5000, 0, 2)
	other.Account.Name = "Savings"
	if c := cycle(t, in.Cycles(append(march(6000), other), current), "2026-03-01"); !near(c.Expected, 7500) {
		t.Errorf("expected = %.2f, want 7500", c.Expected)
	}
}

func TestRentBeforeTheFirstCreditIsInTheBalance(t *testing.T) {
	txs := []ledger.Transaction{
		btx(1, "2026-03-01", -10, 3500, 0),
		btx(10, "2026-03-09", 6200, 8000, 1),
		btx(11, "2026-03-09", -1700, 1800, 3),
		btx(12, "2026-03-12", -5000, 3000, 2),
	}
	if c := cycle(t, in.Cycles(txs, current), "2026-03-01"); !near(c.Expected, 6200) || !c.Ready {
		t.Errorf("cycle = %+v, want 6200 expected and ready: the rent posted before the transfer", c)
	}
}

func TestNoIncomeMakesNoCycle(t *testing.T) {
	if cs := in.Cycles([]ledger.Transaction{btx(1, "2026-03-12", -5000, 3000, 2)}, current); len(cs) != 0 {
		t.Errorf("cycles = %+v, want none without Monthly Income", cs)
	}
}

func TestACreditAfterTheBillFundsTheNextCycle(t *testing.T) {
	txs := append(march(6000)[:5], btx(6, "2026-03-27", 4000, 7000, 1))
	cs := in.Cycles(txs, current)
	if c := cycle(t, cs, "2026-03-01"); len(c.Credits) != 2 || !near(c.Actual, 7500) {
		t.Errorf("March = %+v, want the top-up left out", c)
	}
	if c := cycle(t, cs, "2026-04-01"); len(c.Credits) != 1 || c.Credits[0].ID != 6 || c.Ready {
		t.Errorf("April = %+v, want the top-up, unready", c)
	}
}

func TestCashAfterTheBillIsTheNextCycles(t *testing.T) {
	txs := append(march(6000)[:5],
		btx(30, "2026-03-20", -900, 2100, 4),
		btx(31, "2026-04-03", 3500, 5600, 1),
		btx(32, "2026-04-10", -1700, 3900, 3),
		btx(33, "2026-04-12", -900, 3000, 2),
	)
	cs := in.Cycles(txs, current)
	if c := cycle(t, cs, "2026-03-01"); !near(c.Expected, 7500) {
		t.Errorf("March expected = %.2f, want 7500: the cash came after its bill", c.Expected)
	}
	if c := cycle(t, cs, "2026-04-01"); !c.Ready || !near(c.Expected, 3500) || !near(c.Actual, 3500) {
		t.Errorf("April = %+v, want 3500 expected: the cash is in its opening balance", c)
	}
}

// lateRent is a March whose rent posts the day after its bill, and an April whose own rent
// hasn't posted.
func lateRent() []ledger.Transaction {
	return []ledger.Transaction{
		btx(1, "2026-02-27", -10, 2000, 0),
		btx(2, "2026-03-03", 1500, 3500, 1),
		btx(3, "2026-03-09", 6000, 9500, 1),
		btx(4, "2026-03-12", -4800, 4700, 2),
		btx(5, "2026-03-13", -1700, 3000, 3),
		btx(6, "2026-04-03", 1500, 4500, 1),
		btx(7, "2026-04-12", -1500, 3000, 2),
	}
}

func TestALateRentBelongsToTheCycleItFollows(t *testing.T) {
	cs := in.Cycles(lateRent(), current)
	if c := cycle(t, cs, "2026-03-01"); !c.Ready || !near(c.Expected, 7500) {
		t.Errorf("March = %+v, want ready at 7500 with the late rent", c)
	}
	if c := cycle(t, cs, "2026-04-01"); c.Ready {
		t.Errorf("April = %+v, want unready: March took the rent", c)
	}
	if cs := in.Cycles(lateRent()[:4], current); cycle(t, cs, "2026-03-01").Ready {
		t.Errorf("March = %+v, want unready before its late rent posts", cs)
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

func TestACycleWaitsForAKnownBalance(t *testing.T) {
	txs := march(6000)
	txs[1].ClosingBalance = 0
	c := cycle(t, in.Cycles(txs, current), "2026-03-01")
	if c.Ready || c.Mismatch(1500) {
		t.Errorf("cycle = %+v, want unready without a closing balance", c)
	}
}

func TestCyclesCountCashAfterTheFirstCredit(t *testing.T) {
	txs := []ledger.Transaction{
		btx(1, "2026-02-27", -10, 2000, 0),
		btx(20, "2026-03-02", -500, 1500, 4),
		btx(2, "2026-03-03", 1500, 3000, 1),
		btx(21, "2026-03-05", -700, 2300, 4),
		btx(3, "2026-03-09", 6700, 9000, 1),
		btx(4, "2026-03-10", -1700, 7300, 3),
		btx(5, "2026-03-12", -4300, 3000, 2),
	}
	c := cycle(t, in.Cycles(txs, current), "2026-03-01")
	if !near(c.Expected, 8200) || !near(c.Actual, 8200) {
		t.Errorf("cycle = %+v, want 8200 expected: the cash before the first credit is in the balance", c)
	}
}

func TestEveryCashCategoryCountsAsCash(t *testing.T) {
	both := in
	both.Cash = []ledger.CategoryID{4, 7}
	for _, cat := range []ledger.CategoryID{4, 7} {
		txs := append(march(6000), btx(7, "2026-03-11", -100, 1700, cat))
		if c := cycle(t, both.Cycles(txs, current), "2026-03-01"); !near(c.Expected, 7600) {
			t.Errorf("withdrawal in category %d: expected %.2f, want 7600", cat, c.Expected)
		}
	}
}
