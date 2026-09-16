package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/dominionthedev/crontask/internal/backend"
	"github.com/dominionthedev/crontask/internal/config"
	"github.com/dominionthedev/crontask/internal/schedule"
	"github.com/dominionthedev/crontask/internal/task"
)

// newFlagSet creates a flag set that doesn't call os.Exit on error.
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return fs
}

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "add":
		err = cmdAdd(args)
	case "list", "ls":
		err = cmdList(args)
	case "show":
		err = cmdShow(args)
	case "rm", "remove", "delete":
		err = cmdRm(args)
	case "enable":
		err = cmdEnable(args)
	case "disable":
		err = cmdDisable(args)
	case "run":
		err = cmdRun(args)
	case "logs":
		err = cmdLogs(args)
	case "doctor":
		err = cmdDoctor(args)
	case "export-cron":
		err = cmdExportCron(args)
	case "_run": // internal entrypoint from crontab / launchd
		err = cmdInternalRun(args)
	case "version", "--version", "-v":
		fmt.Printf("crontask %s\n", version)
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		usage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `crontask %s — laptop automations via crontab & launchd

Usage:
  crontask <command> [arguments]

Commands:
  add       create a new scheduled task
  list      list all tasks
  show      show full details of a task
  rm        remove a task
  enable    enable a task
  disable   disable a task
  run       run a task immediately
  logs      show recent logs for a task
  doctor    health check / debug info
  version   print version

Examples:
  crontask add backup --cmd 'rsync -a ~/Docs /Backup' --schedule 'daily at 02:00'
  crontask add ping  --cmd 'curl -fsS https://hc-ping.com/uuid' --schedule 'every 5m'
  crontask list
  crontask run backup
  crontask logs backup
`, version)
}

// ---------------------------------------------------------------------------
// add
// ---------------------------------------------------------------------------

// splitNameAndFlags pulls the first non-flag argument as the task name
// and returns the remaining args for flag parsing.
func splitNameAndFlags(args []string) (name string, flagArgs []string) {
	flagArgs = make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			flagArgs = append(flagArgs, a)
			// if this flag expects a value and next arg doesn't look like a flag, keep it
			if !strings.Contains(a, "=") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				// known boolean flags that take no value
				boolFlags := map[string]bool{"--disabled": true, "--force": true, "-f": true}
				if !boolFlags[a] {
					i++
					flagArgs = append(flagArgs, args[i])
				}
			}
			continue
		}
		if name == "" {
			name = a
		} else {
			flagArgs = append(flagArgs, a)
		}
	}
	return name, flagArgs
}

func cmdAdd(args []string) error {
	// Allow: crontask add <name> --cmd ...  OR  crontask add --cmd ... <name>
	name, flagArgs := splitNameAndFlags(args)
	if name == "" {
		return fmt.Errorf("usage: crontask add <name> --cmd '...' --schedule '...'")
	}

	fs := newFlagSet("add")
	cmdStr := fs.String("cmd", "", "command to run")
	sched := fs.String("schedule", "", "every 15m | daily at 09:00 | cron expr")
	backendName := fs.String("backend", "auto", "auto | crontab | launchd")
	desc := fs.String("description", "", "optional description")
	cwd := fs.String("cwd", "", "working directory")
	disabled := fs.Bool("disabled", false, "create but do not install yet")
	force := fs.Bool("force", false, "overwrite existing task")

	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if *cmdStr == "" || *sched == "" {
		return fmt.Errorf("--cmd and --schedule are required")
	}

	cronExpr, err := schedule.Parse(*sched)
	if err != nil {
		return err
	}

	store := task.NewStore()
	tasks, err := store.Load()
	if err != nil {
		return err
	}
	if _, exists := tasks[name]; exists && !*force {
		return fmt.Errorf("task %q already exists (use --force to overwrite)", name)
	}

	t := task.New(name, *cmdStr, *sched)
	t.Backend = *backendName
	t.Description = *desc
	t.WorkingDir = *cwd
	t.Enabled = !*disabled

	// Resolve concrete backend
	b := backend.Detect(t.Backend)
	t.Backend = b.Name()

	if err := store.Put(t); err != nil {
		return err
	}

	if !t.Enabled {
		fmt.Printf("created disabled task %q (schedule: %s)\n", name, cronExpr)
		return nil
	}

	if err := b.Install(t, cronExpr); err != nil {
		return err
	}
	fmt.Printf("added %q → %s (%s)\n", name, b.Name(), cronExpr)
	return nil
}

// ---------------------------------------------------------------------------
// list
// ---------------------------------------------------------------------------

func cmdList(_ []string) error {
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

	fmt.Printf("%-20s %-8s %-10s %-22s %s\n", "NAME", "ENABLED", "BACKEND", "SCHEDULE", "COMMAND")
	fmt.Println(strings.Repeat("-", 90))

	// stable order
	names := make([]string, 0, len(tasks))
	for n := range tasks {
		names = append(names, n)
	}
	// simple sort
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
		fmt.Printf("%-20s %-8s %-10s %-22s %s%s\n", name, en, t.Backend, t.Schedule, cmdShort, liveMark)
	}
	return nil
}

// ---------------------------------------------------------------------------
// show
// ---------------------------------------------------------------------------

func cmdShow(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: crontask show <name>")
	}
	store := task.NewStore()
	t, err := store.Get(args[0])
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(t)
}

// ---------------------------------------------------------------------------
// rm
// ---------------------------------------------------------------------------

func cmdRm(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: crontask rm <name>")
	}
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
}

// ---------------------------------------------------------------------------
// enable / disable
// ---------------------------------------------------------------------------

func cmdEnable(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: crontask enable <name>")
	}
	store := task.NewStore()
	t, err := store.Get(args[0])
	if err != nil {
		return err
	}
	if t.Enabled {
		fmt.Printf("%q is already enabled\n", t.Name)
		return nil
	}

	cronExpr, err := schedule.Parse(t.Schedule)
	if err != nil {
		return err
	}
	b := backend.Detect(t.Backend)
	if err := b.Install(t, cronExpr); err != nil {
		return err
	}
	t.Enabled = true
	t.UpdatedAt = time.Now().UTC()
	if err := store.Put(t); err != nil {
		return err
	}
	fmt.Printf("enabled %q\n", t.Name)
	return nil
}

func cmdDisable(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: crontask disable <name>")
	}
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
}

// ---------------------------------------------------------------------------
// run / _run
// ---------------------------------------------------------------------------

func cmdRun(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: crontask run <name>")
	}
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
}

func cmdInternalRun(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("internal: missing task name")
	}
	store := task.NewStore()
	t, err := store.Get(args[0])
	if err != nil {
		return err
	}
	status, _ := task.Run(t, true)
	os.Exit(status)
	return nil
}

// ---------------------------------------------------------------------------
// logs
// ---------------------------------------------------------------------------

func cmdLogs(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: crontask logs <name> [--tail N]")
	}
	name := args[0]
	tail := 40
	for i := 1; i < len(args); i++ {
		if (args[i] == "--tail" || args[i] == "-n") && i+1 < len(args) {
			fmt.Sscanf(args[i+1], "%d", &tail)
			i++
		}
	}

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
}

// ---------------------------------------------------------------------------
// doctor
// ---------------------------------------------------------------------------

func cmdDoctor(_ []string) error {
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
}

func avail(ok bool) string {
	if ok {
		return "available"
	}
	return "missing"
}

// ---------------------------------------------------------------------------
// export-cron
// ---------------------------------------------------------------------------

func cmdExportCron(_ []string) error {
	out, err := exec.Command("crontab", "-l").Output()
	if err != nil {
		return nil // empty crontab is fine
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "# crontask:") {
			fmt.Println(line)
		}
	}
	return nil
}
