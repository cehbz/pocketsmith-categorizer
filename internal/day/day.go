// Package day represents calendar dates as midnight UTC times.
package day

import "time"

const layout = "2006-01-02"

// Of returns the calendar date of t in loc.
func Of(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// Parse reads a YYYY-MM-DD date.
func Parse(s string) (time.Time, error) { return time.Parse(layout, s) }

// Format writes a date as YYYY-MM-DD.
func Format(d time.Time) string { return d.Format(layout) }

// Between returns the number of days from a to b.
func Between(a, b time.Time) int { return int(b.Sub(a).Hours() / 24) }
