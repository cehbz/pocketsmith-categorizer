package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
category = "Phone"
`)
	r := rs[0]
	if r.Payee != `(?i)^acme mobile\b` || r.Account != "Checking" || r.Type != "debit" || r.Min != 10 || r.Max != 100 || r.CategoryID() != 3 {
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

func TestFirstMatchingRuleDecides(t *testing.T) {
	rs := mustLoad(t, "[[rule]]\npayee = \"(?i)fitbod\"\ncategory = \"Food\"\n[[rule]]\npayee = \"(?i)^google\"\ncategory = \"Phone\"\n")
	d, ok := rs.Decide(tx("GOOGLE *Fitbod", "Card", -5, ledger.State{}))
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
