package rules

import (
	"testing"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/categorize"
	"github.com/cehbz/pocketsmith-categorizer/decide"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

var reviewTitles = map[ledger.CategoryID]string{3: "Phone", 4: "Cash", 5: "Food"}

// decided is charge id on d, which rule r filed in its category and state after, and which now
// has state now.
func decided(r Rule, id int64, d string, after, now ledger.State) (ledger.Transaction, categorize.Change) {
	date, _ := time.Parse(time.DateOnly, d)
	t := ledger.Transaction{ID: id, Date: date, Amount: -5, Payee: "ACME", Account: ledger.Account{Name: "Card"}, State: now}
	return t, categorize.Change{Charge: id, Before: ledger.State{NeedsReview: true}, After: after, Source: r.Key()}
}

func reviewed(rs Rules, pairs ...any) Revision {
	var txs []ledger.Transaction
	last := map[int64]categorize.Change{}
	for i := 0; i < len(pairs); i += 2 {
		t, c := pairs[i].(ledger.Transaction), pairs[i+1].(categorize.Change)
		txs = append(txs, t)
		last[t.ID] = c
	}
	return rs.Review(txs, last, reviewTitles)
}

func filed(cat ledger.CategoryID, review bool) ledger.State {
	return ledger.State{Category: cat, NeedsReview: review, Labels: []string{Label}}
}

func TestAnOverrideAmendsItsRuleFromTheChargesDate(t *testing.T) {
	rs := mustLoad(t, "[[rule]]\npayee = \"(?i)acme\"\ncategory = \"Phone\"\n")
	v := reviewed(rs, pair(decided(rs[0], 1, "2026-10-05", filed(3, false), filed(5, false)))...)
	now := Rule{Payee: "(?i)acme", Category: "Food", State: Tentative, From: time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)}
	if len(v.Edits) != 1 || v.Edits[0].Index != 0 || v.Edits[0].Rule.Category != now.Category || v.Edits[0].Rule.State != now.State || !v.Edits[0].Rule.From.Equal(now.From) {
		t.Fatalf("edits = %+v; want rule 0 in Food, tentative, from 2026-10-05", v.Edits)
	}
	if len(v.Amended) != 1 || v.Amended[0].Was.Category != "Phone" || v.Amended[0].Now.Category != "Food" || v.Amended[0].Charge.ID != 1 {
		t.Errorf("amended = %+v", v.Amended)
	}
}

func TestAnOverrideWithApplyAllAmendsItsRuleForAllCharges(t *testing.T) {
	rs := mustLoad(t, "[[rule]]\npayee = \"(?i)acme\"\ncategory = \"Phone\"\nfrom = 2026-01-01\n")
	now := filed(5, false)
	now.Labels = append(now.Labels, decide.ApplyAll)
	v := reviewed(rs, pair(decided(rs[0], 1, "2026-10-05", filed(3, false), now))...)
	if len(v.Edits) != 1 || v.Edits[0].Rule.Category != "Food" || !v.Edits[0].Rule.From.IsZero() || v.Edits[0].Rule.State != Tentative {
		t.Errorf("edits = %+v; want rule 0 in Food, tentative, with no start date", v.Edits)
	}
}

func TestOverridesThatAmendNothing(t *testing.T) {
	rs := mustLoad(t, "[[rule]]\npayee = \"(?i)acme\"\ncategory = \"Phone\"\n")
	edited := Rule{Payee: "(?i)acme", Category: "Cash"} // the rule as it was when it decided; the user has since edited it
	for name, v := range map[string]Revision{
		"rule edited since":  reviewed(rs, pair(decided(edited, 1, "2026-10-05", filed(4, false), filed(5, false)))...),
		"to uncategorized":   reviewed(rs, pair(decided(rs[0], 1, "2026-10-05", filed(3, false), filed(0, false)))...),
		"category unchanged": reviewed(rs, pair(decided(rs[0], 1, "2026-10-05", filed(3, false), filed(3, true)))...),
	} {
		if len(v.Edits)+len(v.Amended) != 0 {
			t.Errorf("%s: revision = %+v; want none", name, v)
		}
	}
}

func TestTheLatestOverrideOfARuleDecides(t *testing.T) {
	rs := mustLoad(t, "[[rule]]\npayee = \"(?i)acme\"\ncategory = \"Phone\"\n")
	a := pair(decided(rs[0], 1, "2026-10-09", filed(3, false), filed(4, false)))
	b := pair(decided(rs[0], 2, "2026-10-05", filed(3, false), filed(5, false)))
	v := reviewed(rs, append(b, a...)...)
	if len(v.Edits) != 1 || v.Edits[0].Rule.Category != "Cash" || v.Amended[0].Charge.ID != 1 {
		t.Errorf("revision = %+v; want rule 0 in Cash by charge 1", v)
	}
}

func TestAnAcceptedTentativeDecisionConfirmsItsRule(t *testing.T) {
	rs := mustLoad(t, "[[rule]]\npayee = \"(?i)acme\"\ncategory = \"Phone\"\nstate = \"tentative\"\n")
	v := reviewed(rs, pair(decided(rs[0], 1, "2026-10-05", filed(3, true), filed(3, false)))...)
	if len(v.Edits) != 1 || v.Edits[0].Rule.State != Confirmed || v.Edits[0].Rule.Category != "Phone" || len(v.Amended) != 0 {
		t.Errorf("revision = %+v; want rule 0 confirmed in Phone", v)
	}
	confirmed := mustLoad(t, "[[rule]]\npayee = \"(?i)acme\"\ncategory = \"Phone\"\n")
	if v := reviewed(confirmed, pair(decided(confirmed[0], 1, "2026-10-05", filed(3, true), filed(3, false)))...); len(v.Edits) != 0 {
		t.Errorf("a confirmed rule's acceptance edited %+v", v.Edits)
	}
}

func TestAnOverrideWinsOverAnAcceptance(t *testing.T) {
	rs := mustLoad(t, "[[rule]]\npayee = \"(?i)acme\"\ncategory = \"Phone\"\nstate = \"tentative\"\n")
	accepted := pair(decided(rs[0], 1, "2026-10-05", filed(3, true), filed(3, false)))
	overridden := pair(decided(rs[0], 2, "2026-10-04", filed(3, true), filed(5, true)))
	v := reviewed(rs, append(accepted, overridden...)...)
	if len(v.Edits) != 1 || v.Edits[0].Rule.Category != "Food" || v.Edits[0].Rule.State != Tentative {
		t.Errorf("revision = %+v; want rule 0 amended to Food, tentative", v)
	}
}

func TestOfTwoRulesWithOneKeyTheFirstIsAmended(t *testing.T) {
	rs := mustLoad(t, "[[rule]]\npayee = \"(?i)acme\"\ncategory = \"Phone\"\n[[rule]]\npayee = \"(?i)acme\"\ncategory = \"Phone\"\n")
	v := reviewed(rs, pair(decided(rs[0], 1, "2026-10-05", filed(3, false), filed(5, false)))...)
	if len(v.Edits) != 1 || v.Edits[0].Index != 0 {
		t.Errorf("edits = %+v; want rule 0's", v.Edits)
	}
}

func pair(t ledger.Transaction, c categorize.Change) []any { return []any{t, c} }

func TestARejectedRuleStaysRejected(t *testing.T) {
	rs := mustLoad(t, "[[rule]]\npayee = \"(?i)acme\"\ncategory = \"Phone\"\nstate = \"rejected\"\n")
	// charges the rule decided before the user rejected it
	overridden := pair(decided(rs[0], 1, "2026-10-05", filed(3, false), filed(5, false)))
	accepted := pair(decided(rs[0], 2, "2026-10-06", filed(3, true), filed(3, false)))
	if v := reviewed(rs, append(overridden, accepted...)...); len(v.Edits)+len(v.Amended) != 0 {
		t.Errorf("revision = %+v; want none", v)
	}
}
