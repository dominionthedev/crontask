#!/usr/bin/env python3
"""
crontask - prototype
A simple CLI for laptop automations using crontab (Linux) and launchd (macOS).
This is a working Python prototype to explore the full option surface
before the Go rewrite.
"""

from __future__ import annotations

import argparse
import json
import os
import platform
import re
import shlex
import subprocess
import sys
import tempfile
import time
from dataclasses import asdict, dataclass, field
from datetime import datetime, timezone
from pathlib import Path


def utcnow_iso() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


# ---------------------------------------------------------------------------
# Paths & constants
# ---------------------------------------------------------------------------

APP_NAME = "crontask"
VERSION = "0.1.0-proto"


def get_config_dir() -> Path:
    if platform.system() == "Darwin":
        base = Path.home() / "Library" / "Application Support" / APP_NAME
    else:
        xdg = os.environ.get("XDG_CONFIG_HOME")
        base = Path(xdg) if xdg else Path.home() / ".config"
        base = base / APP_NAME
    base.mkdir(parents=True, exist_ok=True)
    return base


def get_data_dir() -> Path:
    d = get_config_dir() / "data"
    d.mkdir(parents=True, exist_ok=True)
    return d


def get_log_dir() -> Path:
    d = get_config_dir() / "logs"
    d.mkdir(parents=True, exist_ok=True)
    return d


TASKS_FILE = get_config_dir() / "tasks.json"
MARKER_PREFIX = f"# {APP_NAME}:"


# ---------------------------------------------------------------------------
# Data model
# ---------------------------------------------------------------------------


@dataclass
class Task:
    name: str
    command: str
    schedule: str  # human or cron expression
    backend: str = "auto"  # auto | crontab | launchd
    enabled: bool = True
    description: str = ""
    working_dir: str = ""
    shell: str = "/bin/bash"
    env: dict[str, str] = field(default_factory=dict)
    log_stdout: bool = True
    log_stderr: bool = True
    notify_on_failure: bool = False
    webhook_success: str = ""
    webhook_failure: str = ""
    created_at: str = field(default_factory=utcnow_iso)
    updated_at: str = field(default_factory=utcnow_iso)
    last_run: str | None = None
    last_status: int | None = None
    # Internal: generated identifiers
    crontab_marker: str = ""
    launchd_label: str = ""

    def __post_init__(self):
        if not self.crontab_marker:
            self.crontab_marker = f"{MARKER_PREFIX} {self.name}"
        if not self.launchd_label:
            # launchd labels must be reverse-dns style
            safe = re.sub(r"[^a-zA-Z0-9._-]", "-", self.name.lower())
            self.launchd_label = f"com.crontask.{safe}"


# ---------------------------------------------------------------------------
# Persistence
# ---------------------------------------------------------------------------


def load_tasks() -> dict[str, Task]:
    if not TASKS_FILE.exists():
        return {}
    try:
        raw = json.loads(TASKS_FILE.read_text())
        return {k: Task(**v) for k, v in raw.items()}
    except Exception as e:
        print(f"warning: could not load tasks file: {e}", file=sys.stderr)
        return {}


def save_tasks(tasks: dict[str, Task]) -> None:
    data = {k: asdict(v) for k, v in tasks.items()}
    TASKS_FILE.write_text(json.dumps(data, indent=2, sort_keys=True))


# ---------------------------------------------------------------------------
# Schedule helpers (very small subset for the prototype)
# ---------------------------------------------------------------------------

# Map of friendly shortcuts → cron expression
FRIENDLY = {
    "every minute": "* * * * *",
    "every 5m": "*/5 * * * *",
    "every 10m": "*/10 * * * *",
    "every 15m": "*/15 * * * *",
    "every 30m": "*/30 * * * *",
    "hourly": "0 * * * *",
    "every hour": "0 * * * *",
    "daily": "0 0 * * *",
    "every day": "0 0 * * *",
    "midnight": "0 0 * * *",
    "weekly": "0 0 * * 0",
    "monthly": "0 0 1 * *",
    "@reboot": "@reboot",
    "@hourly": "@hourly",
    "@daily": "@daily",
    "@weekly": "@weekly",
    "@monthly": "@monthly",
}


def parse_schedule(s: str) -> str:
    """Return a cron-compatible expression (or keep @reboot etc.)."""
    s = s.strip().lower()
    if s in FRIENDLY:
        return FRIENDLY[s]

    # "every Nm" / "every Nh" / "every Nd"
    m = re.match(r"every\s+(\d+)\s*(m|min|minutes?|h|hours?|d|days?)", s)
    if m:
        n, unit = int(m.group(1)), m.group(2)[0]
        if unit == "m":
            if n < 1 or n > 59:
                raise ValueError("minutes must be 1-59")
            return f"*/{n} * * * *"
        if unit == "h":
            if n < 1 or n > 23:
                raise ValueError("hours must be 1-23")
            return f"0 */{n} * * *"
        if unit == "d":
            return f"0 0 */{n} * *"

    # "daily at HH:MM" or "at HH:MM"
    m = re.match(r"(?:daily\s+)?at\s+(\d{1,2}):(\d{2})", s)
    if m:
        h, mi = int(m.group(1)), int(m.group(2))
        if not (0 <= h <= 23 and 0 <= mi <= 59):
            raise ValueError("invalid time")
        return f"{mi} {h} * * *"

    # "weekdays at HH:MM"
    m = re.match(r"weekdays?\s+at\s+(\d{1,2}):(\d{2})", s)
    if m:
        h, mi = int(m.group(1)), int(m.group(2))
        return f"{mi} {h} * * 1-5"

    # Assume it is already a valid cron expression
    parts = s.split()
    if len(parts) == 5 or s.startswith("@"):
        return s

    raise ValueError(
        f"unrecognised schedule '{s}'. "
        "Try: every 15m | daily at 09:00 | weekdays at 08:30 | classic cron"
    )


def human_schedule(cron: str) -> str:
    """Best-effort reverse mapping for display."""
    for k, v in FRIENDLY.items():
        if v == cron:
            return k
    return cron


# ---------------------------------------------------------------------------
# Platform detection & backends
# ---------------------------------------------------------------------------


def detect_backend(preferred: str = "auto") -> str:
    system = platform.system()
    if preferred == "crontab":
        return "crontab"
    if preferred == "launchd":
        return "launchd"
    # auto
    if system == "Darwin":
        # Prefer launchd on macOS
        return "launchd"
    return "crontab"


def is_launchd_available() -> bool:
    return platform.system() == "Darwin" and shutil_which("launchctl") is not None


def shutil_which(cmd: str) -> str | None:
    from shutil import which

    return which(cmd)


# ---- crontab backend -------------------------------------------------------


def crontab_get() -> list[str]:
    try:
        out = subprocess.check_output(
            ["crontab", "-l"], stderr=subprocess.DEVNULL, text=True
        )
        return out.splitlines()
    except subprocess.CalledProcessError:
        return []


def crontab_set(lines: list[str]) -> None:
    content = "\n".join(lines) + "\n"
    with tempfile.NamedTemporaryFile("w", delete=False) as f:
        f.write(content)
        tmp = f.name
    try:
        subprocess.check_call(["crontab", tmp])
    finally:
        os.unlink(tmp)


def crontab_add_task(task: Task, cron_expr: str) -> None:
    lines = crontab_get()
    # Remove any previous entry for this task
    lines = [l for l in lines if task.crontab_marker not in l]

    log_redirect = ""
    if task.log_stdout or task.log_stderr:
        log_file = get_log_dir() / f"{task.name}.log"
        if task.log_stdout and task.log_stderr:
            log_redirect = f" >> {log_file} 2>&1"
        elif task.log_stdout:
            log_redirect = f" >> {log_file}"
        else:
            log_redirect = f" 2>> {log_file}"

    # Build the command line that will actually run
    # We wrap through our own runner so we can record last_run etc.
    runner = (
        f"{sys.executable} {Path(__file__).resolve()} _run {shlex.quote(task.name)}"
    )
    entry = f"{cron_expr} {runner} {log_redirect}  {task.crontab_marker}"
    lines.append(entry)
    crontab_set(lines)


def crontab_remove_task(task: Task) -> None:
    lines = crontab_get()
    new_lines = [l for l in lines if task.crontab_marker not in l]
    if len(new_lines) != len(lines):
        crontab_set(new_lines)


def crontab_list_markers() -> list[str]:
    lines = crontab_get()
    markers = []
    for l in lines:
        if MARKER_PREFIX in l:
            # extract name after marker
            idx = l.find(MARKER_PREFIX)
            name = l[idx + len(MARKER_PREFIX) :].strip()
            markers.append(name)
    return markers


# ---- launchd backend (macOS only) ------------------------------------------


def launchd_plist_path(task: Task) -> Path:
    agents = Path.home() / "Library" / "LaunchAgents"
    agents.mkdir(parents=True, exist_ok=True)
    return agents / f"{task.launchd_label}.plist"


def launchd_generate_plist(task: Task, cron_expr: str) -> str:
    """Very simplified: convert common cron to StartCalendarInterval or StartInterval.
    Full cron → calendar conversion is non-trivial; this prototype handles a few cases.
    """
    # For prototype we mostly emit StartInterval for "every Xm" and
    # a single StartCalendarInterval for daily/at times.
    label = task.launchd_label
    prog = [task.shell, "-c", task.command]
    if task.working_dir:
        # launchd has WorkingDirectory key
        pass

    # Extremely simplified mapping
    interval = None
    calendar = None

    if cron_expr.startswith("*/") and cron_expr.endswith(" * * * *"):
        # every N minutes
        try:
            n = int(cron_expr.split()[0][2:])
            interval = n * 60
        except Exception:
            pass
    elif cron_expr in ("0 * * * *", "@hourly"):
        interval = 3600
    elif cron_expr in ("0 0 * * *", "@daily"):
        calendar = {"Hour": 0, "Minute": 0}
    else:
        # fallback: treat as daily at midnight for prototype
        calendar = {"Hour": 0, "Minute": 0}

    # Build minimal plist as string (avoid dependency on plistlib for clarity)
    lines = [
        '<?xml version="1.0" encoding="UTF-8"?>',
        '<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">',
        '<plist version="1.0">',
        "<dict>",
        f"  <key>Label</key><string>{label}</string>",
        "  <key>ProgramArguments</key>",
        "  <array>",
    ]
    for p in prog:
        lines.append(f"    <string>{p}</string>")
    lines.append("  </array>")

    if task.working_dir:
        lines.append(
            f"  <key>WorkingDirectory</key><string>{task.working_dir}</string>"
        )

    if interval is not None:
        lines.append(f"  <key>StartInterval</key><integer>{interval}</integer>")
    if calendar is not None:
        lines.append("  <key>StartCalendarInterval</key>")
        lines.append("  <dict>")
        for k, v in calendar.items():
            lines.append(f"    <key>{k}</key><integer>{v}</integer>")
        lines.append("  </dict>")

    # StandardOutPath / StandardErrorPath
    log_file = get_log_dir() / f"{task.name}.log"
    lines.append(f"  <key>StandardOutPath</key><string>{log_file}</string>")
    lines.append(f"  <key>StandardErrorPath</key><string>{log_file}</string>")

    lines += [
        "  <key>RunAtLoad</key><false/>",
        "</dict>",
        "</plist>",
        "",
    ]
    return "\n".join(lines)


def launchd_load(task: Task) -> None:
    plist = launchd_plist_path(task)
    # modern: launchctl bootstrap gui/$(id -u) plist
    # legacy: launchctl load plist
    try:
        uid = os.getuid()
        subprocess.check_call(
            ["launchctl", "bootstrap", f"gui/{uid}", str(plist)],
            stderr=subprocess.DEVNULL,
        )
    except subprocess.CalledProcessError:
        # fall back
        subprocess.check_call(["launchctl", "load", str(plist)])


def launchd_unload(task: Task) -> None:
    plist = launchd_plist_path(task)
    try:
        uid = os.getuid()
        subprocess.check_call(
            ["launchctl", "bootout", f"gui/{uid}", str(plist)],
            stderr=subprocess.DEVNULL,
        )
    except subprocess.CalledProcessError:
        try:
            subprocess.check_call(["launchctl", "unload", str(plist)])
        except subprocess.CalledProcessError:
            pass
    if plist.exists():
        plist.unlink()


# ---------------------------------------------------------------------------
# Core operations
# ---------------------------------------------------------------------------


def cmd_add(args: argparse.Namespace) -> None:
    tasks = load_tasks()
    name = args.name
    if name in tasks and not args.force:
        print(
            f"error: task '{name}' already exists (use --force to overwrite)",
            file=sys.stderr,
        )
        sys.exit(1)

    try:
        cron_expr = parse_schedule(args.schedule)
    except ValueError as e:
        print(f"error: {e}", file=sys.stderr)
        sys.exit(1)

    backend = detect_backend(args.backend)

    task = Task(
        name=name,
        command=args.command,
        schedule=args.schedule,
        backend=backend,
        description=args.description or "",
        working_dir=args.cwd or "",
        enabled=not args.disabled,
    )

    # Persist first
    tasks[name] = task
    save_tasks(tasks)

    if not task.enabled:
        print(f"created disabled task '{name}' (schedule: {cron_expr})")
        return

    # Install into backend
    if backend == "crontab":
        crontab_add_task(task, cron_expr)
        print(f"added '{name}' → crontab ({cron_expr})")
    elif backend == "launchd":
        if not is_launchd_available():
            print("error: launchd not available on this platform", file=sys.stderr)
            sys.exit(1)
        plist_content = launchd_generate_plist(task, cron_expr)
        path = launchd_plist_path(task)
        path.write_text(plist_content)
        launchd_load(task)
        print(f"added '{name}' → launchd ({task.launchd_label})")
    else:
        print(f"error: unknown backend {backend}", file=sys.stderr)
        sys.exit(1)


def cmd_list(args: argparse.Namespace) -> None:
    tasks = load_tasks()
    if not tasks:
        print("no tasks defined")
        return

    # Also show what is actually in crontab for sanity
    live_markers = (
        set(crontab_list_markers()) if detect_backend() == "crontab" else set()
    )

    print(f"{'NAME':<20} {'ENABLED':<8} {'BACKEND':<10} {'SCHEDULE':<22} {'COMMAND'}")
    print("-" * 90)
    for name, t in sorted(tasks.items()):
        en = "yes" if t.enabled else "no"
        # Prefer the original human string the user typed
        sched = t.schedule
        cmd_short = (t.command[:40] + "…") if len(t.command) > 40 else t.command
        live = ""
        if t.backend == "crontab" and name in live_markers:
            live = " [live]"
        print(f"{name:<20} {en:<8} {t.backend:<10} {sched:<22} {cmd_short}{live}")


def cmd_show(args: argparse.Namespace) -> None:
    tasks = load_tasks()
    t = tasks.get(args.name)
    if not t:
        print(f"error: unknown task '{args.name}'", file=sys.stderr)
        sys.exit(1)
    print(json.dumps(asdict(t), indent=2))


def cmd_rm(args: argparse.Namespace) -> None:
    tasks = load_tasks()
    t = tasks.get(args.name)
    if not t:
        print(f"error: unknown task '{args.name}'", file=sys.stderr)
        sys.exit(1)

    if t.backend == "crontab":
        crontab_remove_task(t)
    elif t.backend == "launchd":
        launchd_unload(t)

    del tasks[args.name]
    save_tasks(tasks)
    print(f"removed '{args.name}'")


def cmd_enable(args: argparse.Namespace) -> None:
    tasks = load_tasks()
    t = tasks.get(args.name)
    if not t:
        print(f"error: unknown task '{args.name}'", file=sys.stderr)
        sys.exit(1)
    if t.enabled:
        print(f"'{args.name}' is already enabled")
        return
    t.enabled = True
    t.updated_at = utcnow_iso()
    cron_expr = parse_schedule(t.schedule)
    if t.backend == "crontab":
        crontab_add_task(t, cron_expr)
    elif t.backend == "launchd":
        plist_content = launchd_generate_plist(t, cron_expr)
        launchd_plist_path(t).write_text(plist_content)
        launchd_load(t)
    save_tasks(tasks)
    print(f"enabled '{args.name}'")


def cmd_disable(args: argparse.Namespace) -> None:
    tasks = load_tasks()
    t = tasks.get(args.name)
    if not t:
        print(f"error: unknown task '{args.name}'", file=sys.stderr)
        sys.exit(1)
    if not t.enabled:
        print(f"'{args.name}' is already disabled")
        return
    t.enabled = False
    t.updated_at = utcnow_iso()
    if t.backend == "crontab":
        crontab_remove_task(t)
    elif t.backend == "launchd":
        launchd_unload(t)
    save_tasks(tasks)
    print(f"disabled '{args.name}'")


def cmd_run(args: argparse.Namespace) -> None:
    """Run a task immediately (and record last_run)."""
    tasks = load_tasks()
    t = tasks.get(args.name)
    if not t:
        print(f"error: unknown task '{args.name}'", file=sys.stderr)
        sys.exit(1)
    _execute_task(t, record=True)


def _execute_task(task: Task, record: bool = True) -> int:
    """Actually run the command. Used both by 'run' and by the internal _run entrypoint."""
    env = os.environ.copy()
    env.update(task.env)
    cwd = task.working_dir or None

    log_file = get_log_dir() / f"{task.name}.log"
    print(f"→ running '{task.name}': {task.command}")

    start = time.time()
    try:
        # Always capture so we can both log and (for interactive runs) print
        proc = subprocess.run(
            task.command,
            shell=True,
            executable=task.shell,
            cwd=cwd,
            env=env,
            capture_output=True,
            text=True,
        )
        status = proc.returncode
        out = proc.stdout or ""
        err = proc.stderr or ""

        with open(log_file, "a") as lf:
            lf.write(f"\n--- {utcnow_iso()} ---\n")
            if out:
                lf.write(out)
                if not out.endswith("\n"):
                    lf.write("\n")
            if err:
                lf.write(err)
                if not err.endswith("\n"):
                    lf.write("\n")

        # Interactive visibility
        if out:
            print(out, end="" if out.endswith("\n") else "\n")
        if err:
            print(err, end="" if err.endswith("\n") else "\n", file=sys.stderr)

    except Exception as e:
        print(f"error executing: {e}", file=sys.stderr)
        status = 1

    duration = time.time() - start
    print(f"← finished status={status} ({duration:.1f}s)")

    if record:
        tasks = load_tasks()
        if task.name in tasks:
            tasks[task.name].last_run = utcnow_iso()
            tasks[task.name].last_status = status
            save_tasks(tasks)

    # Optional webhooks (prototype just prints)
    if status == 0 and task.webhook_success:
        print(f"(would POST success to {task.webhook_success})")
    if status != 0 and task.webhook_failure:
        print(f"(would POST failure to {task.webhook_failure})")

    return status


def cmd_internal_run(args: argparse.Namespace) -> None:
    """Called from crontab / launchd. Quiet by default."""
    tasks = load_tasks()
    t = tasks.get(args.name)
    if not t:
        sys.exit(1)
    status = _execute_task(t, record=True)
    sys.exit(status)


def cmd_logs(args: argparse.Namespace) -> None:
    log_file = get_log_dir() / f"{args.name}.log"
    if not log_file.exists():
        print(f"no log file for '{args.name}'")
        return
    # tail
    lines = log_file.read_text().splitlines()
    n = args.tail or 40
    for line in lines[-n:]:
        print(line)


def cmd_doctor(args: argparse.Namespace) -> None:
    print(f"crontask {VERSION}")
    print(f"platform   : {platform.system()} {platform.release()}")
    print(f"config dir : {get_config_dir()}")
    print(
        f"tasks file : {TASKS_FILE} ({'exists' if TASKS_FILE.exists() else 'missing'})"
    )
    print(f"log dir    : {get_log_dir()}")
    print(f"backend    : {detect_backend()}")
    print(f"crontab    : {'available' if shutil_which('crontab') else 'missing'}")
    print(f"launchctl  : {'available' if shutil_which('launchctl') else 'missing'}")

    tasks = load_tasks()
    print(f"tasks      : {len(tasks)}")
    if detect_backend() == "crontab":
        live = crontab_list_markers()
        print(f"live crontab markers: {live or '(none)'}")


def cmd_export_cron(args: argparse.Namespace) -> None:
    """Dump the effective crontab lines that crontask manages."""
    lines = crontab_get()
    for l in lines:
        if MARKER_PREFIX in l:
            print(l)


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------


def build_parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(
        prog=APP_NAME,
        description="Laptop automations via crontab & launchd (Python prototype)",
    )
    p.add_argument("--version", action="version", version=f"%(prog)s {VERSION}")

    sub = p.add_subparsers(dest="command", required=True)

    # add
    add = sub.add_parser("add", help="create a new scheduled task")
    add.add_argument("name", help="unique task name")
    add.add_argument(
        "--cmd", "--command", dest="command", required=True, help="command to run"
    )
    add.add_argument(
        "--schedule", "-s", required=True, help="every 15m | daily at 09:00 | cron expr"
    )
    add.add_argument(
        "--backend", choices=["auto", "crontab", "launchd"], default="auto"
    )
    add.add_argument("--description", "-d", default="")
    add.add_argument("--cwd", default="", help="working directory")
    add.add_argument(
        "--disabled", action="store_true", help="create but do not install yet"
    )
    add.add_argument("--force", "-f", action="store_true")
    add.set_defaults(func=cmd_add)

    # list
    lst = sub.add_parser("list", help="list all tasks")
    lst.set_defaults(func=cmd_list)

    # show
    show = sub.add_parser("show", help="show full details of a task")
    show.add_argument("name")
    show.set_defaults(func=cmd_show)

    # rm
    rm = sub.add_parser("rm", help="remove a task")
    rm.add_argument("name")
    rm.set_defaults(func=cmd_rm)

    # enable / disable
    en = sub.add_parser("enable", help="enable a task")
    en.add_argument("name")
    en.set_defaults(func=cmd_enable)

    dis = sub.add_parser("disable", help="disable a task")
    dis.add_argument("name")
    dis.set_defaults(func=cmd_disable)

    # run
    run = sub.add_parser("run", help="run a task immediately")
    run.add_argument("name")
    run.set_defaults(func=cmd_run)

    # logs
    logs = sub.add_parser("logs", help="show recent logs for a task")
    logs.add_argument("name")
    logs.add_argument("--tail", "-n", type=int, default=40)
    logs.set_defaults(func=cmd_logs)

    # doctor
    doc = sub.add_parser("doctor", help="health check / debug info")
    doc.set_defaults(func=cmd_doctor)

    # internal runner (called by crontab)
    internal = sub.add_parser("_run", help=argparse.SUPPRESS)
    internal.add_argument("name")
    internal.set_defaults(func=cmd_internal_run)

    # export
    exp = sub.add_parser("export-cron", help="show managed crontab lines")
    exp.set_defaults(func=cmd_export_cron)

    return p


def main() -> None:
    parser = build_parser()
    args = parser.parse_args()
    args.func(args)


if __name__ == "__main__":
    main()
