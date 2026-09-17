package ingest

import "time"

// isoWeekStart returns the Monday 00:00 UTC that begins t's ISO-8601 week.
// Anchored on January 4th, which ISO 8601 guarantees always falls in week 1
// of its year — this avoids the ambiguity of parsing/reconstructing a date
// from a "YYYY-Www" label.
func isoWeekStart(t time.Time) time.Time {
	t = t.UTC()

	jan4 := time.Date(t.Year(), time.January, 4, 0, 0, 0, 0, time.UTC)

	isoWeekday := int(jan4.Weekday())
	if isoWeekday == 0 {
		isoWeekday = 7 // Go's Sunday = 0; ISO treats Sunday as day 7
	}

	week1Monday := jan4.AddDate(0, 0, -(isoWeekday - 1))

	_, week := t.ISOWeek()

	return week1Monday.AddDate(0, 0, (week-1)*7)
}
