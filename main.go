package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/dominionthedev/crontask/internal/backend"
	"github.com/dominionthedev/crontask/internal/config"
	"github.com/dominionthedev/crontask/internal/schedule"
	"github.com/dominionthedev/crontask/internal/task"
	"github.com/spf13/cobra"
)

const version = "0.2.0"

func main() {
	root := &cobra.Command{
		Use:   "crontask",
		Short: "Laptop automations via crontab & launchd",
		Long: `crontask schedules commands on your laptop using the native
scheduler for each platform (crontab on Linux, launchd on macOS).

Schedules use a flexible "every …" syntax with 24-hour times, for example:
  every 15m | every 2h | every 1d
  every day at 14:30 | every weekday at 09:00 | every monday at 08:00
  at 14:30 | @daily | classic 5-field cron`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(
		cmdAdd(),
		cmdList(),
		cmdShow(),
		cmdRm(),
		cmdEnable(),
		cmdDisable(),
		cmdRun(),
		cmdLogs(),
		cmdDoctor(),
		cmdImport(),
		cmdExportCron(),
		cmdVersion(),
		cmdInternalRun(),
	)

	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func cmdAdd() *cobra.Command {
	var (
		command     string
		sched       string
		backendName string
		desc        string
		cwd         string
		disabled    bool
		force       bool
		envVars     []string
		hookOK      string
		hookFail    string
	)
	c := &cobra.Command{
		Use:   "add <name>",
		Short: "Create a new scheduled task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if command == "" || sched == "" {
				return fmt.Errorf("--cmd and --schedule are required")
			}

			spec, err := schedule.Parse(sched)
			if err != nil {
				return err
			}

			env, err := parseEnvFlags(envVars)
			if err != nil {
				return err
			}

			store := task.NewStore()
			tasks, err := store.Load()
			if err != nil {
				return err
			}
			if _, exists := tasks[name]; exists && !force {
				return fmt.Errorf("task %q already exists (use --force to overwrite)", name)
			}

			t := task.New(name, command, sched)
			t.Backend = backendName
			t.Description = desc
			t.WorkingDir = cwd
			t.Enabled = !disabled
			t.Env = env
			t.WebhookSuccess = hookOK
			t.WebhookFailure = hookFail

			b := backend.Detect(t.Backend)
			t.Backend = b.Name()

			if err := store.Put(t); err != nil {
				return err
			}

			if !t.Enabled {
				fmt.Printf("created disabled task %q (schedule: %s)\n", name, spec.Cron)
				return nil
			}

			if err := b.Install(t, spec); err != nil {
				return err
			}
			fmt.Printf("added %q → %s (%s)\n", name, b.Name(), spec.Cron)
			return nil
		},
	}
	c.Flags().StringVar(&command, "cmd", "", "command to run")
	c.Flags().StringVarP(&sched, "schedule", "s", "", "every 15m | every day at 14:30 | cron expr")
	c.Flags().StringVar(&backendName, "backend", "auto", "auto | crontab | launchd")
	c.Flags().StringVarP(&desc, "description", "d", "", "optional description")
	c.Flags().StringVar(&cwd, "cwd", "", "working directory")
	c.Flags().StringArrayVar(&envVars, "env", nil, "environment variable KEY=VALUE (repeatable)")
	c.Flags().StringVar(&hookOK, "webhook-success", "", "POST JSON here when the task succeeds")
	c.Flags().StringVar(&hookFail, "webhook-failure", "", "POST JSON here when the task fails")
	c.Flags().BoolVar(&disabled, "disabled", false, "create but do not install yet")
	c.Flags().BoolVarP(&force, "force", "f", false, "overwrite existing task")
	_ = c.MarkFlagRequired("cmd")
	_ = c.MarkFlagRequired("schedule")
	return c
}

func cmdList() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all tasks",
		RunE: func(cmd *cobra.Command, args []string) error {
			store := task.NewStore()
			tasks, err := store.Load()
			if err != nil {
				return err
			}
			if len(tasks) == 0 {
				fmt.Println("no tasks defined")
				return nil
			}

			crontab := &backend.Crontab{}
			live := map[string]bool{}
			for _, n := range crontab.LiveMarkers() {
				live[n] = true
			}

			fmt.Printf("%-20s %-8s %-10s %-28s %s\n", "NAME", "ENABLED", "BACKEND", "SCHEDULE", "COMMAND")
			fmt.Println(strings.Repeat("-", 96))

			names := make([]string, 0, len(tasks))
			for n := range tasks {
				names = append(names, n)
			}
			for i := 0; i < len(names); i++ {
				for j := i + 1; j < len(names); j++ {
					if names[j] < names[i] {
						names[i], names[j] = names[j], names[i]
					}
				}
			}

			for _, name := range names {
				t := tasks[name]
				en := "no"
				if t.Enabled {
					en = "yes"
				}
				cmdShort := t.Command
				if len(cmdShort) > 40 {
					cmdShort = cmdShort[:40] + "…"
				}
				liveMark := ""
				if t.Backend == "crontab" && live[name] {
					liveMark = " [live]"
				}
				fmt.Printf("%-20s %-8s %-10s %-28s %s%s\n", name, en, t.Backend, t.Schedule, cmdShort, liveMark)
			}
			return nil
		},
	}
}

func cmdShow() *cobra.Command {
	return &cobra.Command{
		Use:   "show <name>",
		Short: "Show full details of a task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store := task.NewStore()
			t, err := store.Get(args[0])
			if err != nil {
				return err
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(t)
		},
	}
}

func cmdRm() *cobra.Command {
	return &cobra.Command{
		Use:     "rm <name>",
		Aliases: []string{"remove", "delete"},
		Short:   "Remove a task",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			store := task.NewStore()
			t, err := store.Get(name)
			if err != nil {
				return err
			}
			b := backend.Detect(t.Backend)
			_ = b.Uninstall(t)
			if err := store.Delete(name); err != nil {
				return err
			}
			fmt.Printf("removed %q\n", name)
			return nil
		},
	}
}

func cmdEnable() *cobra.Command {
	return &cobra.Command{
		Use:   "enable <name>",
		Short: "Enable a task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store := task.NewStore()
			t, err := store.Get(args[0])
			if err != nil {
				return err
			}
			if t.Enabled {
				fmt.Printf("%q is already enabled\n", t.Name)
				return nil
			}
			spec, err := schedule.Parse(t.Schedule)
			if err != nil {
				return err
			}
			b := backend.Detect(t.Backend)
			if err := b.Install(t, spec); err != nil {
				return err
			}
			t.Enabled = true
			t.UpdatedAt = time.Now().UTC()
			if err := store.Put(t); err != nil {
				return err
			}
			fmt.Printf("enabled %q\n", t.Name)
			return nil
		},
	}
}

func cmdDisable() *cobra.Command {
	return &cobra.Command{
		Use:   "disable <name>",
		Short: "Disable a task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store := task.NewStore()
			t, err := store.Get(args[0])
			if err != nil {
				return err
			}
			if !t.Enabled {
				fmt.Printf("%q is already disabled\n", t.Name)
				return nil
			}
			b := backend.Detect(t.Backend)
			_ = b.Uninstall(t)
			t.Enabled = false
			t.UpdatedAt = time.Now().UTC()
			if err := store.Put(t); err != nil {
				return err
			}
			fmt.Printf("disabled %q\n", t.Name)
			return nil
		},
	}
}

func cmdRun() *cobra.Command {
	return &cobra.Command{
		Use:   "run <name>",
		Short: "Run a task immediately",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store := task.NewStore()
			t, err := store.Get(args[0])
			if err != nil {
				return err
			}
			status, err := task.Run(t, true)
			if err != nil {
				return err
			}
			if status != 0 {
				os.Exit(status)
			}
			return nil
		},
	}
}

func cmdInternalRun() *cobra.Command {
	return &cobra.Command{
		Use:    "_run <name>",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store := task.NewStore()
			t, err := store.Get(args[0])
			if err != nil {
				return err
			}
			status, _ := task.Run(t, true)
			os.Exit(status)
			return nil
		},
	}
}

func cmdLogs() *cobra.Command {
	var tail int
	c := &cobra.Command{
		Use:   "logs <name>",
		Short: "Show recent logs for a task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			path := config.LogPath(name)
			data, err := os.ReadFile(path)
			if err != nil {
				if os.IsNotExist(err) {
					fmt.Printf("no log file for %q\n", name)
					return nil
				}
				return err
			}
			lines := strings.Split(string(data), "\n")
			if tail > 0 && len(lines) > tail {
				lines = lines[len(lines)-tail:]
			}
			fmt.Print(strings.Join(lines, "\n"))
			if len(lines) > 0 && lines[len(lines)-1] != "" {
				fmt.Println()
			}
			return nil
		},
	}
	c.Flags().IntVarP(&tail, "tail", "n", 40, "number of lines to show")
	return c
}

func cmdDoctor() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Health check / debug info",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("crontask %s\n", version)
			fmt.Printf("platform   : %s %s\n", runtime.GOOS, runtime.GOARCH)
			fmt.Printf("config dir : %s\n", config.ConfigDir())
			fmt.Printf("tasks file : %s", config.TasksFile())
			if _, err := os.Stat(config.TasksFile()); err == nil {
				fmt.Print(" (exists)")
			} else {
				fmt.Print(" (missing)")
			}
			fmt.Println()
			fmt.Printf("log dir    : %s\n", config.LogDir())

			b := backend.Detect("auto")
			fmt.Printf("backend    : %s\n", b.Name())

			ct, ld := backend.Available()
			fmt.Printf("crontab    : %s\n", avail(ct))
			fmt.Printf("launchctl  : %s\n", avail(ld))

			store := task.NewStore()
			tasks, _ := store.Load()
			fmt.Printf("tasks      : %d\n", len(tasks))

			if ct {
				crontab := &backend.Crontab{}
				markers := crontab.LiveMarkers()
				if len(markers) == 0 {
					fmt.Println("live crontab markers: (none)")
				} else {
					fmt.Printf("live crontab markers: %v\n", markers)
				}
			}
			return nil
		},
	}
}

func avail(ok bool) string {
	if ok {
		return "available"
	}
	return "missing"
}


func parseEnvFlags(pairs []string) (map[string]string, error) {
	env := map[string]string{}
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("invalid --env %q (want KEY=VALUE)", p)
		}
		env[k] = v
	}
	return env, nil
}

func cmdImport() *cobra.Command {
	var dryRun bool
	var prefix string
	c := &cobra.Command{
		Use:   "import",
		Short: "Import entries from the user crontab into crontask",
		Long: `Read the current user crontab and create disabled crontask entries
for lines that are not already managed by crontask.

Each imported job is stored with its original 5-field cron expression
and marked disabled so nothing is double-scheduled until you enable it.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			out, err := exec.Command("crontab", "-l").Output()
			if err != nil {
				fmt.Println("no crontab to import (empty or missing)")
				return nil
			}

			store := task.NewStore()
			existing, err := store.Load()
			if err != nil {
				return err
			}

			imported := 0
			skipped := 0
			for _, line := range strings.Split(string(out), "\n") {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				if strings.Contains(line, "# crontask:") {
					skipped++
					continue
				}
				fields := strings.Fields(line)
				if len(fields) < 6 {
					continue
				}
				cronExpr := strings.Join(fields[:5], " ")
				command := strings.Join(fields[5:], " ")
				// strip trailing inline comments that aren't our marker
				if i := strings.Index(command, " #"); i >= 0 {
					command = strings.TrimSpace(command[:i])
				}

				name := prefix + slugFromCommand(command, imported+1)
				if _, exists := existing[name]; exists {
					name = fmt.Sprintf("%s-%d", name, imported+1)
				}

				if dryRun {
					fmt.Printf("would import %q schedule=%q cmd=%q\n", name, cronExpr, command)
					imported++
					continue
				}

				t := task.New(name, command, cronExpr)
				t.Enabled = false
				t.Backend = "crontab"
				t.Description = "imported from crontab"
				if err := store.Put(t); err != nil {
					return err
				}
				existing[name] = t
				fmt.Printf("imported %q (disabled) schedule=%q\n", name, cronExpr)
				imported++
			}
			fmt.Printf("done: %d imported, %d already managed\n", imported, skipped)
			return nil
		},
	}
	c.Flags().BoolVar(&dryRun, "dry-run", false, "print what would be imported without writing")
	c.Flags().StringVar(&prefix, "prefix", "imported-", "name prefix for imported tasks")
	return c
}

func slugFromCommand(cmd string, n int) string {
	// crude slug from first token of the command
	tok := strings.Fields(cmd)
	base := "job"
	if len(tok) > 0 {
		base = filepath.Base(tok[0])
		base = strings.TrimSuffix(base, filepath.Ext(base))
	}
	base = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		default:
			return '-'
		}
	}, base)
	base = strings.Trim(base, "-")
	if base == "" {
		base = "job"
	}
	return fmt.Sprintf("%s%d", base, n)
}

func cmdExportCron() *cobra.Command {
	return &cobra.Command{
		Use:   "export-cron",
		Short: "Show managed crontab lines",
		RunE: func(cmd *cobra.Command, args []string) error {
			out, err := exec.Command("crontab", "-l").Output()
			if err != nil {
				return nil
			}
			for _, line := range strings.Split(string(out), "\n") {
				if strings.Contains(line, "# crontask:") {
					fmt.Println(line)
				}
			}
			return nil
		},
	}
}

func cmdVersion() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("crontask %s\n", version)
		},
	}
}
