package config

import (
	"os"
	"path/filepath"
	"runtime"
)

const AppName = "crontask"

var (
	cfgDir    string
	dataDir   string
	logDir    string
	tasksFile string
)

func initPaths() {
	if cfgDir != "" {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}

	if runtime.GOOS == "darwin" {
		cfgDir = filepath.Join(home, "Library", "Application Support", AppName)
	} else {
		xdg := os.Getenv("XDG_CONFIG_HOME")
		if xdg == "" {
			xdg = filepath.Join(home, ".config")
		}
		cfgDir = filepath.Join(xdg, AppName)
	}

	dataDir = filepath.Join(cfgDir, "data")
	logDir = filepath.Join(cfgDir, "logs")
	tasksFile = filepath.Join(cfgDir, "tasks.json")

	_ = os.MkdirAll(cfgDir, 0o755)
	_ = os.MkdirAll(dataDir, 0o755)
	_ = os.MkdirAll(logDir, 0o755)
}

func ConfigDir() string { initPaths(); return cfgDir }
func LogDir() string    { initPaths(); return logDir }
func TasksFile() string { initPaths(); return tasksFile }

// LogPath returns the log file path for a task.
func LogPath(name string) string {
	return filepath.Join(LogDir(), name+".log")
}
