// Package schedule parses human-friendly schedules into cron expressions
// and structured specs suitable for launchd.
//
// Supported forms (case-insensitive):
//
//	every <N>m|min|minute(s)     → every N minutes
//	every <N>h|hour(s)           → every N hours
//	every <N>d|day(s)            → every N days (at 00:00)
//	every day at HH:MM           → daily at 24h time
//	every weekday(s) at HH:MM    → Mon–Fri at 24h time
//	every <weekday> at HH:MM     → specific weekday (monday…sunday)
//	at HH:MM                     → daily at 24h time
//	@reboot @hourly @daily …     → specials
//	classic 5-field cron         → passed through
package schedule

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Calendar is one launchd StartCalendarInterval dict.
// Nil pointer fields mean "any" (omitted from the plist).
type Calendar struct {
	Minute  *int // 0–59
	Hour    *int // 0–23
	Day     *int // day of month 1–31
	Weekday *int // 0–7 (0 and 7 = Sunday), launchd convention
	Month   *int // 1–12
}

// Spec is the parsed schedule: always has Cron for crontab, plus optional
// IntervalSeconds or Calendars for launchd.
type Spec struct {
	// Original input as typed by the user.
	Input string
	// Canonical 5-field cron or @special (for crontab backend).
	Cron string
	// If > 0, launchd should use StartInterval (seconds).
	IntervalSeconds int
	// If non-empty, launchd should use StartCalendarInterval (one or more).
	Calendars []Calendar
}

var (
	reEveryInterval = regexp.MustCompile(`(?i)^every\s+(\d+)\s*(m|mins?|minutes?|h|hours?|d|days?)$`)
	reEveryAt       = regexp.MustCompile(`(?i)^every\s+(day|days|weekday|weekdays|monday|mon|tuesday|tue|wednesday|wed|thursday|thu|friday|fri|saturday|sat|sunday|sun)\s+at\s+(\d{1,2}):(\d{2})$`)
	reAt            = regexp.MustCompile(`(?i)^at\s+(\d{1,2}):(\d{2})$`)
	reDailyAt       = regexp.MustCompile(`(?i)^daily\s+at\s+(\d{1,2}):(\d{2})$`)
	reWeekdaysAt    = regexp.MustCompile(`(?i)^weekdays?\s+at\s+(\d{1,2}):(\d{2})$`)
)

var weekdayToCron = map[string]string{
	"sunday": "0", "sun": "0",
	"monday": "1", "mon": "1",
	"tuesday": "2", "tue": "2",
	"wednesday": "3", "wed": "3",
	"thursday": "4", "thu": "4",
	"friday": "5", "fri": "5",
	"saturday": "6", "sat": "6",
}

var weekdayToLaunchd = map[string]int{
	"sunday": 0, "sun": 0,
	"monday": 1, "mon": 1,
	"tuesday": 2, "tue": 2,
	"wednesday": 3, "wed": 3,
	"thursday": 4, "thu": 4,
	"friday": 5, "fri": 5,
	"saturday": 6, "sat": 6,
}

// Parse converts a human schedule or cron expression into a Spec.
func Parse(s string) (*Spec, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("empty schedule")
	}

	lower := strings.ToLower(s)

	switch lower {
	case "@reboot":
		return &Spec{Input: s, Cron: "@reboot"}, nil
	case "@hourly":
		return intervalSpec(s, "0 * * * *", 3600), nil
	case "@daily", "@midnight":
		return calendarDaily(s, "0 0 * * *", 0, 0), nil
	case "@weekly":
		wd := 0
		return &Spec{
			Input: s,
			Cron:  "0 0 * * 0",
			Calendars: []Calendar{{
				Minute:  intPtr(0),
				Hour:    intPtr(0),
				Weekday: &wd,
			}},
		}, nil
	case "@monthly":
		return &Spec{
			Input: s,
			Cron:  "0 0 1 * *",
			Calendars: []Calendar{{
				Minute: intPtr(0),
				Hour:   intPtr(0),
				Day:    intPtr(1),
			}},
		}, nil
	}

	if m := reEveryInterval.FindStringSubmatch(s); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil || n < 1 {
			return nil, fmt.Errorf("invalid interval count in %q", s)
		}
		unit := strings.ToLower(m[2])
		switch {
		case strings.HasPrefix(unit, "m"):
			if n > 59 {
				return nil, fmt.Errorf("minutes must be 1–59")
			}
			return intervalSpec(s, fmt.Sprintf("*/%d * * * *", n), n*60), nil
		case strings.HasPrefix(unit, "h"):
			if n > 23 {
				return nil, fmt.Errorf("hours must be 1–23")
			}
			return intervalSpec(s, fmt.Sprintf("0 */%d * * *", n), n*3600), nil
		case strings.HasPrefix(unit, "d"):
			return intervalSpec(s, fmt.Sprintf("0 0 */%d * *", n), n*86400), nil
		}
	}

	if m := reEveryAt.FindStringSubmatch(s); m != nil {
		when := strings.ToLower(m[1])
		h, mi, err := parseHHMM(m[2], m[3])
		if err != nil {
			return nil, err
		}
		return everyAtSpec(s, when, h, mi)
	}

	if m := reAt.FindStringSubmatch(s); m != nil {
		h, mi, err := parseHHMM(m[1], m[2])
		if err != nil {
			return nil, err
		}
		return calendarDaily(s, fmt.Sprintf("%d %d * * *", mi, h), h, mi), nil
	}

	if m := reDailyAt.FindStringSubmatch(s); m != nil {
		h, mi, err := parseHHMM(m[1], m[2])
		if err != nil {
			return nil, err
		}
		return calendarDaily(s, fmt.Sprintf("%d %d * * *", mi, h), h, mi), nil
	}

	if m := reWeekdaysAt.FindStringSubmatch(s); m != nil {
		h, mi, err := parseHHMM(m[1], m[2])
		if err != nil {
			return nil, err
		}
		return everyAtSpec(s, "weekdays", h, mi)
	}

	parts := strings.Fields(s)
	if len(parts) == 5 {
		spec, err := fromCronFields(s, parts)
		if err != nil {
			return &Spec{Input: s, Cron: s}, nil
		}
		return spec, nil
	}

	return nil, fmt.Errorf(
		"unrecognised schedule %q\n"+
			"  try: every 15m | every 2h | every 1d\n"+
			"       every day at 14:30 | every weekday at 09:00 | every monday at 08:00\n"+
			"       at 14:30 | @daily | classic 5-field cron",
		s,
	)
}

// ParseCron returns only the cron string (for callers that only need crontab).
func ParseCron(s string) (string, error) {
	spec, err := Parse(s)
	if err != nil {
		return "", err
	}
	return spec.Cron, nil
}

func parseHHMM(hs, ms string) (h, mi int, err error) {
	h, err = strconv.Atoi(hs)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid hour")
	}
	mi, err = strconv.Atoi(ms)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid minute")
	}
	if h < 0 || h > 23 || mi < 0 || mi > 59 {
		return 0, 0, fmt.Errorf("time must be 00:00–23:59 (24-hour clock)")
	}
	return h, mi, nil
}

func intPtr(v int) *int { return &v }

func intervalSpec(input, cron string, seconds int) *Spec {
	return &Spec{Input: input, Cron: cron, IntervalSeconds: seconds}
}

func calendarDaily(input, cron string, hour, minute int) *Spec {
	return &Spec{
		Input: input,
		Cron:  cron,
		Calendars: []Calendar{{
			Minute: intPtr(minute),
			Hour:   intPtr(hour),
		}},
	}
}

func everyAtSpec(input, when string, hour, minute int) (*Spec, error) {
	cronTime := fmt.Sprintf("%d %d", minute, hour)

	switch when {
	case "day", "days":
		return calendarDaily(input, cronTime+" * * *", hour, minute), nil

	case "weekday", "weekdays":
		cals := make([]Calendar, 0, 5)
		for wd := 1; wd <= 5; wd++ {
			w := wd
			cals = append(cals, Calendar{
				Minute:  intPtr(minute),
				Hour:    intPtr(hour),
				Weekday: &w,
			})
		}
		return &Spec{
			Input:     input,
			Cron:      cronTime + " * * 1-5",
			Calendars: cals,
		}, nil

	default:
		cronWD, ok := weekdayToCron[when]
		if !ok {
			return nil, fmt.Errorf("unknown weekday %q", when)
		}
		ldWD := weekdayToLaunchd[when]
		return &Spec{
			Input: input,
			Cron:  fmt.Sprintf("%s * * %s", cronTime, cronWD),
			Calendars: []Calendar{{
				Minute:  intPtr(minute),
				Hour:    intPtr(hour),
				Weekday: &ldWD,
			}},
		}, nil
	}
}

func fromCronFields(input string, p []string) (*Spec, error) {
	min, hour, dom, mon, dow := p[0], p[1], p[2], p[3], p[4]

	if strings.HasPrefix(min, "*/") && hour == "*" && dom == "*" && mon == "*" && dow == "*" {
		n, err := strconv.Atoi(min[2:])
		if err != nil || n < 1 {
			return &Spec{Input: input, Cron: input}, nil
		}
		return intervalSpec(input, input, n*60), nil
	}

	if min == "0" && strings.HasPrefix(hour, "*/") && dom == "*" && mon == "*" && dow == "*" {
		n, err := strconv.Atoi(hour[2:])
		if err != nil || n < 1 {
			return &Spec{Input: input, Cron: input}, nil
		}
		return intervalSpec(input, input, n*3600), nil
	}

	if isNumber(min) && isNumber(hour) && dom == "*" && mon == "*" && dow == "*" {
		mi, _ := strconv.Atoi(min)
		h, _ := strconv.Atoi(hour)
		return calendarDaily(input, input, h, mi), nil
	}

	if isNumber(min) && isNumber(hour) && dom == "*" && mon == "*" {
		mi, _ := strconv.Atoi(min)
		h, _ := strconv.Atoi(hour)
		if isNumber(dow) {
			wd, _ := strconv.Atoi(dow)
			if wd == 7 {
				wd = 0
			}
			return &Spec{
				Input: input,
				Cron:  input,
				Calendars: []Calendar{{
					Minute:  intPtr(mi),
					Hour:    intPtr(h),
					Weekday: &wd,
				}},
			}, nil
		}
		if dow == "1-5" {
			cals := make([]Calendar, 0, 5)
			for w := 1; w <= 5; w++ {
				wd := w
				cals = append(cals, Calendar{
					Minute:  intPtr(mi),
					Hour:    intPtr(h),
					Weekday: &wd,
				})
			}
			return &Spec{Input: input, Cron: input, Calendars: cals}, nil
		}
	}

	if isNumber(min) && isNumber(hour) && isNumber(dom) && mon == "*" && dow == "*" {
		mi, _ := strconv.Atoi(min)
		h, _ := strconv.Atoi(hour)
		d, _ := strconv.Atoi(dom)
		return &Spec{
			Input: input,
			Cron:  input,
			Calendars: []Calendar{{
				Minute: intPtr(mi),
				Hour:   intPtr(h),
				Day:    intPtr(d),
			}},
		}, nil
	}

	return &Spec{Input: input, Cron: input}, nil
}

func isNumber(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}
