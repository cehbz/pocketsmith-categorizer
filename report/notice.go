package report

import (
	"fmt"
	"strings"
)

// Notice is the subject of an email about r, and whether r is due one: whether a section new
// this run isn't empty. Standing lists (the undecided, receipts matching no charge, income)
// and what the run did for the user (changes, corrections) make no notice.
func Notice(r Run) (subject string, due bool) {
	var parts []string
	count := func(n int, one, many string) {
		switch {
		case n == 1:
			parts = append(parts, "1 "+one)
		case n > 1:
			parts = append(parts, fmt.Sprintf("%d %s", n, many))
		}
	}
	count(r.FlagsApplied, "to review", "to review")
	count(len(r.Suggestions), "suggestion", "suggestions")
	count(len(r.Proposals), "proposed rule", "proposed rules")
	count(len(r.Amendments), "rule amendment", "rule amendments")
	count(len(r.TripsAdded), "trip added", "trips added")
	count(len(r.TripVersions), "trip renamed or redated", "trips renamed or redated")
	count(len(r.Hints), "trip hint", "trip hints")
	count(len(r.Receipts.Unreadable), "receipt not read", "receipts not read")
	if len(parts) == 0 {
		return "", false
	}
	return "categorize: " + strings.Join(parts, ", "), true
}

// Failure is the subject of an email about a run that failed with err: its first line.
func Failure(err error) string {
	line, _, _ := strings.Cut(err.Error(), "\n")
	return "categorize run failed: " + line
}
