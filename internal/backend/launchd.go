package backend

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/dominionthedev/crontask/internal/config"
	"github.com/dominionthedev/crontask/internal/task"
)

// Launchd manages user LaunchAgents on macOS.
type Launchd struct{}

func (l *Launchd) Name() string { return "launchd" }

func (l *Launchd) Install(t *task.Task, cronExpr string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("launchd is only available on macOS")
	}
	if err := mustLookPath("launchctl"); err != nil {
		return err
	}

	plistPath := l.plistPath(t)
	content, err := l.generatePlist(t, cronExpr)
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
	// quick check: plist exists and launchctl list contains the label
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

func (l *Launchd) generatePlist(t *task.Task, cronExpr string) (string, error) {
	self, err := SelfBinary()
	if err != nil {
		return "", err
	}

	// We always run through our own binary so last_run is recorded.
	progArgs := []string{self, "_run", t.Name}

	var interval *int
	var calendar map[string]int

	// Very small subset of cron → launchd mapping (same as Python prototype)
	switch {
	case strings.HasPrefix(cronExpr, "*/") && strings.HasSuffix(cronExpr, " * * * *"):
		nStr := strings.TrimPrefix(strings.Fields(cronExpr)[0], "*/")
		if n, err := strconv.Atoi(nStr); err == nil {
			secs := n * 60
			interval = &secs
		}
	case cronExpr == "0 * * * *" || cronExpr == "@hourly":
		secs := 3600
		interval = &secs
	case cronExpr == "0 0 * * *" || cronExpr == "@daily":
		calendar = map[string]int{"Hour": 0, "Minute": 0}
	default:
		// fallback: try to parse "M H * * *"
		parts := strings.Fields(cronExpr)
		if len(parts) == 5 && parts[2] == "*" && parts[3] == "*" && parts[4] == "*" {
			mi, err1 := strconv.Atoi(parts[0])
			h, err2 := strconv.Atoi(parts[1])
			if err1 == nil && err2 == nil {
				calendar = map[string]int{"Hour": h, "Minute": mi}
			}
		}
		if calendar == nil && interval == nil {
			// last resort
			calendar = map[string]int{"Hour": 0, "Minute": 0}
		}
	}

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

	if interval != nil {
		b.WriteString(fmt.Sprintf("  <key>StartInterval</key><integer>%d</integer>\n", *interval))
	}
	if calendar != nil {
		b.WriteString("  <key>StartCalendarInterval</key>\n  <dict>\n")
		for k, v := range calendar {
			b.WriteString(fmt.Sprintf("    <key>%s</key><integer>%d</integer>\n", k, v))
		}
		b.WriteString("  </dict>\n")
	}

	b.WriteString(fmt.Sprintf("  <key>StandardOutPath</key><string>%s</string>\n", xmlEscape(logFile)))
	b.WriteString(fmt.Sprintf("  <key>StandardErrorPath</key><string>%s</string>\n", xmlEscape(logFile)))
	b.WriteString("  <key>RunAtLoad</key><false/>\n")
	b.WriteString("</dict>\n</plist>\n")
	return b.String(), nil
}

func (l *Launchd) load(plistPath string) error {
	uid := os.Getuid()
	// Prefer modern bootstrap
	err := run("launchctl", "bootstrap", fmt.Sprintf("gui/%d", uid), plistPath)
	if err != nil {
		// fallback for older macOS
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

// Keep the compiler happy on non-darwin when we reference exec
var _ = exec.Command
