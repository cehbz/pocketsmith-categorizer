package main

import (
	"testing"

	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

func TestUndoForgetsTheRevertedJudgements(t *testing.T) {
	seen := emptyMemo(t)
	for _, k := range []string{judgedKey(1), judgedKey(2), incomeKey(mustParse("2026-03-01")), incomeKey(mustParse("2026-04-01"))} {
		seen.Add(k)
	}
	forget(seen, []ledger.Charge{{ID: 1, Date: mustParse("2026-03-09")}})
	if seen.Seen(judgedKey(1)) || seen.Seen(incomeKey(mustParse("2026-03-01"))) {
		t.Error("the reverted charge's judgement or month is still remembered")
	}
	if !seen.Seen(judgedKey(2)) || !seen.Seen(incomeKey(mustParse("2026-04-01"))) {
		t.Error("forgot a judgement or month the undo didn't revert")
	}
}
