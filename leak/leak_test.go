package leak

import (
	"slices"
	"testing"
)

func TestInFindsAPayeesUnusualWordsInAnyCase(t *testing.T) {
	p := NewPrivate([]string{"ZELLE TO BOB QUIMBLE", "STAYS * HQ7XK2PLM"}, []string{"stays"})
	got := p.In("a test of zelle to bob quimble, and code hq7xk2plm in a message", "")
	if want := []string{"HQ7XK2PLM", "QUIMBLE", "ZELLE"}; !slices.Equal(got, want) {
		t.Errorf("In = %v, want %v", got, want)
	}
}

func TestInIgnoresOrdinaryWordsAndTheirInflections(t *testing.T) {
	p := NewPrivate([]string{"THE RESTAURANTS BOOKED CHARGING", "ATM FEE REBATE"}, []string{"the", "restaurant", "book", "charge", "fee", "rebate"})
	for _, text := range []string{"THE RESTAURANTS", "BOOKED", "ATM FEE REBATE"} {
		if got := p.In(text, ""); len(got) != 0 {
			t.Errorf("In(%q) = %v, want none", text, got)
		}
	}
}

func TestInLeavesAWordTheRepositoryAlreadyUses(t *testing.T) {
	p := NewPrivate([]string{"QUIMBLE ROAD"}, nil)
	if got := p.In("quimble", "see the Quimble layer"); len(got) != 0 {
		t.Errorf("In = %v, want none", got)
	}
}

func TestInSkipsShortWordsAndShortNumbers(t *testing.T) {
	p := NewPrivate([]string{"BOB 2026 123456 QUIMBLE7 12345678"}, nil)
	got := p.In("BOB 2026 123456 QUIMBLE7 12345678", "")
	if want := []string{"12345678", "QUIMBLE7"}; !slices.Equal(got, want) {
		t.Errorf("In = %v, want %v", got, want)
	}
}

func TestInReadsWordsInOtherScripts(t *testing.T) {
	p := NewPrivate([]string{"ร้านข้าวมันไก่"}, nil)
	if got := p.In("fixture ร้านข้าวมันไก่ here", ""); len(got) != 1 {
		t.Errorf("In = %v, want the one word", got)
	}
}
