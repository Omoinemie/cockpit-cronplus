"""cronplus config — read/write tasks.conf (JSON)."""

import json
import os
import fcntl
import re
import logging
import tempfile
from datetime import datetime, timedelta

from src.version import VERSION

logger = logging.getLogger('cronplus.config')

DEFAULT_CONF = '/opt/cronplus/tasks.conf'
DEFAULT_LOGS = '/opt/cronplus/logs/logs.json'
DEFAULT_SETTINGS = '/opt/cronplus/settings.json'
MAX_LOG_ENTRIES = 1000


# ──────────────────────────────────────────────
#  Settings file (settings.json)
# ──────────────────────────────────────────────

SETTINGS_DEFAULTS = {
    'language': 'zh-CN',
    'theme': 'auto',
    'autoRefreshInterval': 15,
    'logMaxBytes': 10 * 1024 * 1024,   # 10 MB
    'logBackupCount': 5,
    'defaultRunUser': 'root',
    'defaultTimeout': 0,
    'defaultMaxRetries': 0,
    'defaultRetryInterval': 60,
    'logPageSize': 20,
    'taskPageSize': 20,
    'daemonLogLevel': 'all',
    'daemonLogLines': 100,
    'daemonLogInterval': 2,
}


def read_settings(path=None):
    """Read settings.json → dict (merged with defaults)."""
    path = path or DEFAULT_SETTINGS
    settings = dict(SETTINGS_DEFAULTS)
    if not os.path.exists(path):
        return settings
    try:
        with open(path, 'r') as f:
            data = json.load(f)
        if isinstance(data, dict):
            settings.update(data)
    except (json.JSONDecodeError, IOError) as e:
        logger.warning(f"Failed to read settings from {path}: {e}")
    return settings


def write_settings(settings, path=None):
    """Write settings dict to settings.json atomically."""
    path = path or DEFAULT_SETTINGS
    # Only keep known keys + allow custom keys
    content = json.dumps(settings, ensure_ascii=False, indent=2, default=str)
    _atomic_write(path, content)
    logger.info(f"Settings saved to {path}")


def update_settings(updates, path=None):
    """Merge updates into existing settings and save."""
    current = read_settings(path)
    current.update(updates)
    write_settings(current, path)
    return current


# ──────────────────────────────────────────────
#  Config file (tasks.conf)
# ──────────────────────────────────────────────

def _atomic_write(path, content):
    """Write file atomically via temp + rename."""
    dir_name = os.path.dirname(path)
    os.makedirs(dir_name, exist_ok=True)
    fd, tmp = tempfile.mkstemp(dir=dir_name, prefix='.tmp_')
    try:
        with os.fdopen(fd, 'w') as f:
            f.write(content)
        os.replace(tmp, path)
    except Exception:
        os.unlink(tmp)
        raise


def read_config(path=None):
    """Read tasks.conf → list of task dicts."""
    path = path or DEFAULT_CONF
    if not os.path.exists(path):
        return []
    try:
        with open(path, 'r') as f:
            data = json.load(f)
        if not isinstance(data, list):
            logger.warning(f"{path}: expected JSON array, got {type(data).__name__}")
            return []
        return data
    except (json.JSONDecodeError, IOError) as e:
        logger.error(f"Failed to read {path}: {e}")
        return []


def write_config(tasks, path=None):
    """Write tasks list to tasks.conf atomically."""
    path = path or DEFAULT_CONF
    os.makedirs(os.path.dirname(path), exist_ok=True)
    content = json.dumps(tasks, ensure_ascii=False, indent=2, default=str)
    _atomic_write(path, content)
    logger.debug(f"Wrote {len(tasks)} tasks to {path}")


def get_task(task_id, path=None):
    """Get a single task by id."""
    for t in read_config(path):
        if t.get('id') == task_id:
            return t
    return None


def _validate_cron_field(spec, min_val, max_val, field_name):
    """校验单个 cron 字段是否合法。返回错误消息列表。"""
    errors = []
    if spec == '*':
        return errors
    for part in spec.split(','):
        part = part.strip()
        # */N 或 range/N
        m = re.match(r'^(.*?)/(\d+)$', part)
        if m:
            base, step = m.group(1), int(m.group(2))
            if step < 1 or step > max_val:
                errors.append(f"{field_name}: step {step} out of range [1,{max_val}]")
            if base != '*':
                # 校验 base 部分
                if '-' in base:
                    try:
                        lo, hi = map(int, base.split('-'))
                        if lo < min_val or hi > max_val or lo > hi:
                            errors.append(f"{field_name}: range {base} invalid")
                    except ValueError:
                        errors.append(f"{field_name}: malformed range '{base}'")
                else:
                    try:
                        v = int(base)
                        if v < min_val or v > max_val:
                            errors.append(f"{field_name}: value {v} out of range [{min_val},{max_val}]")
                    except ValueError:
                        errors.append(f"{field_name}: non-numeric '{base}'")
            continue
        # Range: N-M
        m = re.match(r'^(\d+)-(\d+)$', part)
        if m:
            lo, hi = int(m.group(1)), int(m.group(2))
            if lo < min_val or hi > max_val or lo > hi:
                errors.append(f"{field_name}: range {part} invalid")
            continue
        # Single number
        try:
            v = int(part)
            if v < min_val or v > max_val:
                errors.append(f"{field_name}: value {v} out of range [{min_val},{max_val}]")
        except ValueError:
            errors.append(f"{field_name}: unrecognized token '{part}'")
    return errors


def validate_task(task):
    """[优化6] 校验任务配置的合法性。返回错误消息列表（空 = 合法）。

    检查项：
    - Cron 表达式格式与数值范围
    - Base64 命令是否可解码
    - cwd 路径是否存在
    """
    errors = []

    # 校验 schedule
    schedule = task.get('schedule', '')
    if not schedule:
        errors.append('schedule is empty')
    elif schedule.startswith('@'):
        valid_specials = {'@yearly', '@annually', '@monthly', '@weekly',
                          '@daily', '@midnight', '@hourly', '@reboot'}
        if schedule not in valid_specials:
            errors.append(f"unknown special schedule '{schedule}'")
    else:
        parts = schedule.split()
        if len(parts) == 6:
            fields = [('second', 0, 59), ('minute', 0, 59), ('hour', 0, 23),
                      ('day', 1, 31), ('month', 1, 12), ('dow', 0, 7)]
        elif len(parts) == 5:
            fields = [('minute', 0, 59), ('hour', 0, 23),
                      ('day', 1, 31), ('month', 1, 12), ('dow', 0, 7)]
        else:
            errors.append(f"schedule has {len(parts)} fields, expected 5 or 6")
            fields = []
        for i, (name, lo, hi) in enumerate(fields):
            if i < len(parts):
                errors.extend(_validate_cron_field(parts[i], lo, hi, name))

    # 校验 command（base64 合法性）
    cmd = task.get('command', '')
    if not cmd:
        errors.append('command is empty')
    else:
        try:
            import base64 as _b64
            decoded = _b64.b64decode(cmd).decode('utf-8')
            if _b64.b64encode(decoded.encode('utf-8')).decode('ascii') != cmd:
                # 不是合法 base64，当作纯文本 — 不报错
                pass
        except Exception:
            # 不是 base64，当作纯文本命令 — 合法
            pass

    # 校验 cwd
    cwd = task.get('cwd', '')
    if cwd:
        expanded = os.path.expanduser(cwd)
        if not os.path.isdir(expanded):
            errors.append(f"cwd '{cwd}' does not exist or is not a directory")

    return errors


def read_and_validate_config(path=None):
    """读取配置并校验所有任务，返回 (tasks, validation_errors)。"""
    tasks = read_config(path)
    all_errors = {}
    for t in tasks:
        tid = t.get('id', '?')
        errs = validate_task(t)
        if errs:
            all_errors[tid] = errs
    return tasks, all_errors


def add_task(task_data, path=None):
    """Add a new task, auto-assign id."""
    tasks = read_config(path)
    max_id = max((t.get('id', 0) for t in tasks), default=0)
    task_data['id'] = max_id + 1
    task_data.setdefault('enabled', True)
    task_data.setdefault('title', '')
    task_data.setdefault('comment', '')
    task_data.setdefault('run_user', 'root')
    task_data.setdefault('timeout', 0)
    task_data.setdefault('max_retries', 0)
    task_data.setdefault('retry_interval', 60)
    task_data.setdefault('env_vars', {})
    task_data.setdefault('max_concurrent', 1)
    task_data.setdefault('tags', '')
    task_data.setdefault('log_retention_days', 0)
    task_data.setdefault('log_max_entries', 0)
    task_data.setdefault('cwd', '')  # [优化4] 工作目录字段
    # Backward compat: build schedule from individual fields if missing
    if 'schedule' not in task_data:
        task_data['schedule'] = _build_schedule(task_data)
    # Strip individual cron fields — schedule is the single source of truth
    for k in ('second', 'minute', 'hour', 'day', 'month', 'dow'):
        task_data.pop(k, None)
    # [优化6] 校验配置合法性
    errors = validate_task(task_data)
    if errors:
        logger.warning(f"Task {task_data['id']} validation warnings: {'; '.join(errors)}")
    tasks.append(task_data)
    write_config(tasks, path)
    return task_data


def update_task(task_id, updates, path=None):
    """Update an existing task."""
    tasks = read_config(path)
    for t in tasks:
        if t.get('id') == task_id:
            t.update(upk for upk in updates.items() if upk[0] != 'id')
            # Backward compat: build schedule from individual fields if provided
            if any(k in updates for k in ('second', 'minute', 'hour', 'day', 'month', 'dow')):
                t['schedule'] = _build_schedule(t)
            # Strip individual cron fields
            for k in ('second', 'minute', 'hour', 'day', 'month', 'dow'):
                t.pop(k, None)
            write_config(tasks, path)
            return t
    return None


def delete_task(task_id, path=None):
    """Delete a task by id."""
    tasks = read_config(path)
    new_tasks = [t for t in tasks if t.get('id') != task_id]
    if len(new_tasks) == len(tasks):
        return False
    write_config(new_tasks, path)
    return True


def toggle_task(task_id, path=None):
    """Toggle a task's enabled state."""
    tasks = read_config(path)
    for t in tasks:
        if t.get('id') == task_id:
            t['enabled'] = not t.get('enabled', True)
            write_config(tasks, path)
            return t
    return None


def import_tasks(data, path=None):
    """Import tasks from a dict {tasks: [...]}."""
    items = data.get('tasks', [])
    if not items:
        return 0
    tasks = read_config(path)
    max_id = max((t.get('id', 0) for t in tasks), default=0)
    count = 0
    for item in items:
        if not item.get('command'):
            continue
        max_id += 1
        item['id'] = max_id
        item.setdefault('enabled', True)
        item.setdefault('run_user', 'root')
        item.setdefault('timeout', 0)
        item.setdefault('max_retries', 0)
        item.setdefault('retry_interval', 60)
        item.setdefault('env_vars', {})
        item.setdefault('max_concurrent', 1)
        item.setdefault('tags', '')
        item.setdefault('title', '')
        item.setdefault('comment', '')
        item.setdefault('log_retention_days', 0)
        item.setdefault('log_max_entries', 0)
        # Backward compat: build schedule from individual fields if missing
        if 'schedule' not in item:
            item['schedule'] = _build_schedule(item)
        # Strip individual cron fields — schedule is the single source of truth
        for k in ('second', 'minute', 'hour', 'day', 'month', 'dow'):
            item.pop(k, None)
        tasks.append(item)
        count += 1
    write_config(tasks, path)
    return count


def export_tasks(path=None):
    """Export all tasks as a dict."""
    tasks = read_config(path)
    return {
        'version': VERSION,
        'exportTime': datetime.now().isoformat(),
        'source': 'cronplus',
        'tasks': tasks
    }


def _build_schedule(t):
    if t.get('schedule', '').startswith('@'):
        return t['schedule']
    return (f"{t.get('second', '0')} {t.get('minute', '*')} {t.get('hour', '*')} "
            f"{t.get('day', '*')} {t.get('month', '*')} {t.get('dow', '*')}")


# ──────────────────────────────────────────────
#  Logs file (logs.json)
# ──────────────────────────────────────────────

def read_logs(path=None):
    """Read logs.json → list of log dicts."""
    path = path or DEFAULT_LOGS
    if not os.path.exists(path):
        return []
    try:
        with open(path, 'r') as f:
            data = json.load(f)
        if not isinstance(data, list):
            return []
        return data
    except (json.JSONDecodeError, IOError):
        return []


def append_log(entry, path=None):
    """Append a log entry, keep max entries."""
    path = path or DEFAULT_LOGS
    os.makedirs(os.path.dirname(path), exist_ok=True)

    # Lock and read
    with open(path, 'a+') as f:
        fcntl.flock(f.fileno(), fcntl.LOCK_EX)
        try:
            f.seek(0)
            content = f.read().strip()
            logs = []
            if content:
                try:
                    logs = json.loads(content)
                    if not isinstance(logs, list):
                        logs = []
                except json.JSONDecodeError:
                    logs = []

            logs.append(entry)
            # Trim
            if len(logs) > MAX_LOG_ENTRIES:
                logs = logs[-MAX_LOG_ENTRIES:]

            f.seek(0)
            f.truncate()
            f.write(json.dumps(logs, ensure_ascii=False, default=str))
        finally:
            fcntl.flock(f.fileno(), fcntl.LOCK_UN)


def clear_logs(path=None):
    """Clear all logs."""
    path = path or DEFAULT_LOGS
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, 'w') as f:
        f.write('[]')


def cleanup_logs(conf_path=None, logs_path=None):
    """Prune logs based on per-task retention settings (log_retention_days, log_max_entries).

    A value of 0 means no limit for that setting. Both settings are independent:
    a log entry is removed if it exceeds EITHER limit.
    """
    tasks = read_config(conf_path)
    if not tasks:
        return
    # Build retention map: task_id -> {days, max}
    retention = {}
    for t in tasks:
        tid = t.get('id')
        if tid is None:
            continue
        days = t.get('log_retention_days', 0) or 0
        mx = t.get('log_max_entries', 0) or 0
        if days > 0 or mx > 0:
            retention[tid] = {'days': days, 'max': mx}
    if not retention:
        return

    logs = read_logs(logs_path)
    if not logs:
        return

    now = datetime.now()
    changed = False

    # Group logs by task_id
    from collections import defaultdict
    by_task = defaultdict(list)
    for i, l in enumerate(logs):
        by_task[l.get('task_id')].append(i)

    remove_set = set()
    for tid, indices in by_task.items():
        rule = retention.get(tid)
        if not rule:
            continue
        # Max entries: keep only last N
        if rule['max'] > 0 and len(indices) > rule['max']:
            for idx in indices[:-rule['max']]:
                remove_set.add(idx)
            changed = True
        # Retention days: remove older than N days
        if rule['days'] > 0:
            cutoff = now - timedelta(days=rule['days'])
            cutoff_str = cutoff.strftime('%Y-%m-%d %H:%M:%S')
            for idx in indices:
                ca = logs[idx].get('created_at', '')
                if ca and ca < cutoff_str:
                    remove_set.add(idx)
                    changed = True

    if changed:
        new_logs = [l for i, l in enumerate(logs) if i not in remove_set]
        path = logs_path or DEFAULT_LOGS
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, 'w') as f:
            f.write(json.dumps(new_logs, ensure_ascii=False, default=str))
        logger.info(f"Log cleanup: removed {len(remove_set)} entries (kept {len(new_logs)})")
