package config

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
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

func TestReadKeyRunsOpRead(t *testing.T) {
	op := write(t, "op", "#!/bin/sh\n[ \"$1\" = read ] && [ \"$2\" = \"op://Vault/Item/field\" ] && echo secret-value\n", 0o755)
	key, err := ReadKey(context.Background(), op, "op://Vault/Item/field")
	if err != nil || key != "secret-value" {
		t.Errorf("ReadKey = %q, %v", key, err)
	}
}

func TestReadKeyFailsOnEmptyOrFailedOutput(t *testing.T) {
	for name, script := range map[string]string{
		"empty":  "#!/bin/sh\necho\n",
		"failed": "#!/bin/sh\nexit 1\n",
	} {
		op := write(t, "op", script, 0o755)
		if _, err := ReadKey(context.Background(), op, "op://Vault/Item/field"); err == nil {
			t.Errorf("%s: ReadKey succeeded", name)
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

func TestReadKeyReportsOpStderrButNotStdout(t *testing.T) {
	op := write(t, "op", "#!/bin/sh\necho partial-secret\necho \"not signed in\" >&2\nexit 1\n", 0o755)
	_, err := ReadKey(context.Background(), op, "op://Vault/Item/field")
	if err == nil {
		t.Fatal("ReadKey succeeded")
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
