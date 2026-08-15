// Package calendar works out which days count.
//
// The company week runs Monday to Friday, so a Saturday with no file is not a
// miss. Neither are holidays: without them, the 1st of May would show up as the
// whole workforce being absent and the panel would turn into noise.
package calendar

import "time"

// DefaultWorkdays: Monday (1) to Friday (5), ISO numbering.
var DefaultWorkdays = []int{1, 2, 3, 4, 5}

// IsWorkday says whether a date counts for tracking.
func IsWorkday(date time.Time, workdays []int, holidays map[string]bool) bool {
	if holidays[date.Format("2006-01-02")] {
		return false
	}
	if workdays == nil {
		workdays = DefaultWorkdays
	}
	iso := int(date.Weekday())
	if iso == 0 {
		iso = 7 // domingo
	}
	for _, d := range workdays {
		if d == iso {
			return true
		}
	}
	return false
}

// Workdays lists the working days between two dates, both included.
func Workdays(from, to time.Time, holidays map[string]bool) []string {
	out := []string{}
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		if IsWorkday(d, DefaultWorkdays, holidays) {
			out = append(out, d.Format("2006-01-02"))
		}
	}
	return out
}

// WorkWeek returns Monday to Friday of the week containing the given date.
//
// Five days, not seven: the report and the weekly view are about the working
// week. A Saturday with data is still stored, it just does not count as a miss.
func WorkWeek(date time.Time) []time.Time {
	iso := int(date.Weekday())
	if iso == 0 {
		iso = 7
	}
	lunes := date.AddDate(0, 0, -(iso - 1))
	lunes = time.Date(lunes.Year(), lunes.Month(), lunes.Day(), 0, 0, 0, 0, date.Location())

	week := make([]time.Time, 5)
	for i := range week {
		week[i] = lunes.AddDate(0, 0, i)
	}
	return week
}

// DaysBetween returns every day in the range, one by one, Saturdays and Sundays
// included.
//
// It deliberately does not filter by workday: the report uses it, and whoever
// asks for a specific range asks because they want those exact days. Filtering
// here would make the very day someone went to look at disappear from the
// document.
func DaysBetween(from, to time.Time) []time.Time {
	if to.Before(from) {
		return nil
	}
	days := make([]time.Time, 0, int(to.Sub(from).Hours()/24)+1)
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		days = append(days, d)
	}
	return days
}
