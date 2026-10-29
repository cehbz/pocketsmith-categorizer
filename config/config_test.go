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
