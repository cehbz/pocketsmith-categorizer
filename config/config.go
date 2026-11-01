// Package config loads the categorizer's local configuration and its PocketSmith key.
package config

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/cehbz/pocketsmith-categorizer/categorize"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

// DefaultLabel marks the charges the categorizer has matched.
const DefaultLabel = "grab-auto"

// Config is the categorizer's local configuration.
type Config struct {
	KeyRef     string     `toml:"key_ref"`
	AccountID  int64      `toml:"account_id"`
	Payees     []string   `toml:"payees"`
	Label      string     `toml:"label"`
	Categories Categories `toml:"categories"`
	Tips       Tips       `toml:"tips"`
}

// Categories are the PocketSmith category IDs for each kind of service.
type Categories struct {
	Ride     int64 `toml:"ride"`
	Delivery int64 `toml:"delivery"`
}

// Tips are the tip amounts in THB for each kind of service.
type Tips struct {
	Ride     []float64 `toml:"ride"`
	Delivery []float64 `toml:"delivery"`
}

// Load reads and checks a config file.
func Load(path string) (Config, error) {
	var c Config
	md, err := toml.DecodeFile(path, &c)
	if err != nil {
		return Config{}, fmt.Errorf("config %s: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, k := range undecoded {
			keys[i] = k.String()
		}
		return Config{}, fmt.Errorf("config %s: unknown keys %s", path, strings.Join(keys, ", "))
	}
	if c.Label == "" {
		c.Label = DefaultLabel
	}
	var missing []string
	if c.KeyRef == "" {
		missing = append(missing, "key_ref")
	}
	if c.AccountID == 0 {
		missing = append(missing, "account_id")
	}
	if len(c.Payees) == 0 {
		missing = append(missing, "payees")
	}
	if c.Categories.Ride == 0 {
		missing = append(missing, "categories.ride")
	}
	if c.Categories.Delivery == 0 {
		missing = append(missing, "categories.delivery")
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("config %s: missing %s", path, strings.Join(missing, ", "))
	}
	return c, nil
}

// Policy is the categorization policy the config describes.
func (c Config) Policy() categorize.Policy {
	return categorize.Policy{
		Ride:     ledger.CategoryID(c.Categories.Ride),
		Delivery: ledger.CategoryID(c.Categories.Delivery),
		Label:    c.Label,
	}
}

// Paths returns the config file and journal directory under home.
func Paths(home string) (configFile, journalDir string) {
	return filepath.Join(home, ".config", "pocketsmith-categorizer", "config.toml"),
		filepath.Join(home, ".local", "state", "pocketsmith-categorizer", "journal")
}

// ReadKey returns the secret at ref through the 1Password CLI op. A failure reports op's
// stderr, never its stdout, which may hold the secret.
func ReadKey(ctx context.Context, op, ref string) (string, error) {
	out, err := exec.CommandContext(ctx, op, "read", ref).Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			if msg := strings.TrimSpace(string(exit.Stderr)); msg != "" {
				return "", fmt.Errorf("op read %s: %w: %s", ref, err, msg)
			}
		}
		return "", fmt.Errorf("op read %s: %w", ref, err)
	}
	key := strings.TrimSpace(string(out))
	if key == "" {
		return "", errors.New("op read " + ref + ": empty secret")
	}
	return key, nil
}

// RulesPath is the private rules file under home.
func RulesPath(home string) string {
	return filepath.Join(home, ".config", "pocketsmith-categorizer", "rules.toml")
}

// MemoPath is the record of suggestions and proposals already shown, under home.
func MemoPath(home string) string {
	return filepath.Join(home, ".local", "state", "pocketsmith-categorizer", "shown.json")
}
