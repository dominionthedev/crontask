package schedule

import (
	"time"
)

// NextRun estimates the next fire time at or after `from` for a Spec.
// Returns nil when unknown (e.g. @reboot or opaque cron with no mapping).
func NextRun(spec *Spec, from time.Time) *time.Time {
	if spec == nil {
		return nil
	}
	from = from.In(time.Local).Truncate(time.Minute)

	if spec.IntervalSeconds > 0 {
		// Align to wall-clock steps when the interval divides an hour/day cleanly;
		// otherwise step forward from `from` by the interval.
		sec := spec.IntervalSeconds
		t := from.Add(time.Minute) // strictly after current minute
		if sec >= 60 && sec%60 == 0 {
			// minute/hour-based: find next boundary
			for i := 0; i < sec+120; i++ {
				cand := t.Add(time.Duration(i) * time.Minute)
				elapsed := cand.Hour()*3600 + cand.Minute()*60 + cand.Second()
				if elapsed%sec == 0 {
					return &cand
				}
			}
		}
		cand := from.Add(time.Duration(sec) * time.Second)
		return &cand
	}

	if len(spec.Calendars) > 0 {
		// Scan up to 8 days ahead, minute resolution.
		limit := from.Add(8 * 24 * time.Hour)
		for t := from.Add(time.Minute); t.Before(limit); t = t.Add(time.Minute) {
			for _, cal := range spec.Calendars {
				if calendarMatches(cal, t) {
					tt := t
					return &tt
				}
			}
		}
	}

	return nil
}

func calendarMatches(cal Calendar, t time.Time) bool {
	if cal.Minute != nil && t.Minute() != *cal.Minute {
		return false
	}
	if cal.Hour != nil && t.Hour() != *cal.Hour {
		return false
	}
	if cal.Day != nil && t.Day() != *cal.Day {
		return false
	}
	if cal.Month != nil && int(t.Month()) != *cal.Month {
		return false
	}
	if cal.Weekday != nil {
		// launchd: 0 and 7 = Sunday
		wd := int(t.Weekday()) // Go: Sunday=0
		want := *cal.Weekday
		if want == 7 {
			want = 0
		}
		if wd != want {
			return false
		}
	}
	return true
}

// FormatRelative returns a short relative description of t from now.
func FormatRelative(t time.Time, now time.Time) string {
	d := t.Sub(now)
	if d < 0 {
		return t.Local().Format("Jan 2 15:04")
	}
	if d < time.Minute {
		return "now"
	}
	if d < time.Hour {
		return t.Local().Format("15:04")
	}
	if d < 24*time.Hour {
		return t.Local().Format("15:04")
	}
	return t.Local().Format("Mon 15:04")
}
