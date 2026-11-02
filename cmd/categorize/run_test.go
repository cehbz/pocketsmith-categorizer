package main

import (
	"testing"

	"github.com/cehbz/pocketsmith-categorizer/attention"
	"github.com/cehbz/pocketsmith-categorizer/categorize"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

func TestFlagsKeepTheRulesSource(t *testing.T) {
	owned := ledger.State{Category: 5, Labels: []string{"auto-rule"}}
	user := ledger.State{Category: 6}
	flags := []categorize.Change{
		{Charge: 1, Before: owned, After: ledger.State{Category: 5, NeedsReview: true, Labels: []string{"auto-rule", attention.Large}}},
		{Charge: 2, Before: user, After: ledger.State{Category: 6, NeedsReview: true, Labels: []string{attention.Large}}},
		{Charge: 3, Before: owned, After: ledger.State{Category: 5, NeedsReview: true, Labels: []string{"auto-rule", attention.Large}}},
	}
	last := map[int64]categorize.Change{
		1: {Charge: 1, After: owned, Source: "rule coffee"},
		2: {Charge: 2, After: owned, Source: "rule other"},
	}
	inheritSources(flags, last)
	if flags[0].Source != "rule coffee" || flags[1].Source != "" || flags[2].Source != "" {
		t.Errorf("sources = %q, %q, %q", flags[0].Source, flags[1].Source, flags[2].Source)
	}
}
