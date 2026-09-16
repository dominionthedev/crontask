package task

import (
	"time"
)

// Task is the persistent definition of a scheduled automation.
type Task struct {
	Name        string            `json:"name"`
	Command     string            `json:"command"`
	Schedule    string            `json:"schedule"` // human or cron expression as typed by user
	Backend     string            `json:"backend"`  // auto | crontab | launchd
	Enabled     bool              `json:"enabled"`
	Description string            `json:"description,omitempty"`
	WorkingDir  string            `json:"working_dir,omitempty"`
	Shell       string            `json:"shell,omitempty"`
	Env         map[string]string `json:"env,omitempty"`

	LogStdout bool `json:"log_stdout"`
	LogStderr bool `json:"log_stderr"`

	NotifyOnFailure bool   `json:"notify_on_failure,omitempty"`
	WebhookSuccess  string `json:"webhook_success,omitempty"`
	WebhookFailure  string `json:"webhook_failure,omitempty"`

	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	LastRun    *time.Time `json:"last_run,omitempty"`
	LastStatus *int       `json:"last_status,omitempty"`

	// Generated identifiers (stable once created)
	CrontabMarker string `json:"crontab_marker"`
	LaunchdLabel  string `json:"launchd_label"`
}

// New creates a Task with sensible defaults.
func New(name, command, schedule string) *Task {
	now := time.Now().UTC()
	t := &Task{
		Name:          name,
		Command:       command,
		Schedule:      schedule,
		Backend:       "auto",
		Enabled:       true,
		Shell:         "/bin/bash",
		Env:           map[string]string{},
		LogStdout:     true,
		LogStderr:     true,
		CreatedAt:     now,
		UpdatedAt:     now,
		CrontabMarker: "# crontask: " + name,
		LaunchdLabel:  "com.crontask." + sanitizeLabel(name),
	}
	return t
}

func sanitizeLabel(s string) string {
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '_', c == '-':
			b = append(b, c)
		default:
			b = append(b, '-')
		}
	}
	return string(b)
}
