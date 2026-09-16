package backend

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/dominionthedev/crontask/internal/config"
	"github.com/dominionthedev/crontask/internal/schedule"
	"github.com/dominionthedev/crontask/internal/task"
)

// Launchd manages user LaunchAgents on macOS.
type Launchd struct{}

func (l *Launchd) Name() string { return "launchd" }

func (l *Launchd) Install(t *task.Task, spec *schedule.Spec) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("launchd is only available on macOS")
	}
	if err := mustLookPath("launchctl"); err != nil {
		return err
	}

	plistPath := l.plistPath(t)
	content, err := l.generatePlist(t, spec)
	if err != nil {
		return err
	}
	if err := writeFile(plistPath, content); err != nil {
		return err
	}
	return l.load(plistPath)
}

func (l *Launchd) Uninstall(t *task.Task) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	plistPath := l.plistPath(t)
	_ = l.unload(plistPath)
	if fileExists(plistPath) {
		_ = os.Remove(plistPath)
	}
	return nil
}

func (l *Launchd) IsLive(t *task.Task) bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	if !fileExists(l.plistPath(t)) {
		return false
	}
	out, err := output("launchctl", "list")
	if err != nil {
		return false
	}
	return strings.Contains(out, t.LaunchdLabel)
}

func (l *Launchd) plistPath(t *task.Task) string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, "Library", "LaunchAgents")
	_ = os.MkdirAll(dir, 0o755)
	return filepath.Join(dir, t.LaunchdLabel+".plist")
}

func (l *Launchd) generatePlist(t *task.Task, spec *schedule.Spec) (string, error) {
	self, err := SelfBinary()
	if err != nil {
		return "", err
	}

	progArgs := []string{self, "_run", t.Name}
	logFile := config.LogPath(t.Name)

	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")
	b.WriteString(fmt.Sprintf("  <key>Label</key><string>%s</string>\n", t.LaunchdLabel))
	b.WriteString("  <key>ProgramArguments</key>\n  <array>\n")
	for _, a := range progArgs {
		b.WriteString(fmt.Sprintf("    <string>%s</string>\n", xmlEscape(a)))
	}
	b.WriteString("  </array>\n")

	if t.WorkingDir != "" {
		b.WriteString(fmt.Sprintf("  <key>WorkingDirectory</key><string>%s</string>\n", xmlEscape(t.WorkingDir)))
	}

	// Prefer structured Spec mapping over ad-hoc cron parsing.
	switch {
	case spec.IntervalSeconds > 0:
		b.WriteString(fmt.Sprintf("  <key>StartInterval</key><integer>%d</integer>\n", spec.IntervalSeconds))
	case len(spec.Calendars) == 1:
		writeCalendarDict(&b, spec.Calendars[0], "  ")
	case len(spec.Calendars) > 1:
		// Multiple StartCalendarInterval entries (e.g. weekdays).
		b.WriteString("  <key>StartCalendarInterval</key>\n  <array>\n")
		for _, cal := range spec.Calendars {
			writeCalendarDict(&b, cal, "    ")
		}
		b.WriteString("  </array>\n")
	case spec.Cron == "@reboot":
		// Run once when the agent is loaded (login / boot for user agents).
		b.WriteString("  <key>RunAtLoad</key><true/>\n")
	default:
		// Opaque cron we couldn't map — fall back to daily midnight so the
		// plist is still valid; user should prefer crontab for exotic exprs.
		b.WriteString("  <key>StartCalendarInterval</key>\n  <dict>\n")
		b.WriteString("    <key>Hour</key><integer>0</integer>\n")
		b.WriteString("    <key>Minute</key><integer>0</integer>\n")
		b.WriteString("  </dict>\n")
	}

	// Always set RunAtLoad false unless @reboot handled above.
	if spec.Cron != "@reboot" {
		b.WriteString("  <key>RunAtLoad</key><false/>\n")
	}

	b.WriteString(fmt.Sprintf("  <key>StandardOutPath</key><string>%s</string>\n", xmlEscape(logFile)))
	b.WriteString(fmt.Sprintf("  <key>StandardErrorPath</key><string>%s</string>\n", xmlEscape(logFile)))
	b.WriteString("</dict>\n</plist>\n")
	return b.String(), nil
}

func writeCalendarDict(b *strings.Builder, cal schedule.Calendar, indent string) {
	b.WriteString(indent + "<dict>\n")
	if cal.Minute != nil {
		b.WriteString(fmt.Sprintf("%s  <key>Minute</key><integer>%d</integer>\n", indent, *cal.Minute))
	}
	if cal.Hour != nil {
		b.WriteString(fmt.Sprintf("%s  <key>Hour</key><integer>%d</integer>\n", indent, *cal.Hour))
	}
	if cal.Day != nil {
		b.WriteString(fmt.Sprintf("%s  <key>Day</key><integer>%d</integer>\n", indent, *cal.Day))
	}
	if cal.Weekday != nil {
		b.WriteString(fmt.Sprintf("%s  <key>Weekday</key><integer>%d</integer>\n", indent, *cal.Weekday))
	}
	if cal.Month != nil {
		b.WriteString(fmt.Sprintf("%s  <key>Month</key><integer>%d</integer>\n", indent, *cal.Month))
	}
	b.WriteString(indent + "</dict>\n")
}

func (l *Launchd) load(plistPath string) error {
	uid := os.Getuid()
	err := run("launchctl", "bootstrap", fmt.Sprintf("gui/%d", uid), plistPath)
	if err != nil {
		return run("launchctl", "load", plistPath)
	}
	return nil
}

func (l *Launchd) unload(plistPath string) error {
	uid := os.Getuid()
	err := run("launchctl", "bootout", fmt.Sprintf("gui/%d", uid), plistPath)
	if err != nil {
		_ = run("launchctl", "unload", plistPath)
	}
	return nil
}

func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}

var _ = exec.Command
