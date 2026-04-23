# Cronplus

Advanced Cron Task Manager for Linux — built with Go + Cockpit WebUI.

[中文文档](README.zh-CN.md)

## Architecture

```
cronplusd (daemon)
├── Scheduler    Parses cron expressions, triggers tasks
└── Executor Pool  Per-task process isolation, timeout/retry/logging

cronplus (CLI)   Command-line management tool (real-time streaming output, signal forwarding)
```

## Features

- **6-field cron** with seconds support (`sec min hour day month dow`)
- **Per-task process isolation** — each task runs in its own process group
- **Timeout & retry** — SIGTERM → SIGKILL escalation, configurable retry with interval
- **Run ID tracking** — every execution gets a unique ID (`YYMMDD-HHMMSS-xxxx`) for full-chain tracing
- **Manual run** — real-time streaming output, Ctrl+C to terminate, signal forwarding to child process group
- **Multi-user** — run tasks as any system user
- **Cockpit WebUI** — task management, log viewer, daemon log, JSON editor, 9 languages, dark/light theme

## Install

```bash
sudo dpkg -i cronplus_<version>_<arch>.deb
sudo dpkg -i cockpit-cronplus_<version>_<arch>.deb
```

The daemon starts automatically after installation.

## CLI Usage

```bash
cronplus status              # Show daemon & task status
cronplus list                # List all tasks
cronplus run <id>            # Run task immediately (real-time output, Ctrl+C to stop)
cronplus logs [id] [-n 20]   # View execution logs
cronplus reload              # Hot-reload config without restart
cronplus settings show       # View settings
cronplus settings set <k> <v># Update a setting
cronplus settings reset      # Reset to defaults
cronplus export              # Export tasks as JSON
cronplus import <file>       # Import tasks from JSON
cronplus cleanup             # Clean up stuck task entries
cronplus version             # Show version
```

### Manual Run

- Real-time streaming output (stdout + stderr)
- Unique Run ID per execution (`YYMMDD-HHMMSS-xxxx`)
- Ctrl+C sends SIGTERM to child process group
- Timeout auto-termination (SIGTERM → SIGKILL after 5s)
- Results automatically written to task logs

## Web UI (Cockpit Plugin)

- **Task Management** — CRUD, enable/disable, search & filter
- **Manual Run** — dedicated modal with run/stop toggle, real-time output, copy button
- **Task Logs** — pagination, multi-dimensional filters (task/user/trigger/status/date)
- **Daemon Log** — live refresh, level filtering, line limit, clear log
- **Raw Editor** — direct JSON config editing
- **9 Languages** — 中文, English, 日本語, 한국어, Français, Deutsch, Español, Русский, Português
- **Themes** — Dark / Light / Follow System

## Task Configuration

Config file: `/opt/cronplus/tasks.conf` (JSON array)

```json
[
  {
    "id": 1,
    "title": "Daily Backup",
    "command": "bash /opt/backup.sh",
    "schedule": "0 0 2 * * *",
    "enabled": true,
    "run_user": "root",
    "timeout": 3600,
    "max_retries": 3,
    "retry_interval": 60,
    "env_vars": {"PATH": "/usr/local/bin:/usr/bin:/bin"},
    "cwd": "/opt"
  }
]
```

### Schedule Format

6-field cron (with seconds): `sec min hour day month dow`

```
0 * * * * *        Every minute
0 */5 * * * *      Every 5 minutes
0 0 2 * * *        Daily at 2:00 AM
0 0 0 1 * *        1st of every month
0 0 9 * * 1-5      Weekdays at 9:00 AM
*/30 * * * * *     Every 30 seconds
```

Special schedules:

```
@reboot             Run at system startup (supports reboot_delay)
```

### Task Fields

| Field | Type | Description |
|-------|------|-------------|
| id | int | Auto-assigned |
| title | string | Task name |
| command | string | Command to execute (supports base64 encoding) |
| schedule | string | Cron expression or @reboot |
| enabled | bool | Whether enabled |
| run_user | string | Run user, default root |
| cwd | string | Working directory |
| timeout | int | Timeout in seconds, 0=unlimited |
| max_retries | int | Retry count on failure |
| retry_interval | int | Retry interval in seconds |
| max_concurrent | int | Max concurrent instances |
| kill_previous | bool | Kill previous instance on new trigger |
| env_vars | map | Environment variables |
| tags | string | Comma-separated tags |
| log_retention_days | int | Log retention days, 0=unlimited |
| log_max_entries | int | Max log entries per task, 0=unlimited |
| reboot_delay | int | @reboot delay in seconds |

## Run ID Tracking

Every execution gets a unique identifier `YYMMDD-HHMMSS-xxxx` that flows through the entire lifecycle:

```
# Daemon log
[260423-210030-a1f2] Task [1] Daily Backup — started [auto]
[260423-210030-a1f2] Task [1] Daily Backup — ✓ success (1234ms, exit=0, attempt=1) [auto]

# CLI output
▶ [1] Daily Backup [manual] run=260423-210030-a1f2
...
✓ [success] Daily Backup (exit=0, 1234ms) [manual] run=260423-210030-a1f2
```

## Logs

- Daemon log: `/opt/cronplus/logs/cronplus.log` (auto-rotated, configurable size & backup count)
- Task logs: `/opt/cronplus/logs/task_{id}.json` (per-task, max 1000 entries)

### Log Rotation

Daemon logs are checked at startup and rotated when oversized:

```
cronplus.log      Current log
cronplus.log.1    Previous rotation
cronplus.log.2    Older
```

Configurable via settings:
- Max file size (1/5/10/20/50 MB)
- Backup count (1/3/5/10)

## Global Settings

Config file: `/opt/cronplus/settings.json`

```json
{
  "language": "en",
  "theme": "auto",
  "autoRefreshInterval": 15,
  "defaultRunUser": "root",
  "defaultTimeout": 0,
  "defaultMaxRetries": 0,
  "defaultRetryInterval": 60,
  "logPageSize": 20,
  "logMaxBytes": 10485760,
  "logBackupCount": 5,
  "daemonLogLevel": "all",
  "daemonLogLines": 100,
  "daemonLogInterval": 2,
  "daemonLogMaxBytes": 10485760,
  "daemonLogBackupCount": 3
}
```

## Signals

```bash
kill -HUP <pid>     Hot-reload config, clean up stuck tasks
kill -TERM <pid>    Graceful shutdown
kill -USR1 <pid>    Dump running status to log
```

## Directory Structure

```
cockpit-cronplus/
├── build-deb.sh              Build & packaging script
├── VERSION                   Version file
├── cockpit-cronplus/         Cockpit WebUI plugin
│   ├── index.html
│   ├── manifest.json
│   ├── lang/                 i18n (9 languages)
│   └── static/               CSS + JS
└── cronplus/                 Go source code
    ├── cmd/
    │   ├── cronplus/         CLI entry
    │   └── cronplusd/        Daemon entry
    ├── internal/
    │   ├── executor/         Process executor (Run ID, signal forwarding)
    │   ├── scheduler/        Cron scheduler
    │   └── store/            File-based storage
    ├── pkg/model/            Data models
    ├── cronplus.service      systemd unit
    ├── Makefile
    └── go.mod
```

## Build

```bash
# Requires Go 1.23+
cd cronplus && make build

# Package deb (output to dist/)
bash build-deb.sh [amd64|arm64]
```

### GitHub Actions

The project includes a CI workflow (`build.yml`) that:
- Accepts optional manual version input (reads `VERSION` file if empty)
- Builds for amd64 and arm64
- Packages `.deb` files
- Creates GitHub Release with checksums

## License

MIT
