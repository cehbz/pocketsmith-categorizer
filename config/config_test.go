package config

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/evidence"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
	"github.com/cehbz/pocketsmith-categorizer/trips"
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

[merchants.grab]
payees = ["WWW.GRAB.COM", "GRABTAXI"]
start = "2026-10-26"

[merchants.grab.categories]
ride = "Rides"
food = "Delivery"
`

func TestLoadReadsEveryField(t *testing.T) {
	c, err := Load(write(t, "config.toml", full, 0o600))
	if err != nil {
		t.Fatal(err)
	}
	grab := Merchant{Payees: []string{"WWW.GRAB.COM", "GRABTAXI"}, Start: "2026-10-26", Categories: map[string]string{"ride": "Rides", "food": "Delivery"}}
	if c.KeyRef != "op://Vault/Item/field" || c.AccountID != 42 || !reflect.DeepEqual(c.Merchants, map[string]Merchant{"grab": grab}) {
		t.Errorf("config = %+v", c)
	}
}

func TestLoadRejectsGrabsSettingsOutsideItsMerchantTable(t *testing.T) {
	old := "key_ref = \"op://Vault/Item/field\"\naccount_id = 42\npayees = [\"GRAB\"]\nlabel = \"grab-auto\"\n[categories]\nride = 7\n"
	path := write(t, "config.toml", old, 0o600)
	_, err := Load(path)
	want := "config " + path + ": unknown keys payees, label, categories, categories.ride (Grab's payees and categories are in [merchants.grab])"
	if err == nil || err.Error() != want {
		t.Errorf("Load error = %v; want %s", err, want)
	}
}

// sells is a merchant selling rides and food, before the config's table.
var sells = evidence.Merchant{Name: "grab", Label: "grab-auto", Kinds: []evidence.Kind{evidence.Ride, evidence.Food}}

var merchantCats = []ledger.Category{{ID: 21, Title: "Rides"}, {ID: 22, Title: "Delivery"}}

func TestMerchantResolvesItsTable(t *testing.T) {
	c, err := Load(write(t, "config.toml", full+"\n[merchants.lazada]\nstay_home = true\n", 0o600))
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.Merchant(sells, merchantCats)
	if err != nil {
		t.Fatal(err)
	}
	want := sells
	want.Payees, want.Start = []string{"WWW.GRAB.COM", "GRABTAXI"}, time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	want.Categories = map[evidence.Kind]ledger.CategoryID{evidence.Ride: 21, evidence.Food: 22}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Merchant = %+v\nwant %+v", got, want)
	}
	if !c.Merchants["lazada"].StayHome {
		t.Errorf("lazada's table = %+v; want it to stay home", c.Merchants["lazada"])
	}
}

func TestMerchantNamesWhatIsMissing(t *testing.T) {
	c, err := Load(write(t, "config.toml", "key_ref = \"x\"\naccount_id = 1\n[merchants.grab.categories]\nride = \"Rides\"\n", 0o600))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Merchant(sells, merchantCats)
	if want := "config: missing merchants.grab.payees, merchants.grab.start, merchants.grab.categories.food"; err == nil || err.Error() != want {
		t.Errorf("Merchant error = %v; want %s", err, want)
	}
	lazada := evidence.Merchant{Name: "lazada"}
	if _, err := c.Merchant(lazada, merchantCats); err == nil || err.Error() != "config: missing merchants.lazada" {
		t.Errorf("Merchant error = %v; want config: missing merchants.lazada", err)
	}
}

func TestMerchantRejectsAnUnknownKindOrTitleAndABadStart(t *testing.T) {
	for _, c := range []struct{ config, want string }{
		{strings.Replace(full, "food = \"Delivery\"", "food = \"Delivery\"\ntaxi = \"Rides\"", 1), "config: merchants.grab.categories.taxi: grab sells no taxi"},
		{strings.Replace(full, "food = \"Delivery\"", "food = \"Takeaway\"", 1), `config: merchants.grab.categories.food: no category "Takeaway"`},
		{strings.Replace(full, "2026-10-26", "2026-13-01", 1), "config: merchants.grab.start: "},
	} {
		cfg, err := Load(write(t, "config.toml", c.config, 0o600))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := cfg.Merchant(sells, merchantCats); err == nil || !strings.HasPrefix(err.Error(), c.want) {
			t.Errorf("Merchant error = %v; want one starting %s", err, c.want)
		}
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
	_, err := Load(write(t, "config.toml", `[attention]`, 0o600))
	if err == nil {
		t.Fatal("Load accepted an empty config")
	}
	for _, f := range []string{"key_ref", "account_id"} {
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
	path := write(t, "config.toml", "lable = \"x\"\n"+full+"[merchants.lazada]\npayee = [\"x\"]\n", 0o600)
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load accepted unknown keys")
	}
	if want := "config " + path + ": unknown keys lable, merchants.lazada.payee"; err.Error() != want {
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
	if in.Account != "Checking" || in.Category != 1 || in.Bill != 2 || in.Rent != 3 || !slices.Equal(in.Cash, []ledger.CategoryID{4}) || in.Target != 3000 || in.Tolerance != 1500 {
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

const tripsConfig = `
[trips]
places = ["HOMETOWN"]
home_since = "2025-01-01"
home_airports = ["HMT"]
everywhere = ["GRAB"]
stay_home = ["Utilities", "Doctor"]

[trips.big]
transport = "Trip Transport"
lodging = "Trip Lodging"
food = "Trip Food"
discretionary = "Trip Other"
cash = "Trip Cash"

[trips.home]
food = ["Restaurants"]
transport = ["Taxi"]
cash = ["ATM"]

[plaid]
client_id_ref = "/s/id"
secret_ref = "/s/secret"
access_token_ref = "/s/token"

[plaid.accounts]
"1111" = "Card"

[calendar]
id = "someone@example.com"
client_id_ref = "/s/google-id"
client_secret_ref = "/s/google-secret"
token_ref = "/s/calendar-token.json"
`

var tripCats = []ledger.Category{{ID: 11, Title: "Trip Transport"}, {ID: 12, Title: "Trip Lodging"}, {ID: 13, Title: "Trip Food"},
	{ID: 14, Title: "Trip Other"}, {ID: 15, Title: "Restaurants"}, {ID: 16, Title: "Taxi"}, {ID: 17, Title: "Trip Cash"}, {ID: 18, Title: "ATM"},
	{ID: 19, Title: "Utilities"}, {ID: 20, Title: "Doctor"}}

var tripStart = time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)

func TestTripSettingsResolvesTitlesAndDefaultsTheCodes(t *testing.T) {
	c, err := Load(write(t, "config.toml", full+attentionConfig+tripsConfig, 0o600))
	if err != nil {
		t.Fatal(err)
	}
	s, err := c.TripSettings(tripCats, tripStart)
	if err != nil {
		t.Fatal(err)
	}
	k := s.Kinds
	if k.Big[trips.Transport] != 11 || k.Big[trips.Lodging] != 12 || k.Big[trips.Food] != 13 || k.Big[trips.Discretionary] != 14 ||
		k.Big[trips.Cash] != 17 || k.Home[18] != trips.Cash ||
		k.Home[15] != trips.Food || k.Home[16] != trips.Transport || !reflect.DeepEqual(k.Codes, trips.DefaultCodes()) {
		t.Errorf("kinds = %+v", k)
	}
	if !slices.Equal(s.StayHome, []ledger.CategoryID{19, 20}) {
		t.Errorf("stay home = %v, want [19 20]", s.StayHome)
	}
	if !slices.Equal(s.Home.Places, []string{"HOMETOWN"}) || !slices.Equal(s.Everywhere, []string{"GRAB"}) || !s.Start.Equal(tripStart) || s.CashAccount != "Checking" {
		t.Errorf("settings = %+v", s)
	}
	if p := c.Plaid; p.ClientIDRef != "/s/id" || p.SecretRef != "/s/secret" || p.AccessTokenRef != "/s/token" || p.Accounts["1111"] != "Card" {
		t.Errorf("plaid = %+v", p)
	}
	if h := s.Home; !h.Since.Equal(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)) || !slices.Equal(h.Airports, []string{"HMT"}) {
		t.Errorf("home = %+v", h)
	}
	if want := (Calendar{ID: "someone@example.com", ClientIDRef: "/s/google-id", ClientSecretRef: "/s/google-secret", TokenRef: "/s/calendar-token.json"}); c.Calendar != want {
		t.Errorf("calendar = %+v, want %+v", c.Calendar, want)
	}
}

func TestTripSettingsReadsTheCodes(t *testing.T) {
	codes := "\n[trips.codes]\ntransport = [\"4111\"]\nlodging = [\"7011\"]\nfood = [\"5812\"]\nbooking = [\"3000-3299\"]\n"
	c, err := Load(write(t, "config.toml", full+tripsConfig+codes, 0o600))
	if err != nil {
		t.Fatal(err)
	}
	s, err := c.TripSettings(tripCats, tripStart)
	if err != nil {
		t.Fatal(err)
	}
	want := trips.Codes{Transport: []trips.Range{{Lo: 4111, Hi: 4111}}, Lodging: []trips.Range{{Lo: 7011, Hi: 7011}},
		Food: []trips.Range{{Lo: 5812, Hi: 5812}}, Booking: []trips.Range{{Lo: 3000, Hi: 3299}}}
	if !reflect.DeepEqual(s.Kinds.Codes, want) {
		t.Errorf("codes = %+v, want %+v", s.Kinds.Codes, want)
	}
}

func TestAPartialCodesTableKeepsTheDefaultsForTheKindsItOmits(t *testing.T) {
	c, err := Load(write(t, "config.toml", full+tripsConfig+"\n[trips.codes]\nfood = [\"5812\"]\n", 0o600))
	if err != nil {
		t.Fatal(err)
	}
	s, err := c.TripSettings(tripCats, tripStart)
	if err != nil {
		t.Fatal(err)
	}
	want := trips.DefaultCodes()
	want.Food = []trips.Range{{Lo: 5812, Hi: 5812}}
	if !reflect.DeepEqual(s.Kinds.Codes, want) {
		t.Errorf("codes = %+v, want the defaults with food 5812", s.Kinds.Codes)
	}
}

func TestTripSettingsNamesWhatIsMissing(t *testing.T) {
	c, err := Load(write(t, "config.toml", full, 0o600))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.TripSettings(tripCats, tripStart)
	if err == nil {
		t.Fatal("TripSettings accepted a config without them")
	}
	for _, f := range []string{"trips.places", "trips.home_since", "trips.home_airports", "trips.big.transport", "trips.big.lodging", "trips.big.food", "trips.big.discretionary", "trips.big.cash",
		"plaid.client_id_ref", "plaid.secret_ref", "plaid.access_token_ref", "plaid.accounts",
		"calendar.id", "calendar.client_id_ref", "calendar.client_secret_ref", "calendar.token_ref"} {
		if !strings.Contains(err.Error(), f) {
			t.Errorf("error %q doesn't name %s", err, f)
		}
	}
}

func TestTripSettingsRejectsUnknownTitlesBadCodesAndABadHomeStart(t *testing.T) {
	for name, content := range map[string]string{
		"unknown big":    strings.Replace(tripsConfig, `"Trip Food"`, `"Nope"`, 1),
		"unknown home":   strings.Replace(tripsConfig, `"Taxi"`, `"Nope"`, 1),
		"bad code":       tripsConfig + "\n[trips.codes]\ntransport = [\"x\"]\n",
		"bad home start": strings.Replace(tripsConfig, `"2025-01-01"`, `"2025-13-01"`, 1),
	} {
		c, err := Load(write(t, "config.toml", full+content, 0o600))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.TripSettings(tripCats, tripStart); err == nil {
			t.Errorf("%s: TripSettings succeeded", name)
		}
	}
}

func TestTripSettingsNamesAnUnknownStayHomeTitleAndNeedsNone(t *testing.T) {
	c, err := Load(write(t, "config.toml", full+strings.Replace(tripsConfig, `"Doctor"`, `"Nope"`, 1), 0o600))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.TripSettings(tripCats, tripStart); err == nil || !strings.Contains(err.Error(), "trips.stay_home") || !strings.Contains(err.Error(), "Nope") {
		t.Errorf("error = %v, want one naming trips.stay_home and Nope", err)
	}
	c, err = Load(write(t, "config.toml", full+strings.Replace(tripsConfig, "stay_home = [\"Utilities\", \"Doctor\"]\n", "", 1), 0o600))
	if err != nil {
		t.Fatal(err)
	}
	if s, err := c.TripSettings(tripCats, tripStart); err != nil || len(s.StayHome) != 0 {
		t.Errorf("without stay_home: %v, %v; want none", s.StayHome, err)
	}
}

func TestTripPathsAreUnderHome(t *testing.T) {
	if got := TripsPath("/h"); got != "/h/.config/pocketsmith-categorizer/trips.toml" {
		t.Errorf("TripsPath = %q", got)
	}
	if got := WrittenTripsPath("/h"); got != "/h/.local/state/pocketsmith-categorizer/trips-written.toml" {
		t.Errorf("WrittenTripsPath = %q", got)
	}
	if got := PlacedPath("/h"); got != "/h/.local/state/pocketsmith-categorizer/placed.json" {
		t.Errorf("PlacedPath = %q", got)
	}
}

func TestSettingsCountsBigCashAsCash(t *testing.T) {
	c, err := Load(write(t, "config.toml", full+attentionConfig+"\n[trips.big]\ncash = \"Trip Cash\"\n", 0o600))
	if err != nil {
		t.Fatal(err)
	}
	s, err := c.Settings(append(slices.Clone(cats), ledger.Category{ID: 5, Title: "Trip Cash"}))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(s.Income.Cash, []ledger.CategoryID{4, 5}) {
		t.Errorf("cash = %v, want the income check's and Big Cash", s.Income.Cash)
	}
	if _, err := c.Settings(cats); err == nil {
		t.Error("Settings accepted an unknown trips.big.cash title")
	}
}

func TestReportsDirIsUnderTheStateDirectory(t *testing.T) {
	if got := ReportsDir("/h"); got != "/h/.local/state/pocketsmith-categorizer/reports" {
		t.Errorf("ReportsDir = %q", got)
	}
}
