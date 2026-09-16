# crontask

Laptop automations via **crontab** (Linux) and **launchd** (macOS).

## Usage

```bash
crontask doctor

crontask add backup \
  --cmd 'rsync -a ~/Documents /Volumes/Backup' \
  --schedule 'daily at 02:00'

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

| Input                    | Cron expression |
| ------------------------ | --------------- |
| `every 5m` / `every 15m` | `*/5 * * * *`   |
| `every 2h`               | `0 */2 * * *`   |
| `hourly`                 | `0 * * * *`     |
| `daily` / `midnight`     | `0 0 * * *`     |
| `daily at 09:30`         | `30 9 * * *`    |
| `weekdays at 08:00`      | `0 8 * * 1-5`   |
| `@reboot` `@daily` …     | kept as-is      |
| classic 5-field cron     | passed through  |

## Commands

| Command              | Purpose                                |
| -------------------- | -------------------------------------- |
| `add`                | Create + install a task                |
| `list`               | Show all known tasks + live status     |
| `show <name>`        | Full JSON details                      |
| `rm <name>`          | Uninstall + delete                     |
| `enable` / `disable` | Toggle without losing the definition   |
| `run <name>`         | Execute immediately (records last_run) |
| `logs <name>`        | Tail the task log                      |
| `doctor`             | Platform / path / backend health check |
| `export-cron`        | Dump managed crontab lines             |

## Layout

```
cmd/crontask/          CLI entrypoint
internal/
  task/                Task model, store, runner
  schedule/            Human → cron parser
  backend/             crontab + launchd adapters
  config/              Paths (XDG / Application Support)
```

## How it works

- Definitions live in `~/.config/crontask/tasks.json` (macOS: `~/Library/Application Support/crontask/`).
- Linux: manages user crontab, each line tagged `# crontask: <name>`.
- macOS: writes a LaunchAgent plist and uses `launchctl bootstrap` / `bootout`.
- Jobs always execute through `crontask _run <name>` so last_run / last_status are recorded and logs are centralised.

## Status

v0.1.0 — core CLI + crontab backend working. launchd plist generation present; full calendar mapping still minimal.
