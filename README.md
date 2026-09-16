# crontask

Laptop automations via **crontab** (Linux) and **launchd** (macOS).

## Usage

```bash
crontask doctor

crontask add backup \
  --cmd 'rsync -a ~/Documents /Volumes/Backup' \
  --schedule 'every day at 02:00'

crontask add ping \
  --cmd 'curl -fsS https://hc-ping.com/your-uuid' \
  --schedule 'every 5m'

crontask list
crontask run backup
crontask logs backup
crontask disable backup
crontask enable backup
crontask rm backup
```

## Schedule syntax

Flexible `every …` forms with a **24-hour clock**. No fixed alias list — intervals and times are parsed and translated internally.

| Input | Meaning | Cron (crontab) | launchd |
| ----- | ------- | -------------- | ------- |
| `every 5m` / `every 15min` | every N minutes | `*/N * * * *` | `StartInterval` N×60s |
| `every 2h` / `every 2 hours` | every N hours | `0 */N * * *` | `StartInterval` N×3600s |
| `every 1d` / `every 3 days` | every N days at midnight | `0 0 */N * *` | `StartInterval` N×86400s |
| `every day at 14:30` | daily at 14:30 | `30 14 * * *` | `StartCalendarInterval` Hour/Minute |
| `at 14:30` | same as every day at … | `30 14 * * *` | calendar |
| `every weekday at 09:00` | Mon–Fri | `0 9 * * 1-5` | five calendar entries |
| `every monday at 08:15` | specific weekday | `15 8 * * 1` | calendar + Weekday |
| `@reboot` `@hourly` `@daily` `@weekly` `@monthly` | specials | as-is | interval / calendar / RunAtLoad |
| classic 5-field cron | passed through | as-is | best-effort mapping |

## Commands

| Command | Purpose |
| ------- | ------- |
| `add` | Create + install a task |
| `list` | Show all known tasks + live status |
| `show <name>` | Full JSON details |
| `rm <name>` | Uninstall + delete |
| `enable` / `disable` | Toggle without losing the definition |
| `run <name>` | Execute immediately (records last_run) |
| `logs <name>` | Tail the task log |
| `doctor` | Platform / path / backend health check |
| `export-cron` | Dump managed crontab lines |
| `edit <name>` | Change command/schedule/env/webhooks |
| `import` | Import existing crontab entries (disabled) |
| `version` | Print version |

## Layout

```
main.go                 Cobra CLI entrypoint
internal/
  task/                 Task model, store, runner
  schedule/             every… parser → cron + launchd Spec
  backend/              crontab + launchd adapters
  config/               Paths (XDG / Application Support)
proto/                  Python prototype (reference)
```

## How it works

- Definitions live in `~/.config/crontask/tasks.json` (macOS: `~/Library/Application Support/crontask/`).
- Linux: manages user crontab, each line tagged `# crontask: <name>`.
- macOS: writes a LaunchAgent plist from a structured `schedule.Spec` (`StartInterval` or one/many `StartCalendarInterval` dicts) and uses `launchctl bootstrap` / `bootout`.
- Jobs always execute through `crontask _run <name>` so last_run / last_status are recorded and logs are centralised.

## Status

v0.2.1 — edit, list with last/next run, shell completions; plus Cobra, every… schedules, env/webhooks, import.
