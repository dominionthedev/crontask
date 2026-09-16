package backend

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/dominionthedev/crontask/internal/schedule"
	"github.com/dominionthedev/crontask/internal/task"
)

// Backend installs/uninstalls a task into the system scheduler.
type Backend interface {
	Name() string
	Install(t *task.Task, spec *schedule.Spec) error
	Uninstall(t *task.Task) error
	IsLive(t *task.Task) bool
}

// Detect returns the preferred backend for the current platform.
func Detect(preferred string) Backend {
	switch preferred {
	case "crontab":
		return &Crontab{}
	case "launchd":
		return &Launchd{}
	default: // auto
		if runtime.GOOS == "darwin" {
			if _, err := exec.LookPath("launchctl"); err == nil {
				return &Launchd{}
			}
		}
		return &Crontab{}
	}
}

// Available reports which backends can actually be used right now.
func Available() (crontab, launchd bool) {
	_, err := exec.LookPath("crontab")
	crontab = err == nil
	if runtime.GOOS == "darwin" {
		_, err = exec.LookPath("launchctl")
		launchd = err == nil
	}
	return
}

// SelfBinary returns the absolute path of the current executable.
func SelfBinary() (string, error) {
	return os.Executable()
}

func quote(s string) string {
	if strings.ContainsAny(s, " \t\n\"'\\$`") {
		return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
	}
	return s
}

func markerLine(t *task.Task) string {
	return t.CrontabMarker
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func output(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.Output()
	return string(out), err
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

func mustLookPath(bin string) error {
	_, err := exec.LookPath(bin)
	if err != nil {
		return fmt.Errorf("%s not found in PATH", bin)
	}
	return nil
}
