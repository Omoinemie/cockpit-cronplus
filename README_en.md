# cockpit-cronplus

**Visual cron task manager for Cockpit** — Replace traditional crontab with second-precision scheduling, timeout control, auto-retry, concurrency limits, execution logs, and a multi-language UI. Works out of the box.

> 🌐 [中文](README.md) · [日本語](README_ja.md) · [한국어](README_ko.md) · [Français](README_fr.md) · [Deutsch](README_de.md) · [Español](README_es.md) · [Русский](README_ru.md) · [Português](README_pt-BR.md)

## Features

- Second-precision cron scheduling (6-field: sec/min/hour/day/month/dow)
- Auto-kill on timeout, auto-retry on failure, concurrency control
- Per-task environment variables, working directory, log retention
- Cockpit Web UI: task editor, schedule presets, next-run preview, manual execution
- Task logs: command/output split view, syntax highlighting, multi-filter, bulk cleanup
- Daemon logs: real-time log viewer with keyword highlighting
- 9 languages, dark/light theme, full mobile responsive
- CLI tool: `cronplus status|list|run|logs|reload`

## Project Structure

```
cockpit-cronplus/
├── VERSION                  Version (auto-incremented on build)
├── build-deb.sh             One-click build (frontend + backend)
├── .github/workflows/       GitHub Actions manual build
├── daemon/                  Backend (Python daemon + CLI)
│   ├── src/                 Python source
│   └── systemd/             systemd service file
└── webui/                   Frontend (Cockpit plugin)
    ├── index.html
    ├── manifest.json
    ├── lang/                9 language packs
    └── static/              CSS + JS modules
```

## Installation

```bash
sudo dpkg -i cronplus_<version>_all.deb
sudo dpkg -i cockpit-cronplus_<version>_all.deb
```

After installation, find **Cronplus** in the Cockpit sidebar.

## Build

```bash
./build-deb.sh    # Read VERSION → build → auto patch+1
```

Or trigger manually from GitHub Actions — choose patch/minor/major to auto-build and create a Release.

## Files

| Path | Description |
|------|-------------|
| `/opt/cronplus/tasks.conf` | Task config (JSON) |
| `/opt/cronplus/settings.json` | Global settings |
| `/opt/cronplus/logs/logs.json` | Execution logs |
| `/opt/cronplus/logs/cronplus.log` | Daemon log (auto-rotated) |
| `/usr/bin/cronplus` | CLI tool |

## License

MIT
