package config

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

func write(t *testing.T, name, content string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

const full = `
key_ref = "op://Vault/Item/field"
account_id = 42
payees = ["WWW.GRAB.COM", "GRABTAXI"]

[categories]
ride = 7
delivery = 9
`

func TestLoadReadsEveryFieldAndDefaultsTheLabel(t *testing.T) {
	c, err := Load(write(t, "config.toml", full, 0o600))
	if err != nil {
		t.Fatal(err)
	}
	if c.KeyRef != "op://Vault/Item/field" || c.AccountID != 42 || !slices.Equal(c.Payees, []string{"WWW.GRAB.COM", "GRABTAXI"}) {
		t.Errorf("config = %+v", c)
	}
	if c.Label != "grab-auto" {
		t.Errorf("label = %q, want grab-auto", c.Label)
	}
	p := c.Policy()
	if p.Ride != 7 || p.Delivery != 9 || p.Label != "grab-auto" {
		t.Errorf("policy = %+v", p)
	}
}

func TestLoadReadsIntegerTipAmounts(t *testing.T) {
	c, err := Load(write(t, "config.toml", full+"\n[tips]\nride = [60]\ndelivery = [20, 50]\n", 0o600))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(c.Tips.Ride, []float64{60}) || !slices.Equal(c.Tips.Delivery, []float64{20, 50}) {
		t.Errorf("tips = %+v, want ride [60], delivery [20 50]", c.Tips)
	}
}

func TestLoadWithoutTipsHasNone(t *testing.T) {
	c, err := Load(write(t, "config.toml", full, 0o600))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Tips.Ride) != 0 || len(c.Tips.Delivery) != 0 {
		t.Errorf("tips = %+v, want none", c.Tips)
	}
}

func TestLoadNamesMissingFields(t *testing.T) {
	_, err := Load(write(t, "config.toml", `label = "x"`, 0o600))
	if err == nil {
		t.Fatal("Load accepted an empty config")
	}
	for _, f := range []string{"key_ref", "account_id", "payees", "categories.ride", "categories.delivery"} {
		if !strings.Contains(err.Error(), f) {
			t.Errorf("error %q doesn't name %s", err, f)
		}
	}
}

func TestPathsAreUnderHome(t *testing.T) {
	cfg, journal := Paths("/h")
	if cfg != "/h/.config/pocketsmith-categorizer/config.toml" || journal != "/h/.local/state/pocketsmith-categorizer/journal" {
		t.Errorf("Paths = %s, %s", cfg, journal)
	}
}

func TestReadSecretRunsOpRead(t *testing.T) {
	op := write(t, "op", "#!/bin/sh\n[ \"$1\" = read ] && [ \"$2\" = \"op://Vault/Item/field\" ] && echo secret-value\n", 0o755)
	key, err := ReadSecret(context.Background(), op, "op://Vault/Item/field")
	if err != nil || key != "secret-value" {
		t.Errorf("ReadSecret = %q, %v", key, err)
	}
}

func TestReadSecretFailsOnEmptyOrFailedOutput(t *testing.T) {
	for name, script := range map[string]string{
		"empty":  "#!/bin/sh\necho\n",
		"failed": "#!/bin/sh\nexit 1\n",
	} {
		op := write(t, "op", script, 0o755)
		if _, err := ReadSecret(context.Background(), op, "op://Vault/Item/field"); err == nil {
			t.Errorf("%s: ReadSecret succeeded", name)
		}
	}
}

func TestReadSecretReadsAnOwnerOnlyFile(t *testing.T) {
	path := write(t, "key", "secret-value\n", 0o600)
	key, err := ReadSecret(context.Background(), "no-op", path)
	if err != nil || key != "secret-value" {
		t.Errorf("ReadSecret = %q, %v", key, err)
	}
}

func TestReadSecretRefusesAFileOthersCanRead(t *testing.T) {
	for _, mode := range []os.FileMode{0o640, 0o604} {
		path := write(t, "key", "secret-value\n", mode)
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		_, err := ReadSecret(context.Background(), "no-op", path)
		if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), mode.String()) {
			t.Errorf("mode %v: err = %v, want the path and mode named", mode, err)
		}
	}
}

func TestReadSecretRefusesAnEmptyOrMissingFile(t *testing.T) {
	for name, path := range map[string]string{
		"empty":   write(t, "key", "\n", 0o600),
		"missing": filepath.Join(t.TempDir(), "absent"),
	} {
		if _, err := ReadSecret(context.Background(), "no-op", path); err == nil {
			t.Errorf("%s: ReadSecret succeeded", name)
		}
	}
}

func TestLoadRejectsUnknownKeys(t *testing.T) {
	path := write(t, "config.toml", "lable = \"x\"\n"+full+"rid = 8\n", 0o600)
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load accepted unknown keys")
	}
	if want := "config " + path + ": unknown keys lable, categories.rid"; err.Error() != want {
		t.Errorf("error = %q, want %q", err, want)
	}
}

func TestReadSecretReportsOpStderrButNotStdout(t *testing.T) {
	op := write(t, "op", "#!/bin/sh\necho partial-secret\necho \"not signed in\" >&2\nexit 1\n", 0o755)
	_, err := ReadSecret(context.Background(), op, "op://Vault/Item/field")
	if err == nil {
		t.Fatal("ReadSecret succeeded")
	}
	if want := "op read op://Vault/Item/field: exit status 1: not signed in"; err.Error() != want {
		t.Errorf("error = %q, want %q", err, want)
	}
}

func TestRulesAndMemoPathsAreUnderHome(t *testing.T) {
	if got := RulesPath("/h"); got != "/h/.config/pocketsmith-categorizer/rules.toml" {
		t.Errorf("RulesPath = %q", got)
	}
	if got := MemoPath("/h"); got != "/h/.local/state/pocketsmith-categorizer/shown.json" {
		t.Errorf("MemoPath = %q", got)
	}
}

const attentionConfig = `
[attention]
start = "2026-03-01"
large = 500
sd = 1

[income]
account = "Checking"
category = "Monthly Income"
bill = "Card Payment"
rent = "Rent"
cash = "Cash"
target = 3000
tolerance = 1500
`

var cats = []ledger.Category{{ID: 1, Title: "Monthly Income"}, {ID: 2, Title: "Card Payment"}, {ID: 3, Title: "Rent"}, {ID: 4, Title: "Cash"}}

func TestSettingsResolvesTitles(t *testing.T) {
	c, err := Load(write(t, "config.toml", full+attentionConfig, 0o600))
	if err != nil {
		t.Fatal(err)
	}
	s, err := c.Settings(cats)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Start.Equal(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)) || s.Size.Absolute != 500 || s.Size.SD != 1 {
		t.Errorf("settings = %+v", s)
	}
	in := s.Income
	if in.Account != "Checking" || in.Category != 1 || in.Bill != 2 || in.Rent != 3 || in.Cash != 4 || in.Target != 3000 || in.Tolerance != 1500 {
		t.Errorf("income = %+v", in)
	}
}

func TestSettingsNamesWhatIsMissing(t *testing.T) {
	c, err := Load(write(t, "config.toml", full, 0o600))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Settings(cats)
	if err == nil {
		t.Fatal("Settings accepted a config without them")
	}
	for _, f := range []string{"attention.start", "attention.large", "attention.sd", "income.account", "income.category", "income.bill", "income.rent", "income.cash", "income.target", "income.tolerance"} {
		if !strings.Contains(err.Error(), f) {
			t.Errorf("error %q doesn't name %s", err, f)
		}
	}
}

func TestSettingsRejectsUnknownTitlesAndBadDates(t *testing.T) {
	for name, content := range map[string]string{
		"title": strings.Replace(attentionConfig, `rent = "Rent"`, `rent = "Lodging"`, 1),
		"date":  strings.Replace(attentionConfig, `"2026-03-01"`, `"March"`, 1),
	} {
		c, err := Load(write(t, "config.toml", full+content, 0o600))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.Settings(cats); err == nil {
			t.Errorf("%s: Settings succeeded", name)
		}
	}
}

func TestJudgedPathIsUnderHome(t *testing.T) {
	if got := JudgedPath("/h"); got != "/h/.local/state/pocketsmith-categorizer/judged.json" {
		t.Errorf("JudgedPath = %q", got)
	}
}
