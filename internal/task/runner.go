package task

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/dominionthedev/crontask/internal/config"
)

// Run executes the task command, appends to its log, and optionally records last_run.
func Run(t *Task, record bool) (int, error) {
	logFile := config.LogPath(t.Name)

	fmt.Fprintf(os.Stderr, "→ running %q: %s\n", t.Name, t.Command)

	start := time.Now()

	shell := t.Shell
	if shell == "" {
		shell = "/bin/bash"
	}

	cmd := exec.Command(shell, "-c", t.Command)
	if t.WorkingDir != "" {
		cmd.Dir = t.WorkingDir
	}
	if len(t.Env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range t.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	out, err := cmd.CombinedOutput()
	status := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			status = exitErr.ExitCode()
		} else {
			status = 1
		}
	}

	// Always write to log
	f, ferr := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if ferr == nil {
		fmt.Fprintf(f, "\n--- %s ---\n", time.Now().UTC().Format(time.RFC3339))
		f.Write(out)
		if len(out) > 0 && out[len(out)-1] != '\n' {
			f.Write([]byte("\n"))
		}
		f.Close()
	}

	// Echo to caller (interactive run)
	if len(out) > 0 {
		os.Stdout.Write(out)
		if out[len(out)-1] != '\n' {
			fmt.Println()
		}
	}

	duration := time.Since(start).Seconds()
	fmt.Fprintf(os.Stderr, "← finished status=%d (%.1fs)\n", status, duration)

	if record {
		now := time.Now().UTC()
		t.LastRun = &now
		t.LastStatus = &status
		t.UpdatedAt = now
		store := NewStore()
		_ = store.Put(t)
	}

	// Placeholder webhooks
	if status == 0 && t.WebhookSuccess != "" {
		fmt.Fprintf(os.Stderr, "(would POST success to %s)\n", t.WebhookSuccess)
	}
	if status != 0 && t.WebhookFailure != "" {
		fmt.Fprintf(os.Stderr, "(would POST failure to %s)\n", t.WebhookFailure)
	}

	return status, nil
}
