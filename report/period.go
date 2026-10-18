package report

import "time"

// RangeLabel names a requested date range in the report's style: a whole
// month reads "September 2026" (or "سبتمبر 2026"), whole months "August 2026
// – September 2026", anything else its real endpoints ("1–15 Sep 2026",
// "من 1 إلى 15 سبتمبر 2026"). Set it as Report.Period so the header names
// the range that was asked for rather than the dates trips happened to fall on.
func RangeLabel(l Lang, start, end time.Time) string {
	if end.Before(start) {
		start, end = end, start
	}
	firstDay := start.Day() == 1
	lastDay := end.AddDate(0, 0, 1).Day() == 1
	if firstDay && lastDay {
		a, b := monthYear(l, start), monthYear(l, end)
		if a == b {
			return a
		}
		return a + " – " + b
	}
	return dateRange(l, start, end)
}
