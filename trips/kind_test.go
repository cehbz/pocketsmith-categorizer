package trips

import (
	"slices"
	"testing"

	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

var kinds = Kinds{
	Codes: DefaultCodes(),
	Big:   map[Kind]ledger.CategoryID{Transport: 101, Lodging: 102, Food: 103, Discretionary: 104, Cash: 105},
	Home:  map[ledger.CategoryID]Kind{5: Food, 6: Transport, 8: Cash},
}

func TestEachDefaultRangeMapsToItsKind(t *testing.T) {
	for code, want := range map[string]Kind{
		"3000": Transport, "3299": Transport, "4111": Transport, "4112": Transport, "4121": Transport, "4131": Transport, "4411": Transport, "4511": Transport,
		"3501": Lodging, "3999": Lodging, "4722": Lodging, "6513": Lodging, "7011": Lodging,
		"5411": Food, "5422": Food, "5441": Food, "5451": Food, "5462": Food, "5499": Food, "5811": Food, "5814": Food,
		"3300": Discretionary, "5999": Discretionary,
	} {
		if got, ok := DefaultCodes().Kind(code); !ok || got != want {
			t.Errorf("Kind(%s) = %v, %v, want %v", code, got, ok, want)
		}
	}
	if _, ok := DefaultCodes().Kind(""); ok {
		t.Error("an empty code has a kind")
	}
}

func TestBookingCodesAreLongDistanceTravelAndLodging(t *testing.T) {
	for code, want := range map[string]bool{"3000": true, "3501": true, "4112": true, "4411": true, "4511": true, "4722": true, "6513": true, "7011": true,
		"4111": false, "4121": false, "5812": false, "": false} {
		if got := DefaultCodes().IsBooking(code); got != want {
			t.Errorf("IsBooking(%q) = %v, want %v", code, got, want)
		}
	}
}

func TestABigSubcategoryGivesItsKindAheadOfTheCode(t *testing.T) {
	if got := kinds.Of(101, "4722"); got != Transport {
		t.Errorf("Of(Big Transport, 4722) = %v, want Transport", got)
	}
}

func TestWithoutACodeTheHomeCategoryGivesTheKind(t *testing.T) {
	for _, c := range []struct {
		category ledger.CategoryID
		code     string
		want     Kind
	}{{5, "", Food}, {6, "", Transport}, {8, "", Cash}, {9, "", Discretionary}, {5, "7011", Lodging}} {
		if got := kinds.Of(c.category, c.code); got != c.want {
			t.Errorf("Of(%d, %q) = %v, want %v", c.category, c.code, got, c.want)
		}
	}
}

func TestParseRangesReadsCodesAndRanges(t *testing.T) {
	got, err := ParseRanges([]string{"4111", "3000-3299"})
	if err != nil || !slices.Equal(got, []Range{{4111, 4111}, {3000, 3299}}) {
		t.Errorf("ParseRanges = %v, %v", got, err)
	}
	for _, bad := range []string{"x", "3299-3000", "1-"} {
		if _, err := ParseRanges([]string{bad}); err == nil {
			t.Errorf("ParseRanges(%q) succeeded", bad)
		}
	}
}
