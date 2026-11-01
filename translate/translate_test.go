package translate

import (
	"strings"
	"testing"

	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

var titles = map[ledger.CategoryID]string{3: "Phone", 5: "Food", 6: "Fun"}

func h(payee string, cat ledger.CategoryID) ledger.Transaction {
	return ledger.Transaction{Payee: payee, Amount: -1, State: ledger.State{Category: cat}}
}

func TestReadExport(t *testing.T) {
	got, err := ReadExport(strings.NewReader(`[{"id":1,"payee_matches":"ACME","category":{"id":3,"title":"Phone"}}]`))
	if err != nil || len(got) != 1 || got[0] != (ExportRule{PayeeMatches: "ACME", Category: "Phone"}) {
		t.Errorf("ReadExport = %+v, %v", got, err)
	}
}

func TestTranslateDropsRulesWithNoMatch(t *testing.T) {
	r := Translate([]ExportRule{{"ACME", "Phone"}, {"GONE", "Food"}}, []ledger.Transaction{h("acme mobile", 3)}, titles)
	if len(r.Rules) != 1 || r.Rules[0].Category != "Phone" || len(r.Dropped) != 1 || r.Dropped[0].PayeeMatches != "GONE" {
		t.Errorf("Translate = %+v", r)
	}
}

func TestTranslateKeepsPocketSmithsSubstringSemantics(t *testing.T) {
	r := Translate([]ExportRule{{"Cafe", "Food"}}, []ledger.Transaction{h("WWW.SFCAFECITY.COM", 5)}, titles)
	if len(r.Rules) != 1 || r.Rules[0].Payee != `(?i)Cafe` {
		t.Errorf("Translate = %+v", r)
	}
}

func TestTranslateMergesDuplicatesByTheUsersCategorizations(t *testing.T) {
	export := []ExportRule{{"Shop Co", "Fun"}, {"SHOP CO", "Food"}}
	hist := []ledger.Transaction{h("SHOP CO 1", 5), h("SHOP CO 2", 5), h("SHOP CO 3", 6)}
	r := Translate(export, hist, titles)
	if len(r.Rules) != 1 || r.Rules[0].Category != "Food" {
		t.Fatalf("rules = %+v", r.Rules)
	}
	if len(r.Merged) != 1 || r.Merged[0].Chosen != "Food" || len(r.Merged[0].Categories) != 2 {
		t.Errorf("merged = %+v", r.Merged)
	}
}

func TestTranslatePutsSpecificPatternsFirst(t *testing.T) {
	export := []ExportRule{{"FI", "Phone"}, {"GOOGLE *FITBOD", "Fun"}}
	hist := []ledger.Transaction{h("FI 123", 3), h("GOOGLE *FITBOD W", 6)}
	r := Translate(export, hist, titles)
	if len(r.Rules) != 2 || r.Rules[0].Category != "Fun" || r.Rules[1].Category != "Phone" {
		t.Errorf("rules = %+v", r.Rules)
	}
}

func TestWriteRulesIsLoadableTOML(t *testing.T) {
	var b strings.Builder
	if err := WriteRules(&b, []Rule{{Payee: `(?i)A\.B`, Category: "Food"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "[[rule]]") || !strings.Contains(b.String(), `category = "Food"`) {
		t.Errorf("WriteRules:\n%s", b.String())
	}
}

func TestWriteReportCounts(t *testing.T) {
	var b strings.Builder
	r := Result{Rules: []Rule{{Payee: "(?i)A", Category: "Food"}}, Dropped: []ExportRule{{"GONE", "Food"}}, Merged: []Merge{{Pattern: "SHOP CO", Categories: []string{"Fun", "Food"}, Chosen: "Food"}}}
	if err := WriteReport(&b, r); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"1 rules kept, 1 dropped with no match, 1 duplicate patterns merged", "dropped: GONE (Food)", "merged: SHOP CO [Fun Food] -> Food"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("report lacks %q:\n%s", want, b.String())
		}
	}
}
