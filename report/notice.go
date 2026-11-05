package report

import "strings"

// Notice is the subject of an email about r, and whether r is due one: whether a section new
// this run isn't empty. Trip hints and unreadable receipts recur every run, so only those no
// earlier run counted (NewHints, NewUnreadable) make one. Standing lists (the undecided,
// receipts matching no charge, income), what the run did for the user (changes,
// corrections) and corrections of the trips layer's categories make no notice.
func Notice(r Run) (subject string, due bool) {
	var parts []string
	count := func(n int, one, many string) {
		if n > 0 {
			parts = append(parts, plural(n, one, many))
		}
	}
	byRule, _ := r.amendments()
	count(len(r.RulesAdded), "rule added", "rules added")
	count(len(r.RulesAmended), "rule amended", "rules amended")
	count(len(byRule), "overridden decision", "overridden decisions")
	count(len(r.Suggestions), "suggestion", "suggestions")
	count(len(r.TripsAdded), "trip added", "trips added")
	count(r.NewHints, "trip hint", "trip hints")
	count(len(r.TripVersions), "trip renamed or redated", "trips renamed or redated")
	count(r.NewUnreadable, "unreadable receipt", "unreadable receipts")
	count(r.FlagsApplied, "transaction to review", "transactions to review")
	if len(parts) == 0 {
		return "", false
	}
	return "Categorizer: " + strings.Join(parts, " · "), true
}

// Failure is the subject of an email about a run that failed with err: its first line.
func Failure(err error) string {
	line, _, _ := strings.Cut(err.Error(), "\n")
	return "Categorizer: run failed: " + line
}
