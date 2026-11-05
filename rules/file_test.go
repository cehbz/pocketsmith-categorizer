package rules

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const fileText = `# my rules
[[rule]]
payee = '(?i)^acme'   # the phone company
category = "Phone"

# cash
[[rule]]
  payee = '(?i)^atm'
  category = "Cash"   # withdrawals
  from = 2026-01-01
`

func revise(t *testing.T, text string, edits []Edit, additions []Addition) (string, Rules, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rules.toml")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	rs, err := File{Path: path, Categories: categories}.Revise(edits, additions)
	b, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatal(rerr)
	}
	return string(b), rs, err
}

func TestReviseSetsARulesFieldsAndLeavesTheRestAsItWas(t *testing.T) {
	r := Rule{Category: "Food", State: Tentative, From: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
	got, rs, err := revise(t, fileText, []Edit{{Index: 0, Rule: r}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := `# my rules
[[rule]]
payee = '(?i)^acme'   # the phone company
category = "Food"
state = "tentative"
from = 2026-10-01

# cash
[[rule]]
  payee = '(?i)^atm'
  category = "Cash"   # withdrawals
  from = 2026-01-01
`
	if got != want {
		t.Errorf("file =\n%s\nwant\n%s", got, want)
	}
	if len(rs) != 2 || rs[0].Category != "Food" || rs[0].State != Tentative {
		t.Errorf("rules = %+v", rs)
	}
}

func TestReviseRewritesAnIndentedFieldAndRemovesAStartDate(t *testing.T) {
	got, _, err := revise(t, fileText, []Edit{{Index: 1, Rule: Rule{Category: "Food", State: Tentative}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := `# my rules
[[rule]]
payee = '(?i)^acme'   # the phone company
category = "Phone"

# cash
[[rule]]
  payee = '(?i)^atm'
  category = "Food"
  state = "tentative"
`
	if got != want {
		t.Errorf("file =\n%s\nwant\n%s", got, want)
	}
}

func TestReviseAppendsARuleUnderItsComment(t *testing.T) {
	add := Addition{Comment: "added 2026-11-05 from 3 categorizations of TEA HOUSE", Rule: Rule{Payee: `(?i)\bTEA[^A-Za-z]*HOUSE\b`, Category: "Food", State: Tentative}}
	for _, text := range []string{fileText, fileText[:len(fileText)-1]} {
		got, rs, err := revise(t, text, nil, []Addition{add})
		if err != nil {
			t.Fatal(err)
		}
		want := fileText + `
# added 2026-11-05 from 3 categorizations of TEA HOUSE
[[rule]]
payee = '(?i)\bTEA[^A-Za-z]*HOUSE\b'
category = "Food"
state = "tentative"
`
		if got != want {
			t.Errorf("file =\n%s\nwant\n%s", got, want)
		}
		if len(rs) != 3 || rs[2].CategoryID() != 5 {
			t.Errorf("rules = %+v", rs)
		}
	}
}

func TestARevisionThatDoesntLoadLeavesTheFileAsItWas(t *testing.T) {
	got, _, err := revise(t, fileText, []Edit{{Index: 0, Rule: Rule{Category: "Nonexistent"}}}, nil)
	if err == nil {
		t.Error("a revision to an unknown category succeeded")
	}
	if got != fileText {
		t.Errorf("file =\n%s\nwant it as it was", got)
	}
}
