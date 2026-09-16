package schedule

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Friendly shortcuts → cron expression
var friendly = map[string]string{
	"every minute": "* * * * *",
	"every 5m":     "*/5 * * * *",
	"every 10m":    "*/10 * * * *",
	"every 15m":    "*/15 * * * *",
	"every 30m":    "*/30 * * * *",
	"hourly":       "0 * * * *",
	"every hour":   "0 * * * *",
	"daily":        "0 0 * * *",
	"every day":    "0 0 * * *",
	"midnight":     "0 0 * * *",
	"weekly":       "0 0 * * 0",
	"monthly":      "0 0 1 * *",
	"@reboot":      "@reboot",
	"@hourly":      "@hourly",
	"@daily":       "@daily",
	"@weekly":      "@weekly",
	"@monthly":     "@monthly",
}

var (
	reEvery   = regexp.MustCompile(`(?i)^every\s+(\d+)\s*(m|min|minutes?|h|hours?|d|days?)$`)
	reAt      = regexp.MustCompile(`(?i)^(?:daily\s+)?at\s+(\d{1,2}):(\d{2})$`)
	reWeekday = regexp.MustCompile(`(?i)^weekdays?\s+at\s+(\d{1,2}):(\d{2})$`)
)

// Parse converts a human schedule or cron expression into a canonical cron string.
func Parse(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("empty schedule")
	}

	lower := strings.ToLower(s)
	if cron, ok := friendly[lower]; ok {
		return cron, nil
	}

	// every Nm / Nh / Nd
	if m := reEvery.FindStringSubmatch(s); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return "", fmt.Errorf("invalid number in schedule")
		}
		unit := strings.ToLower(m[2][:1])
		switch unit {
		case "m":
			if n < 1 || n > 59 {
				return "", fmt.Errorf("minutes must be 1-59")
			}
			return fmt.Sprintf("*/%d * * * *", n), nil
		case "h":
			if n < 1 || n > 23 {
				return "", fmt.Errorf("hours must be 1-23")
			}
			return fmt.Sprintf("0 */%d * * *", n), nil
		case "d":
			if n < 1 {
				return "", fmt.Errorf("days must be >= 1")
			}
			return fmt.Sprintf("0 0 */%d * *", n), nil
		}
	}

	// daily at HH:MM  or  at HH:MM
	if m := reAt.FindStringSubmatch(s); m != nil {
		h, _ := strconv.Atoi(m[1])
		mi, _ := strconv.Atoi(m[2])
		if h < 0 || h > 23 || mi < 0 || mi > 59 {
			return "", fmt.Errorf("invalid time")
		}
		return fmt.Sprintf("%d %d * * *", mi, h), nil
	}

	// weekdays at HH:MM
	if m := reWeekday.FindStringSubmatch(s); m != nil {
		h, _ := strconv.Atoi(m[1])
		mi, _ := strconv.Atoi(m[2])
		if h < 0 || h > 23 || mi < 0 || mi > 59 {
			return "", fmt.Errorf("invalid time")
		}
		return fmt.Sprintf("%d %d * * 1-5", mi, h), nil
	}

	// Assume classic 5-field cron or @special
	parts := strings.Fields(s)
	if len(parts) == 5 || strings.HasPrefix(s, "@") {
		return s, nil
	}

	return "", fmt.Errorf("unrecognised schedule %q — try: every 15m | daily at 09:00 | weekdays at 08:30 | classic cron", s)
}
