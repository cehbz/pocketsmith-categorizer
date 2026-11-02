package attention

import (
	"testing"

	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

func TestReplayJudgesEveryTransactionAsNew(t *testing.T) {
	s := Settings{Size: size, Income: in}
	txs := []ledger.Transaction{
		tx(1, "2026-02-20", -5, "OLD SHOP", 0),
		tx(2, "2026-03-02", -5, "OLD SHOP", 0),
		tx(3, "2026-03-03", -600, "OLD SHOP", 0),
		tx(4, "2026-03-10", -5, "NEW SHOP", 0),
	}
	known := func(t ledger.Transaction) (ledger.CategoryID, bool) { return 9, t.Payee == "OLD SHOP" }
	got := Replay(txs, date("2026-03-01"), date("2026-03-14"), s, known)
	if got.Reasons[NewPayee] != 1 || got.Reasons[Large] != 1 || got.Reasons[Undecided] != 1 {
		t.Errorf("reasons = %v", got.Reasons)
	}
	if len(got.Weeks) != 2 || got.Weeks[0] != (Week{All: 1, WithoutUndecided: 1}) || got.Weeks[1] != (Week{All: 1, WithoutUndecided: 1}) {
		t.Errorf("weeks = %+v", got.Weeks)
	}
}

func TestReplayChecksIncomeMonths(t *testing.T) {
	s := Settings{Size: size, Income: in}
	current := func(t ledger.Transaction) (ledger.CategoryID, bool) { return t.State.Category, false }
	got := Replay(march(6000), date("2026-03-01"), date("2026-03-31"), s, current)
	if len(got.Income) != 1 || !near(got.Income[0].Expected, 7500) {
		t.Errorf("income = %+v", got.Income)
	}
}
