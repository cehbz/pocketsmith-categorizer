package categorize

import (
	"context"
	"fmt"

	"github.com/cehbz/pocketsmith-categorizer/ledger"
)

// Ledger is the categorizer's view of PocketSmith.
type Ledger interface {
	Charge(ctx context.Context, id int64) (ledger.Charge, error)
	Update(ctx context.Context, id int64, s ledger.State) error
}

// Apply records each change, then applies it, stopping at the first failure.
func Apply(ctx context.Context, l Ledger, record func(Change) error, changes []Change) (applied int, err error) {
	for _, c := range changes {
		if err := record(c); err != nil {
			return applied, fmt.Errorf("journal charge %d: %w", c.Charge, err)
		}
		if err := l.Update(ctx, c.Charge, c.After); err != nil {
			return applied, fmt.Errorf("update charge %d: %w", c.Charge, err)
		}
		applied++
	}
	return applied, nil
}

// Undo returns the changes that reverse run, newest first. A charge whose category is no
// longer the one the run set is skipped; one already back in its before state needs nothing
// and is counted as unchanged.
func Undo(ctx context.Context, l Ledger, run []Change) (changes, skipped []Change, unchanged int, err error) {
	for i := len(run) - 1; i >= 0; i-- {
		c := run[i]
		cur, err := l.Charge(ctx, c.Charge)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("read charge %d: %w", c.Charge, err)
		}
		if cur.State.Equal(c.Before) {
			unchanged++
			continue
		}
		if cur.State.Category != c.After.Category {
			skipped = append(skipped, c)
			continue
		}
		changes = append(changes, Change{Charge: c.Charge, Before: cur.State, After: c.Before})
	}
	return changes, skipped, unchanged, nil
}
