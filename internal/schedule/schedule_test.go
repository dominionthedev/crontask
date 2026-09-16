package schedule

import (
	"testing"
	"time"
)

func TestParseEveryInterval(t *testing.T) {
	cases := []struct {
		in       string
		cron     string
		interval int
	}{
		{"every 5m", "*/5 * * * *", 300},
		{"every 15min", "*/15 * * * *", 900},
		{"every 2h", "0 */2 * * *", 7200},
		{"every 1d", "0 0 */1 * *", 86400},
		{"every 3 days", "0 0 */3 * *", 3 * 86400},
	}
	for _, tc := range cases {
		spec, err := Parse(tc.in)
		if err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if spec.Cron != tc.cron {
			t.Errorf("%q cron: got %q want %q", tc.in, spec.Cron, tc.cron)
		}
		if spec.IntervalSeconds != tc.interval {
			t.Errorf("%q interval: got %d want %d", tc.in, spec.IntervalSeconds, tc.interval)
		}
	}
}

func TestParseEveryAt(t *testing.T) {
	spec, err := Parse("every day at 14:30")
	if err != nil {
		t.Fatal(err)
	}
	if spec.Cron != "30 14 * * *" {
		t.Errorf("cron %q", spec.Cron)
	}
	if len(spec.Calendars) != 1 || *spec.Calendars[0].Hour != 14 || *spec.Calendars[0].Minute != 30 {
		t.Errorf("calendar %#v", spec.Calendars)
	}

	spec, err = Parse("every weekday at 09:00")
	if err != nil {
		t.Fatal(err)
	}
	if spec.Cron != "0 9 * * 1-5" {
		t.Errorf("cron %q", spec.Cron)
	}
	if len(spec.Calendars) != 5 {
		t.Errorf("want 5 weekday calendars, got %d", len(spec.Calendars))
	}

	spec, err = Parse("every monday at 08:15")
	if err != nil {
		t.Fatal(err)
	}
	if spec.Cron != "15 8 * * 1" {
		t.Errorf("cron %q", spec.Cron)
	}
	if len(spec.Calendars) != 1 || *spec.Calendars[0].Weekday != 1 {
		t.Errorf("calendar %#v", spec.Calendars)
	}
}

func TestParseAt(t *testing.T) {
	spec, err := Parse("at 23:59")
	if err != nil {
		t.Fatal(err)
	}
	if spec.Cron != "59 23 * * *" {
		t.Errorf("cron %q", spec.Cron)
	}
}

func TestParseRejectsHardcodedAliases(t *testing.T) {
	// These old aliases are intentionally gone.
	for _, s := range []string{"hourly", "daily", "midnight", "weekly", "every hour", "every day"} {
		if _, err := Parse(s); err == nil {
			t.Errorf("expected error for legacy alias %q", s)
		}
	}
}

func TestParseSpecials(t *testing.T) {
	spec, err := Parse("@hourly")
	if err != nil {
		t.Fatal(err)
	}
	if spec.IntervalSeconds != 3600 {
		t.Errorf("interval %d", spec.IntervalSeconds)
	}
}

func TestNextRunDaily(t *testing.T) {
	spec, err := Parse("every day at 14:30")
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 9, 16, 10, 0, 0, 0, time.Local)
	n := NextRun(spec, from)
	if n == nil {
		t.Fatal("expected next run")
	}
	if n.Hour() != 14 || n.Minute() != 30 {
		t.Fatalf("got %v", n)
	}
}

func TestNextRunInterval(t *testing.T) {
	spec, err := Parse("every 15m")
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 9, 16, 10, 7, 0, 0, time.Local)
	n := NextRun(spec, from)
	if n == nil {
		t.Fatal("expected next run")
	}
}
