# Cronplus

Advanced Cron Task Manager for Linux — built with Go + Cockpit WebUI.
<img width="2149" height="1199" alt="image" src="https://github.com/user-attachments/assets/72976321-d291-4400-a5b8-80f0fee3bc31" />
<img width="2150" height="1197" alt="image" src="https://github.com/user-attachments/assets/beec91c9-9e50-4832-8cbc-1dc9d8111b3e" />
<img width="2153" height="1273" alt="image" src="https://github.com/user-attachments/assets/887763d0-63cc-4e1e-a435-268f74f8f9b0" />
<img width="2202" height="1303" alt="image" src="https://github.com/user-attachments/assets/9a5da631-fb2f-4495-90aa-9ced3fe3ebc1" />
<img width="2183" height="1297" alt="image" src="https://github.com/user-attachments/assets/e129dee0-4d10-49bc-aa83-25e5498df5fd" />


[中文文档](README.zh-CN.md)

## Architecture

```
cronplusd (daemon)
├── Scheduler    Parses cron expressions, triggers tasks (anchor-based step interval)
└── Executor Pool  Per-task process isolation, timeout/retry/logging

cronplus (CLI)   Command-line management tool (real-time streaming output, signal forwarding)
```

## Features

- **6-field cron** with seconds support (`sec min hour day month dow`)
- **Anchor-based step interval** — edit the first predicted run time as anchor, subsequent times calculated from anchor + interval
- **Per-task process isolation** — each task runs in its own process group
- **Timeout & retry** — SIGTERM → SIGKILL escalation, configurable retry with interval
- **Run ID tracking** — every execution gets a unique ID (`YYMMDD-HHMMSS-xxxx`) for full-chain tracing
- **Manual run** — real-time streaming output, Ctrl+C to terminate, signal forwarding to child process group
- **Multi-user** — run tasks as any system user
- **Cockpit WebUI** — task management, log viewer, daemon log, JSON editor, 9 languages, dark/light theme, SVG vector icons
- **Security** — environment variable allowlist, atomic config writes, file permission enforcement (0600)

## Install

```bash
sudo dpkg -i cronplus_<version>_<arch>.deb
sudo dpkg -i cockpit-cronplus_<version>_<arch>.deb
```

The daemon starts automatically after installation.

## CLI Usage

```bash
cronplus status              # Show daemon & task status (incl. running tasks)
cronplus list                # List all tasks
cronplus ps [--json]         # Show running tasks (from the daemon control socket)
cronplus run <id>            # Run task immediately (real-time output, Ctrl+C to stop)
cronplus stop <id>           # Stop a running task (no argument stops the daemon)
cronplus logs [id] [-n 20]   # View logs (--no-output = summaries, --run-id = one entry)
cronplus prune [filters]     # Delete selected log entries (--task/--status/--user/--trigger/--before/--keep, --dry-run)
cronplus reload              # Hot-reload config without restart
cronplus settings show       # View settings
cronplus settings set <k> <v># Update a setting
cronplus settings reset      # Reset to defaults
cronplus export              # Export tasks as JSON
cronplus import <file>       # Import tasks from JSON
cronplus cleanup             # Apply retention rules and remove stale temp files
cronplus version             # Show version
```

### Manual Run

- Shares the daemon's executor (`executor.RunCommand`): identical timeout, env
  allow-list, output truncation and log writing
- The run shows up in `cronplus ps` and counts towards the concurrency limit
- Real-time streaming output (stdout + stderr merged)
- Unique Run ID per execution (`#<task>-<user>-manual-xxxx`)
- Ctrl+C sends SIGTERM to the child process group (SIGKILL after 5s) and still
  writes the log entry
- Timeout auto-termination (SIGTERM → SIGKILL after 5s)
- Results automatically written to task logs — the CLI is the only writer, the
  WebUI no longer assembles log entries of its own
- **Exit status is passed through**: a failing task makes `cronplus run` exit
  with the task's own code, so it can be used in scripts
- **No automatic retries**: a manual run executes once; `max_retries` applies to
  scheduled runs only

### Command behaviour

- `cronplus logs <id>`: exits 1 when the task does not exist, prints `No logs`
  when it exists but has never run
- `cronplus prune --before/--after`: accepts `YYYY-MM-DD HH:MM:SS`,
  `YYYY-MM-DDTHH:MM:SS` or `YYYY-MM-DD` and rejects anything else; refuses to run
  without a filter
- `cronplus reload`: notifies the daemon over the control socket first, so it
  works without systemd (containers, hand-started daemon); falls back to `SIGHUP`

## Web UI (Cockpit Plugin)

- **UI Language** — follows the Cockpit session language by default (previously ignored in favour of the browser locale), pinnable to any of the 9 bundled languages
- **Task Management** — CRUD, enable/disable, search & filter, paging (`taskPageSize`)
- **Manual Run** — dedicated modal with run/stop toggle, real-time output, copy button; executed by `cronplus run`
- **Running State** — task cards show a running badge and a stop button (data from `cronplus ps`)
- **Task Logs** — pagination, multi-dimensional filters (task/user/trigger/status/date), output loaded only on expand
- **Log Cleanup** — delete by age/task/status/user/kept-entries, with the preview count coming from `cronplus prune --dry-run`
- **Daemon Log** — live refresh, level filtering, line limit, clear log
- **Raw Editor** — direct JSON config editing
- **Anchor Editing** — edit the first predicted run time as anchor for step patterns (`*/N`), with auto-sync of fixed cron fields
- **9 Languages** — 中文, English, 日本語, 한국어, Français, Deutsch, Español, Русский, Português
- **Themes** — Dark / Light / Follow System
- **SVG Icons** — all UI icons use vector graphics for crisp rendering at any resolution

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

### Anchor Base Time

For step patterns (`*/N`), the first predicted run time can be edited as an anchor. Subsequent run times are calculated from `anchor + N * interval`, ensuring consistent intervals across month/year boundaries.

Example: `0 0 */3 * * *` (every 3 hours) — edit the first run time to `2026-05-19 08:00:00`, and subsequent runs will be at `11:00`, `14:00`, `17:00`, etc., always exactly 3 hours apart.

When confirming the anchor, fixed cron fields (plain numbers like `8` in `0 0 8 */3 * *`) are automatically synced to match the edited time.

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
| max_runs | int | Auto-disable after N runs, 0=unlimited |
| env_vars | map | Environment variables |
| tags | string | Comma-separated tags |
| comment | string | Task description |
| log_retention_days | int | Log retention days, 0=unlimited |
| log_max_entries | int | Max log entries per task, 0=unlimited |
| reboot_delay | int | @reboot delay in seconds |
| cron_base_time | string | Anchor time for step patterns, format `YYYY-MM-DD HH:mm:ss` |

## Run ID Tracking

Every execution gets a unique identifier `YYMMDD-HHMMSS-xxxx` that flows through the entire lifecycle:

```
# Daemon log
[260423-210030-a1f2] Task [1] Daily Backup — started [auto]
[260423-210030-a1f2] Task [1] Daily Backup — ✓ success (1.2s, exit=0, attempt=1) [auto]

# CLI output
▶ [1] Daily Backup [manual] run=260423-210030-a1f2
...
✓ [success] Daily Backup (exit=0, 1.2s) [manual] run=260423-210030-a1f2
```

## Logs

- Daemon log: `/opt/cronplus/logs/cronplus.log` (auto-rotated, configurable size & backup count)
- Task logs: `/opt/cronplus/logs/task_{id}.json` (per-task JSONL, appended one line per run, rotated when the file exceeds the size limit)

> Since 2.4.8 task logs are JSONL (one entry per line). Files in the old JSON-array
> format are still read, and are migrated to JSONL on the first append. Appending is
> a single `O_APPEND` write instead of "read everything → modify the array → rewrite
> the file", so a busy task no longer rewrites its whole history on every run.

### Log Rotation

Daemon logs are checked at startup and rotated when oversized:

```
cronplus.log      Current log
cronplus.log.1    Previous rotation
cronplus.log.2    Older
```

Task logs follow the same scheme, checked on write:

```
task_3.json       Current log (newest)
task_3.json.1     Previous rotation
task_3.json.2     Older
```

Rotated segments are still read by the log views, so rotation bounds the size of a
single file without losing history. The WebUI fetches the newest 50 entries per
task (`cronplus logs --all --per-task 50 --no-output`), so one chatty task can no
longer push the other tasks' entries out of the list. `--no-output` keeps the list
payload to summaries; a single entry's output is fetched with `--run-id` when it is
expanded.

Configurable via settings:
- Max file size (1/5/10/20/50 MB)
- Backup count (1/3/5/10)

Output of a single run is capped at 256 KB. Over that, the first 64 KB and the end
are kept, a `[cronplus] ---- output truncated ----` marker separates them, and a
trailing `[cronplus] result: <status> (exit=<code>, <duration>)` line always records the
verdict — so a truncated entry still tells you whether the run succeeded.

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
  "taskPageSize": 20,
  "maxConcurrentTasks": 50,
  "logMaxBytes": 10485760,
  "logBackupCount": 5,
  "daemonLogLevel": "all",
  "daemonLogLines": 100,
  "daemonLogInterval": 2,
  "daemonLogMaxBytes": 10485760,
  "daemonLogBackupCount": 3
}
```

| Setting | Meaning |
| --- | --- |
| `maxConcurrentTasks` | Daemon-wide concurrency ceiling (default 50); scheduled runs beyond it are skipped and logged |
| `taskPageSize` | Tasks per page in the WebUI list (default 20) |
| `logPageSize` | Log entries per page in the WebUI list (default 20) |

## Signals

```bash
kill -HUP <pid>     Hot-reload config only (never interrupts running tasks)
kill -TERM <pid>    Graceful shutdown
kill -USR1 <pid>    Dump running status to log
kill -USR2 <pid>    Terminate every running task
```

> Before 2.4.8, `SIGHUP` killed every running task, so saving a task (which
> hot-reloads) could kill work in progress. Reload and terminate are now separate
> signals; saving a task in the WebUI only triggers a reload.

## Directory Structure

```
cockpit-cronplus/
├── build-deb.sh              Build & packaging script
├── VERSION                   Version file
├── cockpit-cronplus/         Cockpit WebUI plugin
│   ├── index.html
│   ├── manifest.json
│   ├── lang/                 i18n (9 languages)
│   └── static/               CSS + JS (SVG vector icons)
└── cronplus/                 Go source code
    ├── cmd/
    │   ├── cronplus/         CLI entry
    │   └── cronplusd/        Daemon entry
    ├── internal/
    │   ├── executor/         Process executor (Run ID, signal forwarding)
    │   ├── scheduler/        Cron scheduler (anchor-based step interval)
    │   └── store/            File-based storage (atomic writes)
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
- Accepts optional changelog input for release notes
- Builds for amd64 and arm64
- Packages `.deb` files
- Creates GitHub Release with checksums and changelog

## License

MIT
