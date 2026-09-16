package backend

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/dominionthedev/crontask/internal/config"
	"github.com/dominionthedev/crontask/internal/task"
)

// Crontab manages user crontab entries tagged with the crontask marker.
type Crontab struct{}

func (c *Crontab) Name() string { return "crontab" }

func (c *Crontab) Install(t *task.Task, cronExpr string) error {
	if err := mustLookPath("crontab"); err != nil {
		return err
	}

	self, err := SelfBinary()
	if err != nil {
		return err
	}

	lines, err := c.get()
	if err != nil {
		return err
	}

	// Remove any previous entry for this task
	marker := markerLine(t)
	filtered := make([]string, 0, len(lines))
	for _, l := range lines {
		if !strings.Contains(l, marker) {
			filtered = append(filtered, l)
		}
	}

	logRedirect := ""
	if t.LogStdout || t.LogStderr {
		logFile := config.LogPath(t.Name)
		if t.LogStdout && t.LogStderr {
			logRedirect = " >> " + logFile + " 2>&1"
		} else if t.LogStdout {
			logRedirect = " >> " + logFile
		} else {
			logRedirect = " 2>> " + logFile
		}
	}

	// crontab calls back into us so we can record last_run etc.
	runner := fmt.Sprintf("%s _run %s", quote(self), quote(t.Name))
	entry := fmt.Sprintf("%s %s%s  %s", cronExpr, runner, logRedirect, marker)

	filtered = append(filtered, entry)
	return c.set(filtered)
}

func (c *Crontab) Uninstall(t *task.Task) error {
	if err := mustLookPath("crontab"); err != nil {
		return err
	}
	lines, err := c.get()
	if err != nil {
		return err
	}
	marker := markerLine(t)
	filtered := make([]string, 0, len(lines))
	changed := false
	for _, l := range lines {
		if strings.Contains(l, marker) {
			changed = true
			continue
		}
		filtered = append(filtered, l)
	}
	if !changed {
		return nil
	}
	return c.set(filtered)
}

func (c *Crontab) IsLive(t *task.Task) bool {
	lines, err := c.get()
	if err != nil {
		return false
	}
	marker := markerLine(t)
	for _, l := range lines {
		if strings.Contains(l, marker) {
			return true
		}
	}
	return false
}

// LiveMarkers returns the task names currently present in the user crontab.
func (c *Crontab) LiveMarkers() []string {
	lines, err := c.get()
	if err != nil {
		return nil
	}
	const prefix = "# crontask: "
	var names []string
	for _, l := range lines {
		if idx := strings.Index(l, prefix); idx >= 0 {
			name := strings.TrimSpace(l[idx+len(prefix):])
			if name != "" {
				names = append(names, name)
			}
		}
	}
	return names
}

func (c *Crontab) get() ([]string, error) {
	cmd := exec.Command("crontab", "-l")
	out, err := cmd.Output()
	if err != nil {
		// crontab -l exits 1 when empty
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		return nil, err
	}
	text := strings.TrimRight(string(out), "\n")
	if text == "" {
		return nil, nil
	}
	return strings.Split(text, "\n"), nil
}

func (c *Crontab) set(lines []string) error {
	content := strings.Join(lines, "\n")
	if content != "" {
		content += "\n"
	}
	tmp, err := os.CreateTemp("", "crontask-*.cron")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()

	cmd := exec.Command("crontab", tmp.Name())
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
