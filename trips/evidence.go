// Package trips finds the user's trips and puts their spending in the Big subcategories.
package trips

import (
	"cmp"
	"context"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/internal/day"
	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

// Record is a card transaction as the card's issuer reports it.
type Record struct {
	ID       string
	Account  string    // the PocketSmith account it belongs to
	Date     time.Time // the issuer's date, midnight UTC
	Amount   float64   // a charge is positive
	Name     string    // the issuer's description
	Merchant string    // the issuer's merchant name; empty when it has none
	Code     string    // the merchant category code; empty when it has none
	InStore  bool      // paid in person
}

// Card is the card issuer's records.
type Card interface {
	Records(ctx context.Context, from, to time.Time) ([]Record, error)
}

// Evidence is what a ledger transaction's record says about it.
type Evidence struct {
	Merchant string
	Code     string
	InStore  bool
}

const (
	matchBefore = 2 // days a record may be dated before its ledger transaction
	matchAfter  = 5 // days a record may be dated after it
)

// Match gives each ledger transaction on the records' accounts the evidence of the record it
// became: the same cents with the opposite sign, dated 2 days before to 5 days after it, the
// most shared payee words, then the nearest date. Each record matches at most once.
func Match(txs []ledger.Transaction, recs []Record) map[int64]Evidence {
	type key struct {
		account string
		cents   int64
	}
	recs = slices.Clone(recs)
	slices.SortStableFunc(recs, func(a, b Record) int { return a.Date.Compare(b.Date) })
	candidates := map[key][]int{}
	for i, r := range recs {
		k := key{r.Account, cents(r.Amount)}
		candidates[k] = append(candidates[k], i)
	}
	ordered := slices.Clone(txs)
	slices.SortStableFunc(ordered, func(a, b ledger.Transaction) int {
		if c := a.Date.Compare(b.Date); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})
	used := map[int]bool{}
	out := map[int64]Evidence{}
	for _, t := range ordered {
		best, bestWords, bestGap := -1, 0, 0
		for _, i := range candidates[key{t.Account.Name, -cents(t.Amount)}] {
			gap := day.Between(t.Date, recs[i].Date)
			if used[i] || gap < -matchBefore || gap > matchAfter {
				continue
			}
			w := shared(t.Payee, recs[i].Name)
			if best < 0 || w > bestWords || w == bestWords && absInt(gap) < absInt(bestGap) {
				best, bestWords, bestGap = i, w, gap
			}
		}
		if best >= 0 {
			used[best] = true
			r := recs[best]
			out[t.ID] = Evidence{Merchant: r.Merchant, Code: r.Code, InStore: r.InStore}
		}
	}
	return out
}

// shared counts the distinct words a and b have in common.
func shared(a, b string) int {
	in := map[string]bool{}
	for _, w := range words(a) {
		in[w] = true
	}
	n := 0
	for _, w := range words(b) {
		if in[w] {
			n++
			delete(in, w)
		}
	}
	return n
}

// words are the runs of letters in s, upper-cased.
func words(s string) []string {
	return strings.FieldsFunc(strings.ToUpper(s), func(r rune) bool { return r < 'A' || r > 'Z' })
}

func cents(x float64) int64 { return int64(math.Round(x * 100)) }

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
