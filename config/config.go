// Package config loads the categorizer's local configuration and its secrets.
package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/cehbz/pocketsmith-categorizer/attention"
	"github.com/cehbz/pocketsmith-categorizer/categorize"
	"github.com/cehbz/pocketsmith-categorizer/internal/day"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
	"github.com/cehbz/pocketsmith-categorizer/trips"
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
	Attention  Attention  `toml:"attention"`
	Income     Income     `toml:"income"`
	Trips      Trips      `toml:"trips"`
	Plaid      Plaid      `toml:"plaid"`
	Calendar   Calendar   `toml:"calendar"`
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

// ReadSecret returns the secret at ref: through the 1Password CLI op for an op:// reference,
// otherwise from the file ref names, which only its owner may read. A failure reports op's
// stderr, never its stdout, which may hold the secret.
func ReadSecret(ctx context.Context, op, ref string) (string, error) {
	if !strings.HasPrefix(ref, "op://") {
		return readFile(ref)
	}
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

// readFile returns the secret in an owner-only regular file.
func readFile(path string) (string, error) {
	b, err := ReadOwnerOnly(path)
	if err != nil {
		return "", fmt.Errorf("secret: %w", err)
	}
	secret := strings.TrimSpace(string(b))
	if secret == "" {
		return "", errors.New("secret " + path + ": empty")
	}
	return secret, nil
}

// ReadOwnerOnly reads the regular file at path, refusing one its group or others can read or write.
func ReadOwnerOnly(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s: mode %v, want a regular file only its owner can read", path, info.Mode())
	}
	return os.ReadFile(path)
}

// RulesPath is the private rules file under home.
func RulesPath(home string) string {
	return filepath.Join(home, ".config", "pocketsmith-categorizer", "rules.toml")
}

// MemoPath is the record of suggestions and proposals already shown, under home.
func MemoPath(home string) string {
	return filepath.Join(home, ".local", "state", "pocketsmith-categorizer", "shown.json")
}

// Attention is when a transaction needs the user.
type Attention struct {
	Start string  `toml:"start"` // first date judged whether or not it needs review, YYYY-MM-DD
	Large float64 `toml:"large"` // an amount at least this large always needs the user
	SD    float64 `toml:"sd"`    // standard deviations above a payee's usual amount that need the user
}

// Income is the monthly income check, naming PocketSmith categories by title.
type Income struct {
	Account   string  `toml:"account"`
	Category  string  `toml:"category"`
	Bill      string  `toml:"bill"`
	Rent      string  `toml:"rent"`
	Cash      string  `toml:"cash"`
	Target    float64 `toml:"target"`
	Tolerance float64 `toml:"tolerance"`
}

// Settings resolves the attention settings, naming categories by title against cats.
func (c Config) Settings(cats []ledger.Category) (attention.Settings, error) {
	var missing []string
	need := func(ok bool, name string) {
		if !ok {
			missing = append(missing, name)
		}
	}
	a, in := c.Attention, c.Income
	need(a.Start != "", "attention.start")
	need(a.Large > 0, "attention.large")
	need(a.SD > 0, "attention.sd")
	need(in.Account != "", "income.account")
	need(in.Category != "", "income.category")
	need(in.Bill != "", "income.bill")
	need(in.Rent != "", "income.rent")
	need(in.Cash != "", "income.cash")
	need(in.Target > 0, "income.target")
	need(in.Tolerance > 0, "income.tolerance")
	if len(missing) > 0 {
		return attention.Settings{}, fmt.Errorf("config: missing %s", strings.Join(missing, ", "))
	}
	start, err := day.Parse(a.Start)
	if err != nil {
		return attention.Settings{}, fmt.Errorf("config: attention.start: %w", err)
	}
	ids := map[string]ledger.CategoryID{}
	for _, cat := range cats {
		ids[cat.Title] = cat.ID
	}
	var resolved [4]ledger.CategoryID
	for i, title := range []string{in.Category, in.Bill, in.Rent, in.Cash} {
		id, ok := ids[title]
		if !ok {
			return attention.Settings{}, fmt.Errorf("config: income: no category %q", title)
		}
		resolved[i] = id
	}
	cash := []ledger.CategoryID{resolved[3]}
	if title := c.Trips.Big.Cash; title != "" {
		id, ok := ids[title]
		if !ok {
			return attention.Settings{}, fmt.Errorf("config: trips.big.cash: no category %q", title)
		}
		cash = append(cash, id)
	}
	return attention.Settings{
		Start: start,
		Size:  attention.Size{Absolute: a.Large, SD: a.SD},
		Income: attention.Income{Account: in.Account, Category: resolved[0], Bill: resolved[1], Rent: resolved[2],
			Cash: cash, Target: in.Target, Tolerance: in.Tolerance},
	}, nil
}

// JudgedPath is the record of transactions judged and income cycles checked, under home.
func JudgedPath(home string) string {
	return filepath.Join(home, ".local", "state", "pocketsmith-categorizer", "judged.json")
}

// Trips is the trips layer's configuration, naming PocketSmith categories by title.
type Trips struct {
	Places       []string   `toml:"places"`        // home places, as named in merchant names, payees and event locations
	HomeSince    string     `toml:"home_since"`    // the home start, YYYY-MM-DD: nothing before it is settled
	HomeAirports []string   `toml:"home_airports"` // home's airports, as IATA codes
	Everywhere   []string   `toml:"everywhere"`    // merchants always everywhere merchants (Grab), as word prefixes of the merchant
	Big          TripTitles `toml:"big"`           // each kind's Big subcategory
	Home         HomeKinds  `toml:"home"`          // home categories with a kind, for charges without a code
	Codes        *Codes     `toml:"codes"`         // merchant category codes by kind; the defaults when absent
}

// TripTitles names each kind's Big subcategory.
type TripTitles struct {
	Transport     string `toml:"transport"`
	Lodging       string `toml:"lodging"`
	Food          string `toml:"food"`
	Discretionary string `toml:"discretionary"`
	Cash          string `toml:"cash"`
}

// HomeKinds names the home categories of each kind.
type HomeKinds struct {
	Transport     []string `toml:"transport"`
	Lodging       []string `toml:"lodging"`
	Food          []string `toml:"food"`
	Discretionary []string `toml:"discretionary"`
	Cash          []string `toml:"cash"`
}

// Codes are merchant category codes and ranges ("3000-3299") by kind, and the codes of bookings.
type Codes struct {
	Transport []string `toml:"transport"`
	Lodging   []string `toml:"lodging"`
	Food      []string `toml:"food"`
	Booking   []string `toml:"booking"`
}

// Plaid is where the card's records come from: references to its secrets, read as key_ref
// is, and each card account's mask with the PocketSmith account it is.
type Plaid struct {
	ClientIDRef    string            `toml:"client_id_ref"`
	SecretRef      string            `toml:"secret_ref"`
	AccessTokenRef string            `toml:"access_token_ref"`
	Accounts       map[string]string `toml:"accounts"`
}

// Calendar is the Google calendar the trips layer reads: its id, references to the OAuth
// client's id and secret, read as key_ref is, and the token file categorize auth writes and
// runs rewrite when they refresh it.
type Calendar struct {
	ID              string `toml:"id"`
	ClientIDRef     string `toml:"client_id_ref"`
	ClientSecretRef string `toml:"client_secret_ref"`
	TokenRef        string `toml:"token_ref"`
}

// Missing names the calendar settings left out.
func (c Calendar) Missing() []string {
	var out []string
	for _, f := range []struct{ value, name string }{
		{c.ID, "calendar.id"}, {c.ClientIDRef, "calendar.client_id_ref"}, {c.ClientSecretRef, "calendar.client_secret_ref"}, {c.TokenRef, "calendar.token_ref"},
	} {
		if f.value == "" {
			out = append(out, f.name)
		}
	}
	return out
}

// TripSettings resolves the trips settings, naming categories by title against cats; start
// is the attention start, and cash is withdrawn from the income check's account.
func (c Config) TripSettings(cats []ledger.Category, start time.Time) (trips.Settings, error) {
	var missing []string
	need := func(ok bool, name string) {
		if !ok {
			missing = append(missing, name)
		}
	}
	tr, pl := c.Trips, c.Plaid
	need(len(tr.Places) > 0, "trips.places")
	need(tr.HomeSince != "", "trips.home_since")
	need(len(tr.HomeAirports) > 0, "trips.home_airports")
	need(tr.Big.Transport != "", "trips.big.transport")
	need(tr.Big.Lodging != "", "trips.big.lodging")
	need(tr.Big.Food != "", "trips.big.food")
	need(tr.Big.Discretionary != "", "trips.big.discretionary")
	need(tr.Big.Cash != "", "trips.big.cash")
	need(pl.ClientIDRef != "", "plaid.client_id_ref")
	need(pl.SecretRef != "", "plaid.secret_ref")
	need(pl.AccessTokenRef != "", "plaid.access_token_ref")
	need(len(pl.Accounts) > 0, "plaid.accounts")
	missing = append(missing, c.Calendar.Missing()...)
	if len(missing) > 0 {
		return trips.Settings{}, fmt.Errorf("config: missing %s", strings.Join(missing, ", "))
	}
	since, err := day.Parse(tr.HomeSince)
	if err != nil {
		return trips.Settings{}, fmt.Errorf("config: trips.home_since: %w", err)
	}
	ids := map[string]ledger.CategoryID{}
	for _, cat := range cats {
		ids[cat.Title] = cat.ID
	}
	kinds := trips.Kinds{Codes: trips.DefaultCodes(), Big: map[trips.Kind]ledger.CategoryID{}, Home: map[ledger.CategoryID]trips.Kind{}}
	for _, k := range []struct {
		kind  trips.Kind
		big   string
		homes []string
	}{
		{trips.Transport, tr.Big.Transport, tr.Home.Transport},
		{trips.Lodging, tr.Big.Lodging, tr.Home.Lodging},
		{trips.Food, tr.Big.Food, tr.Home.Food},
		{trips.Discretionary, tr.Big.Discretionary, tr.Home.Discretionary},
		{trips.Cash, tr.Big.Cash, tr.Home.Cash},
	} {
		for i, title := range append([]string{k.big}, k.homes...) {
			id, ok := ids[title]
			if !ok {
				return trips.Settings{}, fmt.Errorf("config: trips: no category %q", title)
			}
			if i == 0 {
				kinds.Big[k.kind] = id
			} else {
				kinds.Home[id] = k.kind
			}
		}
	}
	if cs := tr.Codes; cs != nil {
		for _, f := range []struct {
			dst *[]trips.Range
			src []string
		}{{&kinds.Codes.Transport, cs.Transport}, {&kinds.Codes.Lodging, cs.Lodging}, {&kinds.Codes.Food, cs.Food}, {&kinds.Codes.Booking, cs.Booking}} {
			if f.src == nil {
				continue // omitted: the default stands
			}
			rs, err := trips.ParseRanges(f.src)
			if err != nil {
				return trips.Settings{}, fmt.Errorf("config: trips.codes: %w", err)
			}
			*f.dst = rs
		}
	}
	return trips.Settings{Kinds: kinds, Home: trips.Home{Places: tr.Places, Airports: tr.HomeAirports, Since: since}, Everywhere: tr.Everywhere, Start: start, CashAccount: c.Income.Account}, nil
}

// TripsPath is the trip list the user edits, under home.
func TripsPath(home string) string {
	return filepath.Join(home, ".config", "pocketsmith-categorizer", "trips.toml")
}

// WrittenTripsPath is the record of every trip the categorizer wrote to the list, under home.
func WrittenTripsPath(home string) string {
	return filepath.Join(home, ".local", "state", "pocketsmith-categorizer", "trips-written.toml")
}

// PlacedPath is the record of the trip placements made, under home.
func PlacedPath(home string) string {
	return filepath.Join(home, ".local", "state", "pocketsmith-categorizer", "placed.json")
}
