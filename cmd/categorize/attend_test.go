package main

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/attention"
	"github.com/cehbz/pocketsmith-categorizer/categorize"
	"github.com/cehbz/pocketsmith-categorizer/decide"
	"github.com/cehbz/pocketsmith-categorizer/internal/day"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
	"github.com/cehbz/pocketsmith-categorizer/memo"
)

var set = attention.Settings{
	Start:  mustParse("2026-03-01"),
	Size:   attention.Size{Absolute: 500, SD: 1},
	Income: attention.Income{Account: "Checking", Category: 1, Bill: 2, Rent: 3, Target: 3000, Tolerance: 1500},
}

func mustParse(s string) time.Time {
	d, err := day.Parse(s)
	if err != nil {
		panic(err)
	}
	return d
}

func atx(id int64, d string, amount, closing float64, payee string, s ledger.State) ledger.Transaction {
	return ledger.Transaction{ID: id, Date: mustParse(d), Amount: amount, ClosingBalance: closing, Payee: payee, Account: ledger.Account{Name: "Checking"}, State: s}
}

func emptyMemo(t *testing.T) *memo.Memo {
	t.Helper()
	m, err := memo.Load(filepath.Join(t.TempDir(), "judged.json"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestAttendJudgesEachTransactionOnce(t *testing.T) {
	txs := []ledger.Transaction{atx(1, "2026-03-05", -5, 0, "NEW SHOP", ledger.State{NeedsReview: true})}
	seen := emptyMemo(t)
	j := attend(txs, decide.Plan{Undecided: txs}, set, seen, mustParse("2026-03-06"))
	if !slices.Equal(j.reasons[1], []string{attention.NewPayee, attention.Undecided}) || !slices.Equal(j.judged, []int64{1}) {
		t.Fatalf("judgement = %+v", j)
	}
	remember(seen, j, nil)
	if j := attend(txs, decide.Plan{Undecided: txs}, set, seen, mustParse("2026-03-06")); len(j.reasons) != 0 || len(j.judged) != 0 {
		t.Errorf("second judgement = %+v, want none", j)
	}
}

func TestAttendJudgesOldTransactionsOnlyWhenTheyNeedReview(t *testing.T) {
	txs := []ledger.Transaction{
		atx(1, "2026-02-27", -5, 0, "OLD SHOP", ledger.State{Category: 4}),
		atx(2, "2026-02-27", -5, 0, "WAITING SHOP", ledger.State{NeedsReview: true}),
		atx(3, "2026-03-01", -5, 0, "NEW SHOP", ledger.State{Category: 4}),
	}
	j := attend(txs, decide.Plan{}, set, emptyMemo(t), mustParse("2026-03-06"))
	if !slices.Equal(j.judged, []int64{2, 3}) {
		t.Errorf("judged = %v, want 2 3", j.judged)
	}
}

func TestAttendJudgesTheDecidedCategory(t *testing.T) {
	credit := atx(1, "2026-03-09", 6000, 9000, "TRANSFER", ledger.State{NeedsReview: true})
	plan := decide.Plan{Changes: []categorize.Change{{Charge: 1, Before: credit.State, After: ledger.State{Category: 1, Labels: []string{"auto-rule"}}}}}
	j := attend([]ledger.Transaction{credit}, plan, set, emptyMemo(t), mustParse("2026-03-10"))
	if !slices.Contains(j.reasons[1], attention.MonthlyIncome) || slices.Contains(j.reasons[1], attention.Undecided) {
		t.Errorf("reasons = %v", j.reasons[1])
	}
}

// mismatched is a March whose 9000 transfer is 1500.01 more than the month needed.
func mismatched() []ledger.Transaction {
	return []ledger.Transaction{
		atx(1, "2026-02-27", -10, 2000, "OPENING", ledger.State{}),
		atx(2, "2026-03-09", 9000, 11000, "TRANSFER", ledger.State{Category: 1, Labels: []string{attention.MonthlyIncome}}),
		atx(3, "2026-03-12", -6499.99, 4500.01, "CARD", ledger.State{Category: 2}),
	}
}

func TestAttendFlagsAMismatchedMonthTheUserCleared(t *testing.T) {
	seen := emptyMemo(t)
	for _, id := range []int64{1, 2, 3} {
		seen.Add(judgedKey(id))
	}
	j := attend(mismatched(), decide.Plan{}, set, seen, mustParse("2026-03-13"))
	if !slices.Equal(j.reasons[2], []string{attention.IncomeMismatch}) || len(j.months) != 1 {
		t.Errorf("judgement = %+v", j)
	}
	remember(seen, j, nil)
	if j := attend(mismatched(), decide.Plan{}, set, seen, mustParse("2026-03-13")); len(j.months) != 0 {
		t.Errorf("a checked month was checked again: %+v", j.months)
	}
}

func TestAttendRetriesAMonthWithoutItsBill(t *testing.T) {
	seen := emptyMemo(t)
	j := attend(mismatched()[:2], decide.Plan{}, set, seen, mustParse("2026-03-10"))
	remember(seen, j, nil)
	if len(j.months) != 1 || j.months[0].Ready || seen.Seen(incomeKey(j.months[0].Start)) {
		t.Errorf("months = %+v; an unready month was remembered", j.months)
	}
}

func TestRememberSkipsUnwrittenJudgements(t *testing.T) {
	seen := emptyMemo(t)
	j := judgement{judged: []int64{1, 2}, months: []attention.Month{{Start: mustParse("2026-03-01"), Ready: true, Credits: []ledger.Transaction{{ID: 2}}}}}
	remember(seen, j, []categorize.Change{{Charge: 2}})
	if !seen.Seen(judgedKey(1)) || seen.Seen(judgedKey(2)) || seen.Seen(incomeKey(mustParse("2026-03-01"))) {
		t.Error("remembered an unwritten judgement, or forgot a written one")
	}
}

func TestLandedDropsUnwrittenReasons(t *testing.T) {
	got := landed(map[int64][]string{1: {"a"}, 2: {"b"}}, []categorize.Change{{Charge: 2}})
	if len(got) != 1 || got[1][0] != "a" {
		t.Errorf("landed = %v", got)
	}
}

func TestAttendSkipsAMonthWithACorrectedCredit(t *testing.T) {
	seen := emptyMemo(t)
	for _, id := range []int64{1, 2, 3} {
		seen.Add(judgedKey(id))
	}
	plan := decide.Plan{Corrections: []categorize.Change{{Charge: 2}}}
	j := attend(mismatched(), plan, set, seen, mustParse("2026-03-13"))
	if len(j.months) != 0 || len(j.reasons[2]) != 0 {
		t.Errorf("judgement = %+v, want the month skipped", j)
	}
}

func TestAttendJudgesTheCreditsAMismatchFlags(t *testing.T) {
	late := set
	late.Start = mustParse("2026-03-10")
	seen := emptyMemo(t)
	j := attend(mismatched(), decide.Plan{}, late, seen, mustParse("2026-03-13"))
	if !slices.Contains(j.judged, 2) || !slices.Contains(j.reasons[2], attention.MonthlyIncome) || !slices.Contains(j.reasons[2], attention.IncomeMismatch) {
		t.Fatalf("judgement = %+v, want the flagged credit judged", j)
	}
	remember(seen, j, nil)
	flagged := mismatched()
	flagged[1].State.NeedsReview = true
	if j := attend(flagged, decide.Plan{}, late, seen, mustParse("2026-03-13")); len(j.reasons) != 0 || len(j.judged) != 0 {
		t.Errorf("second judgement = %+v, want none", j)
	}
}
