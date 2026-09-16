package task

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
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

	duration := time.Since(start).Seconds()

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

	fmt.Fprintf(os.Stderr, "← finished status=%d (%.1fs)\n", status, duration)

	if record {
		now := time.Now().UTC()
		t.LastRun = &now
		t.LastStatus = &status
		t.UpdatedAt = now
		store := NewStore()
		_ = store.Put(t)
	}

	url := ""
	if status == 0 && t.WebhookSuccess != "" {
		url = t.WebhookSuccess
	}
	if status != 0 && t.WebhookFailure != "" {
		url = t.WebhookFailure
	}
	if url != "" {
		if err := postWebhook(url, t, status, duration, string(out)); err != nil {
			fmt.Fprintf(os.Stderr, "webhook error: %v\n", err)
		}
	}

	return status, nil
}

func postWebhook(url string, t *Task, status int, duration float64, output string) error {
	body, _ := json.Marshal(map[string]any{
		"task":     t.Name,
		"status":   status,
		"duration": duration,
		"command":  t.Command,
		"schedule": t.Schedule,
		"output":   truncate(output, 4096),
		"time":     time.Now().UTC().Format(time.RFC3339),
	})
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "crontask/0.2")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
