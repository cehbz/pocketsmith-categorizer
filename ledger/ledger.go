// Package ledger models the card charges in PocketSmith.
package ledger

import (
	"slices"
	"strings"
	"time"
)

// CategoryID identifies a PocketSmith category; zero is uncategorized.
type CategoryID int64

// State is the part of a charge the categorizer reads and sets.
type State struct {
	Category    CategoryID `json:"category"`
	NeedsReview bool       `json:"needs_review"`
	Labels      []string   `json:"labels"`
}

// HasLabel reports whether the state carries label.
func (s State) HasLabel(label string) bool { return slices.Contains(s.Labels, label) }

// Equal reports whether s and o match, ignoring label order.
func (s State) Equal(o State) bool {
	if s.Category != o.Category || s.NeedsReview != o.NeedsReview || len(s.Labels) != len(o.Labels) {
		return false
	}
	a, b := slices.Clone(s.Labels), slices.Clone(o.Labels)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

// Charge is one card transaction in the ledger.
type Charge struct {
	ID    int64
	USD   float64   // amount charged; a credit is negative
	Date  time.Time // ledger date, midnight UTC
	Payee string
	State State
}

// Account is the transaction account a transaction belongs to.
type Account struct {
	ID   int64
	Name string
}

// Transaction is one transaction on any account.
type Transaction struct {
	ID      int64
	Date    time.Time // ledger date, midnight UTC
	Amount  float64   // a debit is negative
	Payee   string
	Account Account
	State   State
}

// Category is a PocketSmith category.
type Category struct {
	ID    CategoryID
	Title string
}

// IsCategorizerLabel reports whether label marks a category the categorizer set.
func IsCategorizerLabel(label string) bool {
	return strings.HasPrefix(label, "auto-") || label == "grab-auto"
}

// Owned reports whether the categorizer owns the state's category.
func (s State) Owned() bool { return slices.ContainsFunc(s.Labels, IsCategorizerLabel) }

// UserLabels returns the state's labels that aren't the categorizer's.
func (s State) UserLabels() []string {
	var out []string
	for _, l := range s.Labels {
		if !IsCategorizerLabel(l) {
			out = append(out, l)
		}
	}
	return out
}

// UserDecided reports whether the user categorized t: reviewed, categorized, not the categorizer's.
func (t Transaction) UserDecided() bool {
	return !t.State.NeedsReview && !t.State.Owned() && t.State.Category != 0
}
