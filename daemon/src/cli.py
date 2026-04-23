"""cronplus CLI — run single tasks, reload daemon, show status."""

import argparse
import json
import os
import signal
import subprocess
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from src.config import read_config, get_task, read_logs, clear_logs, export_tasks, import_tasks, write_config, DEFAULT_CONF, DEFAULT_LOGS, read_settings, write_settings, update_settings
from src.executor import Executor, decode_command
from src.version import VERSION

SERVICE_NAME = 'cronplus.service'
REBOOT_FILE = '/run/cronplus'


def _daemon_pid():
    """Get daemon PID via systemctl (no PID file needed)."""
    try:
        r = subprocess.run(
            ['systemctl', 'show', '-p', 'MainPID', '--value', SERVICE_NAME],
            capture_output=True, text=True, timeout=5
        )
        pid = int(r.stdout.strip())
        return pid if pid > 0 else None
    except (FileNotFoundError, ValueError, subprocess.TimeoutExpired):
        return None


def cmd_reload(args):
    """Reload daemon config via systemctl."""
    try:
        r = subprocess.run(['systemctl', 'reload', SERVICE_NAME],
                           capture_output=True, text=True, timeout=10)
        if r.returncode == 0:
            print("Config reloaded")
            return 0
        print(f"Error: {r.stderr.strip() or r.stdout.strip()}", file=sys.stderr)
        return 1
    except FileNotFoundError:
        print("Error: systemctl not found", file=sys.stderr)
        return 1


def cmd_run(args):
    """Run a single task by id."""
    task = get_task(args.task_id)
    if not task:
        print(f"Error: task {args.task_id} not found", file=sys.stderr)
        return 1
    cmd_display = decode_command(task.get('command', ''))
    print(f"Running: {task.get('title') or cmd_display} ...")
    ex = Executor()
    result = ex.execute(task, trigger='manual')
    if result is None:
        print("Skipped: concurrency limit reached")
        return 1
    status, output, duration_ms, exit_code = result
    if output:
        print(output)
    print(f"\n{'✓' if status == 'success' else '✗'} {status} ({duration_ms}ms, exit={exit_code})")
    return 0 if status == 'success' else 1


def cmd_list(args):
    """List all tasks."""
    tasks = read_config()
    if not tasks:
        print("No tasks configured")
        return
    for t in tasks:
        en = '✓' if t.get('enabled', True) else '✗'
        title = t.get('title') or decode_command(t.get('command', ''))[:50]
        sched = t.get('schedule', '?')
        print(f"  {en} [{t['id']}] {title}  ({sched})")


def cmd_status(args):
    """Show daemon status."""
    pid = _daemon_pid()
    if pid:
        print(f"cronplus: running (pid {pid})")
    else:
        print("cronplus: not running")

    tasks = read_config()
    enabled = sum(1 for t in tasks if t.get('enabled', True))
    print(f"Tasks: {len(tasks)} total, {enabled} enabled")

    logs = read_logs()
    success = sum(1 for l in logs if l.get('status') == 'success')
    failed = sum(1 for l in logs if l.get('status') != 'success')
    print(f"Logs: {len(logs)} entries ({success} success, {failed} failed)")

    if os.path.exists(REBOOT_FILE):
        print("Reboot marker: present (will skip @reboot tasks on next start)")
    else:
        print("Reboot marker: absent (will fire @reboot tasks on next start)")


def cmd_cleanup(args):
    """Clean up stuck/stale task entries (sends SIGHUP to force re-check)."""
    pid = _daemon_pid()
    if not pid:
        print("cronplus: not running, nothing to clean up")
        return 0
    try:
        os.kill(pid, signal.SIGHUP)
        print("Cleanup signal sent (SIGHUP) — daemon will re-check stale entries")
        return 0
    except ProcessLookupError:
        print("Error: daemon process not found", file=sys.stderr)
        return 1


def cmd_logs(args):
    """Show recent logs."""
    logs = read_logs()
    if not logs:
        print("No logs")
        return
    task_id = args.task_id
    for l in reversed(logs[-args.limit:]):
        if task_id and l.get('task_id') != task_id:
            continue
        icon = '✓' if l.get('status') == 'success' else '✗'
        title = l.get('title') or decode_command(l.get('command', ''))[:50]
        ts = l.get('created_at', '')
        dur = l.get('duration', 0)
        print(f"  {icon} [{ts}] {title}  ({dur}ms)")


def cmd_export(args):
    """Export tasks to stdout as JSON."""
    data = export_tasks()
    json.dump(data, sys.stdout, ensure_ascii=False, indent=2)
    print()


def cmd_import(args):
    """Import tasks from JSON file."""
    with open(args.file) as f:
        data = json.load(f)
    count = import_tasks(data)
    print(f"Imported {count} tasks")
    # Signal daemon
    try:
        subprocess.run(['systemctl', 'reload', SERVICE_NAME],
                       capture_output=True, timeout=10)
    except (FileNotFoundError, subprocess.TimeoutExpired):
        pass


def cmd_clear_logs(args):
    """Clear all logs."""
    clear_logs()
    print("Logs cleared")


def _systemctl(action):
    """Run systemctl action on cronplus service."""
    try:
        result = subprocess.run(
            ['systemctl', action, SERVICE_NAME],
            capture_output=True, text=True, timeout=15
        )
        if result.returncode == 0:
            print(f"cronplus: {action} successful")
            return 0
        else:
            err = result.stderr.strip() or result.stdout.strip()
            print(f"Error: {err}", file=sys.stderr)
            return 1
    except FileNotFoundError:
        print("Error: systemctl not found (not a systemd system?)", file=sys.stderr)
        return 1
    except subprocess.TimeoutExpired:
        print("Error: systemctl timed out", file=sys.stderr)
        return 1


def cmd_start(args):
    """Start the cronplus daemon."""
    pid = _daemon_pid()
    if pid:
        print("cronplus: already running")
        return 0
    return _systemctl('start')


def cmd_stop(args):
    """Stop the cronplus daemon."""
    pid = _daemon_pid()
    if not pid:
        print("cronplus: not running")
        return 0
    return _systemctl('stop')


def cmd_restart(args):
    """Restart the cronplus daemon."""
    return _systemctl('restart')


def cmd_version(args):
    """Show version info."""
    print(f"cronplus v{VERSION}")
    pid = _daemon_pid()
    if pid:
        print(f"Daemon: running (pid {pid})")
    else:
        print("Daemon: not running")
    tasks = read_config()
    print(f"Tasks:   {len(tasks)} configured")


def cmd_settings(args):
    """Show or update settings."""
    if args.action == 'show' or args.action is None:
        settings = read_settings()
        print(json.dumps(settings, ensure_ascii=False, indent=2))
        return 0
    elif args.action == 'set':
        if not args.key:
            print("Error: --key is required", file=sys.stderr)
            return 1
        # Try to parse value as JSON, fallback to string
        try:
            value = json.loads(args.value)
        except (json.JSONDecodeError, TypeError):
            value = args.value
        updated = update_settings({args.key: value})
        print(f"Set {args.key} = {json.dumps(value, ensure_ascii=False)}")
        return 0
    elif args.action == 'reset':
        from src.config import SETTINGS_DEFAULTS
        write_settings(dict(SETTINGS_DEFAULTS))
        print("Settings reset to defaults")
        return 0
    else:
        print(f"Unknown settings action: {args.action}", file=sys.stderr)
        return 1


def main():
    parser = argparse.ArgumentParser(
        prog='cronplus',
        description='cronplus — Advanced cron task manager',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""\
Commands:
  start        Start the daemon
  stop         Stop the daemon
  restart      Restart the daemon
  reload       Reload config without restart
  status       Show daemon & task status
  list (ls)    List all tasks
  run <id>     Run a task immediately
  logs [id]    Show execution logs
  export       Export tasks to stdout (JSON)
  import <f>   Import tasks from JSON file
  clear-logs   Clear all execution logs
  cleanup      Clean up stuck task entries
  settings     Show/update settings
  version      Show version info

Examples:
  cronplus status
  cronplus run 3
  cronplus logs -n 20
  cronplus restart
  cronplus cleanup
"""
    )
    parser.add_argument('-v', '--version', action='store_true', help='Show version info')
    sub = parser.add_subparsers(dest='command')

    sub.add_parser('start', help='Start the daemon')
    sub.add_parser('stop', help='Stop the daemon')
    sub.add_parser('restart', help='Restart the daemon')
    sub.add_parser('reload', help='Reload daemon config')
    sub.add_parser('status', help='Show daemon status')
    sub.add_parser('list', aliases=['ls'], help='List tasks')
    sub.add_parser('export', help='Export tasks to stdout')
    sub.add_parser('version', help='Show version info')

    p_run = sub.add_parser('run', help='Run a task now')
    p_run.add_argument('task_id', type=int, help='Task ID')

    p_logs = sub.add_parser('logs', help='Show logs')
    p_logs.add_argument('task_id', type=int, nargs='?', default=None)
    p_logs.add_argument('-n', '--limit', type=int, default=50)

    p_import = sub.add_parser('import', help='Import tasks from JSON')
    p_import.add_argument('file', help='JSON file path')

    sub.add_parser('clear-logs', help='Clear all logs')
    sub.add_parser('cleanup', help='Clean up stuck task entries')

    p_settings = sub.add_parser('settings', help='Show/update settings')
    p_settings.add_argument('action', nargs='?', choices=['show', 'set', 'reset'], default='show',
                            help='show (default), set, or reset to defaults')
    p_settings.add_argument('--key', help='Setting key to update')
    p_settings.add_argument('--value', help='New value (JSON or string)')

    args = parser.parse_args()

    if args.version:
        cmd_version(args)
        sys.exit(0)

    if not args.command:
        parser.print_help()
        return

    handlers = {
        'start': cmd_start, 'stop': cmd_stop, 'restart': cmd_restart,
        'reload': cmd_reload, 'run': cmd_run, 'list': cmd_list, 'ls': cmd_list,
        'status': cmd_status, 'logs': cmd_logs, 'export': cmd_export,
        'import': cmd_import, 'clear-logs': cmd_clear_logs,
        'cleanup': cmd_cleanup, 'settings': cmd_settings,
        'version': cmd_version,
    }
    sys.exit(handlers[args.command](args) or 0)


if __name__ == '__main__':
    main()
