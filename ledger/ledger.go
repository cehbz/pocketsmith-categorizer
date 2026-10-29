// Package ledger models the card charges in PocketSmith.
package ledger

import (
	"slices"
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
