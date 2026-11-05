package rules

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/categorize"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

var categories = []ledger.Category{{ID: 3, Title: "Phone"}, {ID: 4, Title: "Cash"}, {ID: 5, Title: "Food"}}

func load(t *testing.T, content string) (Rules, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rules.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return Load(path, categories)
}

func mustLoad(t *testing.T, content string) Rules {
	t.Helper()
	rs, err := load(t, content)
	if err != nil {
		t.Fatal(err)
	}
	return rs
}

func tx(payee, account string, amount float64, s ledger.State) ledger.Transaction {
	return ledger.Transaction{ID: 1, Payee: payee, Account: ledger.Account{ID: 1, Name: account}, Amount: amount, State: s}
}

func TestLoadReadsEveryField(t *testing.T) {
	rs := mustLoad(t, `
[[rule]]
payee = "(?i)^acme mobile\\b"
account = "Checking"
type = "debit"
min = 10
max = 100
days = [8, 11]
category = "Phone"
`)
	r := rs[0]
	if r.Payee != `(?i)^acme mobile\b` || r.Account != "Checking" || r.Type != "debit" || r.Min != 10 || r.Max != 100 || !slices.Equal(r.Days, []int{8, 11}) || r.CategoryID() != 3 {
		t.Errorf("rule = %+v, category %d", r, r.CategoryID())
	}
}

func TestLoadRejectsBadRules(t *testing.T) {
	for name, content := range map[string]string{
		"bad pattern":      "[[rule]]\npayee = \"(\"\ncategory = \"Phone\"\n",
		"unknown category": "[[rule]]\npayee = \"x\"\ncategory = \"Nope\"\n",
		"bad type":         "[[rule]]\npayee = \"x\"\ntype = \"both\"\ncategory = \"Phone\"\n",
		"bad range":        "[[rule]]\npayee = \"x\"\nmin = 5\nmax = 1\ncategory = \"Phone\"\n",
		"no condition":     "[[rule]]\ncategory = \"Phone\"\n",
		"day zero":         "[[rule]]\npayee = \"x\"\ndays = [0, 5]\ncategory = \"Phone\"\n",
		"day 32":           "[[rule]]\npayee = \"x\"\ndays = [8, 32]\ncategory = \"Phone\"\n",
		"reversed days":    "[[rule]]\npayee = \"x\"\ndays = [11, 8]\ncategory = \"Phone\"\n",
		"one day":          "[[rule]]\npayee = \"x\"\ndays = [8]\ncategory = \"Phone\"\n",
		"unknown key":      "[[rule]]\npayee = \"x\"\ncategry = \"Phone\"\ncategory = \"Phone\"\n",
	} {
		if _, err := load(t, content); err == nil {
			t.Errorf("%s: Load succeeded", name)
		}
	}
}

func TestLoadNamesTheRuleAtFault(t *testing.T) {
	_, err := load(t, "[[rule]]\npayee = \"x\"\ncategory = \"Phone\"\n[[rule]]\npayee = \"y\"\ncategory = \"Nope\"\n")
	if err == nil || !strings.Contains(err.Error(), "rule 2") || !strings.Contains(err.Error(), `"Nope"`) {
		t.Errorf("err = %v, want rule 2 and the title", err)
	}
}

func TestMatchesEveryCondition(t *testing.T) {
	rs := mustLoad(t, "[[rule]]\npayee = \"(?i)^atm\"\naccount = \"Checking\"\ntype = \"debit\"\nmin = 100\nmax = 1000\ncategory = \"Cash\"\n")
	r := rs[0]
	cases := map[string]struct {
		t    ledger.Transaction
		want bool
	}{
		"all hold":      {tx("ATM 123 CITY", "Checking", -500, ledger.State{}), true},
		"payee differs": {tx("SHOP", "Checking", -500, ledger.State{}), false},
		"other account": {tx("ATM 1", "Card", -500, ledger.State{}), false},
		"a credit":      {tx("ATM 1", "Checking", 500, ledger.State{}), false},
		"too small":     {tx("ATM 1", "Checking", -50, ledger.State{}), false},
		"too large":     {tx("ATM 1", "Checking", -5000, ledger.State{}), false},
	}
	for name, c := range cases {
		if got := r.Matches(c.t); got != c.want {
			t.Errorf("%s: Matches = %v, want %v", name, got, c.want)
		}
	}
}

func TestMatchesTheDayWindow(t *testing.T) {
	r := mustLoad(t, "[[rule]]\npayee = \"(?i)^funds transfer\"\ndays = [8, 11]\ncategory = \"Food\"\n")[0]
	for day, want := range map[int]bool{7: false, 8: true, 10: true, 11: true, 12: false} {
		tr := tx("Funds Transfer from Brokerage", "Checking", 5000, ledger.State{})
		tr.Date = time.Date(2026, 10, day, 0, 0, 0, 0, time.UTC)
		if got := r.Matches(tr); got != want {
			t.Errorf("day %d: Matches = %v, want %v", day, got, want)
		}
	}
}

func TestNoDayWindowMatchesAnyDay(t *testing.T) {
	r := mustLoad(t, "[[rule]]\npayee = \"(?i)^funds transfer\"\ncategory = \"Food\"\n")[0]
	for _, day := range []int{1, 15, 31} {
		tr := tx("Funds Transfer from Brokerage", "Checking", 5000, ledger.State{})
		tr.Date = time.Date(2026, 10, day, 0, 0, 0, 0, time.UTC)
		if !r.Matches(tr) {
			t.Errorf("day %d: no match", day)
		}
	}
}

func TestFirstMatchingRuleDecides(t *testing.T) {
	rs := mustLoad(t, "[[rule]]\npayee = \"(?i)fitapp\"\ncategory = \"Food\"\n[[rule]]\npayee = \"(?i)^google\"\ncategory = \"Phone\"\n")
	d, ok := rs.Decide(tx("GOOGLE *Fitapp", "Card", -5, ledger.State{}))
	if !ok || d.Category != 5 || d.Label != Label || d.Source != rs[0].Key() {
		t.Errorf("Decide = %+v, %v", d, ok)
	}
	if _, ok := rs.Decide(tx("SHOP", "Card", -5, ledger.State{})); ok {
		t.Error("an unmatched transaction was decided")
	}
}

func TestKeyChangesWithTheRule(t *testing.T) {
	a := mustLoad(t, "[[rule]]\npayee = \"x\"\ncategory = \"Phone\"\n")[0]
	b := mustLoad(t, "[[rule]]\npayee = \"x\"\ncategory = \"Food\"\n")[0]
	if a.Key() == b.Key() {
		t.Errorf("keys equal: %q", a.Key())
	}
	c := mustLoad(t, "[[rule]]\npayee = \"x\"\ndays = [8, 11]\ncategory = \"Phone\"\n")[0]
	if a.Key() == c.Key() {
		t.Errorf("a day window left the key unchanged: %q", a.Key())
	}
}

func TestReplayCountsAgreementWithTheUser(t *testing.T) {
	rs := mustLoad(t, "[[rule]]\npayee = \"(?i)acme\"\ncategory = \"Phone\"\n")
	txs := []ledger.Transaction{
		tx("ACME 1", "Card", -5, ledger.State{Category: 3}),
		tx("ACME 2", "Card", -5, ledger.State{Category: 5}),
		tx("ACME 3", "Card", -5, ledger.State{Category: 5, NeedsReview: true}),
		tx("ACME 4", "Card", -5, ledger.State{Category: 5, Labels: []string{"auto-rule"}}),
		tx("OTHER", "Card", -5, ledger.State{Category: 5}),
	}
	st := rs.Replay(txs)
	if len(st) != 1 || st[0].Matches != 2 || st[0].Agree != 1 || len(st[0].Disagree) != 1 || st[0].Disagree[0].Payee != "ACME 2" {
		t.Errorf("Replay = %+v", st)
	}
}

func TestReplaySkipsRejectedRules(t *testing.T) {
	rs := mustLoad(t, "[[rule]]\npayee = \"(?i)acme\"\ncategory = \"Food\"\nstate = \"rejected\"\n[[rule]]\npayee = \"(?i)acme\"\ncategory = \"Phone\"\n")
	st := rs.Replay([]ledger.Transaction{tx("ACME 1", "Card", -5, ledger.State{Category: 3})})
	if len(st) != 1 || st[0].Rule.Category != "Phone" || st[0].Matches != 1 || st[0].Agree != 1 {
		t.Errorf("Replay = %+v; want the Phone rule agreeing once", st)
	}
}

func TestADecisionNamesItsRule(t *testing.T) {
	rs := mustLoad(t, "[[rule]]\npayee = \"(?i)fitapp\"\ncategory = \"Food\"\n")
	d, _ := rs.Decide(tx("GOOGLE *Fitapp", "Card", -5, ledger.State{}))
	if want := (categorize.Reason{Why: categorize.ByRule, Rule: "fitapp"}); d.Reason != want {
		t.Errorf("reason = %+v, want %+v", d.Reason, want)
	}
}

func TestARuleIsNamedByItsPayeePatternElseItsAccount(t *testing.T) {
	rs := mustLoad(t, "[[rule]]\npayee = \"(?i)^Cafe\\\\*One\"\naccount = \"Card\"\ncategory = \"Food\"\n"+
		"[[rule]]\npayee = \"Shop (?i)two\"\ncategory = \"Food\"\n"+
		"[[rule]]\naccount = \"Checking\"\ntype = \"credit\"\ncategory = \"Cash\"\n")
	for i, want := range []string{`^Cafe\*One`, "Shop (?i)two", "account Checking"} {
		if got := rs[i].Name(); got != want {
			t.Errorf("rule %d: Name = %q, want %q", i, got, want)
		}
		if got, ok := KeyName(rs[i].Key()); !ok || got != want {
			t.Errorf("rule %d: KeyName = %q, %t; want %q", i, got, ok, want)
		}
	}
	for _, source := range []string{"grab A-TESTRIDE0001", "trips: trip-abroad", ""} {
		if got, ok := KeyName(source); ok {
			t.Errorf("KeyName(%q) = %q; want no rule", source, got)
		}
	}
}

func TestLoadReadsStateAndStartDate(t *testing.T) {
	r := mustLoad(t, "[[rule]]\npayee = \"x\"\ncategory = \"Phone\"\nstate = \"tentative\"\nfrom = 2026-10-01\n")[0]
	if r.State != Tentative || !r.From.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("rule = %+v; want tentative from 2026-10-01 UTC", r)
	}
}

func TestLoadRejectsABadState(t *testing.T) {
	if _, err := load(t, "[[rule]]\npayee = \"x\"\ncategory = \"Phone\"\nstate = \"maybe\"\n"); err == nil {
		t.Error("state maybe loaded")
	}
}

func TestARuleDoesntMatchChargesBeforeItsStartDate(t *testing.T) {
	rs := mustLoad(t, "[[rule]]\npayee = \"(?i)acme\"\ncategory = \"Phone\"\nfrom = 2026-10-01\n")
	before, on := tx("ACME", "Card", -5, ledger.State{}), tx("ACME", "Card", -5, ledger.State{})
	before.Date, on.Date = time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if _, ok := rs.Match(before); ok {
		t.Error("a charge before the start date matched")
	}
	if _, ok := rs.Match(on); !ok {
		t.Error("a charge on the start date didn't match")
	}
	if !rs.Covers(before) {
		t.Error("the rule doesn't cover its payee's charge before its start date")
	}
}

func TestARejectedRuleDecidesNothingButCoversItsPayees(t *testing.T) {
	rs := mustLoad(t, "[[rule]]\npayee = \"(?i)acme\"\ncategory = \"Phone\"\nstate = \"rejected\"\n")
	acme := tx("ACME", "Card", -5, ledger.State{})
	if _, ok := rs.Decide(acme); ok {
		t.Error("a rejected rule decided")
	}
	if !rs.Covers(acme) {
		t.Error("a rejected rule doesn't cover its payee")
	}
	if rs.Covers(tx("OTHER", "Card", -5, ledger.State{})) {
		t.Error("a rule covers a payee it doesn't match")
	}
}

func TestATentativeRulesDecisionIsTentative(t *testing.T) {
	for state, want := range map[string]bool{"": false, Confirmed: false, Tentative: true} {
		rs := mustLoad(t, "[[rule]]\npayee = \"(?i)acme\"\ncategory = \"Phone\"\nstate = \""+state+"\"\n")
		if d, ok := rs.Decide(tx("ACME", "Card", -5, ledger.State{})); !ok || d.Tentative != want {
			t.Errorf("state %q: decision %+v, %v; want tentative %v", state, d, ok, want)
		}
	}
}

func TestKeyNamesTheStartDateButNotTheState(t *testing.T) {
	plain := mustLoad(t, "[[rule]]\npayee = \"x\"\ncategory = \"Phone\"\n")[0]
	tentative := mustLoad(t, "[[rule]]\npayee = \"x\"\ncategory = \"Phone\"\nstate = \"tentative\"\n")[0]
	dated := mustLoad(t, "[[rule]]\npayee = \"x\"\ncategory = \"Phone\"\nfrom = 2026-10-01\n")[0]
	if want := `payee="x" account="" type="" min=0 max=0 category="Phone"`; plain.Key() != want {
		t.Errorf("key = %q, want %q", plain.Key(), want)
	}
	if tentative.Key() != plain.Key() {
		t.Errorf("the state changed the key: %q", tentative.Key())
	}
	if want := `payee="x" account="" type="" min=0 max=0 from=2026-10-01 category="Phone"`; dated.Key() != want {
		t.Errorf("key = %q, want %q", dated.Key(), want)
	}
}
