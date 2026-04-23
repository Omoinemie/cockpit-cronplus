import os
import threading
import time
import re
import logging
from datetime import datetime, timedelta

from src.config import read_config
from src.version import VERSION

logger = logging.getLogger('cronplus.scheduler')

# /run is tmpfs — this file disappears on reboot.
# File exists  → daemon was restarted (skip @reboot)
# File missing → system rebooted (fire @reboot, then create file)
REBOOT_FILE = '/run/cronplus'


class Scheduler:
    def __init__(self, executor, conf_path=None, reload_interval=60):
        self.executor = executor
        self.conf_path = conf_path
        self.reload_interval = reload_interval
        self._running = False
        self._thread = None
        self._cond = threading.Condition()
        self._stats = {'tasks_executed': 0, 'tasks_failed': 0, 'last_run': None, 'started_at': None}
        self._last_conf_mtime = 0
        self._last_cleanup = 0
        self._task_last_run = {}  # task_id → datetime of last scheduled run
        self._cached_tasks = None  # cached task list, invalidated on config change

    def start(self):
        if self._running:
            return
        self._running = True
        self._stats['started_at'] = datetime.now().isoformat()
        self._thread = threading.Thread(target=self._loop, daemon=True, name='cronplus-scheduler')
        self._thread.start()
        logger.info(f"Scheduler v{VERSION} started")

    def stop(self):
        self._running = False
        with self._cond:
            self._cond.notify_all()
        if self._thread:
            self._thread.join(timeout=5)
        logger.info("Scheduler stopped")

    def wakeup(self):
        """Force re-read of config and reschedule."""
        self._last_conf_mtime = 0
        self._cached_tasks = None
        with self._cond:
            self._cond.notify_all()

    def _is_real_reboot(self):
        """Check /run/cronplus file: missing = system rebooted (tmpfs cleared it).

        Returns True once after a real reboot, then creates the file so
        subsequent daemon restarts won't re-trigger @reboot tasks.
        """
        exists = os.path.exists(REBOOT_FILE)
        if exists:
            logger.debug(f"Reboot check: {REBOOT_FILE} exists → NOT a reboot")
            return False
        try:
            with open(REBOOT_FILE, 'w') as f:
                f.write('1')
            logger.info(f"Reboot check: {REBOOT_FILE} missing → REBOOT detected, marker created")
            return True
        except OSError as e:
            logger.warning(f"Reboot check: failed to create marker: {e}")
            return False

    def get_stats(self):
        stats = dict(self._stats)
        stats['running_tasks'] = self.executor.get_running()
        stats['running_count'] = len(stats['running_tasks'])
        return stats

    def get_next_runs(self, second='0', minute='*', hour='*', day='*', month='*', dow='*',
                      count=5, special=None):
        if special:
            return []
        results = []
        now = datetime.now()
        current = now.replace(microsecond=0) + timedelta(seconds=1)
        max_check = current + timedelta(days=730)
        safety = 0
        while len(results) < count and current < max_check and safety < 500000:
            safety += 1
            if self._match_cron(current, second, minute, hour, day, month, dow):
                results.append(current.isoformat())
            current += timedelta(seconds=1)
        return results

    def _drift_compensated_wait(self, timeout):
        """[优化2] 飘移补偿等待：动态计算到下一秒整点的精确等待时间。

        相比固定 time.sleep(1) 或 cond.wait(timeout=1)，此方法通过计算
        距离下一秒边界的精确毫秒数来消除累积飘移，确保长时间运行后调度精度。
        """
        now = time.time()
        # 计算距离下一秒整点的秒数（例如 now=3.7 → wait=0.3）
        wait_time = 1.0 - (now % 1.0)
        # 确保不会因浮点精度问题出现负数或超大值
        wait_time = max(0.05, min(wait_time, 1.0))
        with self._cond:
            self._cond.wait(timeout=min(wait_time, timeout))

    # ── Main loop ──

    def _loop(self):
        logger.info(f"Scheduler loop started, reboot marker: {REBOOT_FILE} "
                    f"{'exists' if os.path.exists(REBOOT_FILE) else 'MISSING (will fire @reboot tasks)'}")
        while self._running:
            try:
                # Periodic cleanup of stale executor entries + zombie processes (every 5 min)
                now_ts = time.time()
                if now_ts - self._last_cleanup > 300:
                    self.executor.cleanup_stale()
                    self.executor.cleanup_zombies()  # [优化1] 定期巡检僵尸进程
                    self._last_cleanup = now_ts

                tasks = self._load_tasks()
                if not tasks:
                    logger.debug("No tasks configured, waiting...")
                    self._drift_compensated_wait(1)
                    continue

                now_floor = datetime.now().replace(microsecond=0)
                next_runs = []
                real_reboot = self._is_real_reboot()

                for task in tasks:
                    task_id = task.get('id', '?')
                    if not task.get('enabled', True):
                        logger.debug(f"Task [{task_id}] disabled, skipping")
                        continue

                    schedule = task.get('schedule', '')

                    # Handle @reboot tasks — only fire on actual system reboot
                    if schedule == '@reboot':
                        if real_reboot:
                            delay = int(task.get('reboot_delay', 0) or 0)
                            if delay > 0:
                                logger.info(f"Task [{task_id}] @reboot: scheduled in {delay}s")
                                def _delayed_reboot(t=task, d=delay):
                                    time.sleep(d)
                                    self._execute_task(t)
                                threading.Thread(target=_delayed_reboot, daemon=True).start()
                            else:
                                logger.info(f"Task [{task_id}] @reboot: firing now")
                                self._execute_task(task)
                        else:
                            logger.debug(f"Task [{task_id}] @reboot: skipped (not a reboot)")
                        continue

                    if self.executor.is_running(task_id):
                        logger.debug(f"Task [{task_id}] already running, skipping")
                        continue

                    # Other @ specials → convert to cron
                    if schedule.startswith('@'):
                        cron = self._special_to_cron(schedule)
                        if cron is None:
                            continue
                        s, m, h, d, mo, w = cron
                    else:
                        s, m, h, d, mo, w = self.parse_schedule(schedule)

                    # Find next run strictly after last scheduled run
                    last_run = self._task_last_run.get(task_id, now_floor - timedelta(seconds=1))
                    nxt = self._next_run_time(s, m, h, d, mo, w, last_run, inclusive=False)
                    if nxt:
                        next_runs.append((nxt, task, s, m, h, d, mo, w))

                if not next_runs:
                    logger.debug("No runnable tasks found, waiting 1s")
                    self._drift_compensated_wait(1)
                    continue

                next_runs.sort(key=lambda x: x[0])

                # FIX: Execute tasks whose next scheduled time <= current second
                # Changed from == to <= to prevent missed-second deadlock
                for run_time, task, s, m, h, d, mo, w in next_runs:
                    if run_time <= now_floor:
                        task_id = task.get('id', '?')

                        # FIX: Always update last_run immediately upon reaching the
                        # scheduled time, even if the task is skipped due to being
                        # already running. This prevents the infinite-skip deadlock.
                        self._task_last_run[task_id] = run_time

                        if self.executor.is_running(task_id):
                            logger.debug(f"Task [{task_id}] already running, skipping this window")
                            continue

                        self._execute_task(task)
                    else:
                        break

                # Calculate wait until next task
                remaining = [item for item in next_runs if item[0] != now_floor]
                if remaining:
                    nearest_time = remaining[0][0]
                    nearest_task = remaining[0][1]
                    wait_sec = (nearest_time - datetime.now()).total_seconds()
                    if wait_sec > 0:
                        logger.info(f"Next: task [{nearest_task.get('id','?')}] "
                                   f"'{nearest_task.get('title') or nearest_task.get('command','')[:40]}' "
                                   f"at {nearest_time.strftime('%Y-%m-%d %H:%M:%S')} "
                                   f"(in {wait_sec:.0f}s)")
                        # [优化2] 飘移补偿：精确等待到下一秒边界，而非固定 1s
                        self._drift_compensated_wait(min(wait_sec, 1))
                        continue

                # Short sleep to avoid tight loop
                self._drift_compensated_wait(0.5)

            except Exception as e:
                logger.error(f"Scheduler loop error: {e}", exc_info=True)
                time.sleep(5)

    def _load_tasks(self):
        """Load tasks from config, with mtime-based caching.

        Only re-parses the file when its modification time changes,
        making hot-reload reliable even without SIGHUP.
        """
        try:
            mtime = os.path.getmtime(self.conf_path)
        except OSError:
            mtime = 0

        if mtime != self._last_conf_mtime or self._cached_tasks is None:
            try:
                self._cached_tasks = read_config(self.conf_path)
                self._last_conf_mtime = mtime
                if self._cached_tasks:
                    logger.info(f"Config reloaded: {len(self._cached_tasks)} tasks (mtime={mtime})")
            except Exception as e:
                logger.error(f"Failed to load config: {e}")
                # Keep cached tasks on parse error (don't lose existing schedule)
                if self._cached_tasks is None:
                    self._cached_tasks = []

        return self._cached_tasks

    def _execute_task(self, task):
        task_id = task.get('id', '?')
        logger.info(f"Scheduler: triggering task [{task_id}] {task.get('title') or task.get('command','')[:60]}")
        def _run():
            try:
                result = self.executor.execute(task, trigger='auto')
                if result:
                    self._stats['tasks_executed'] += 1
                    self._stats['last_run'] = datetime.now().isoformat()
                    if result[0] == 'error':
                        self._stats['tasks_failed'] += 1
            except Exception as e:
                logger.error(f"Task [{task_id}] thread error: {e}", exc_info=True)
        threading.Thread(target=_run, daemon=True).start()

    # ── Cron matching ──

    @staticmethod
    def parse_schedule(schedule):
        """Parse a 6-field cron expression into (second, minute, hour, day, month, dow)."""
        parts = schedule.split()
        if len(parts) == 6:
            return parts[0], parts[1], parts[2], parts[3], parts[4], parts[5]
        if len(parts) == 5:
            return '0', parts[0], parts[1], parts[2], parts[3], parts[4]
        return '0', '*', '*', '*', '*', '*'

    def _next_run_time(self, second, minute, hour, day, month, dow, after, inclusive=False):
        if inclusive:
            current = after.replace(microsecond=0)
        else:
            current = after.replace(microsecond=0) + timedelta(seconds=1)
        max_check = current + timedelta(days=730)
        safety = 0
        while current < max_check and safety < 5000000:
            safety += 1
            if self._match_cron(current, second, minute, hour, day, month, dow):
                return current
            current += timedelta(seconds=1)
        return None

    def _match_cron(self, dt, second, minute, hour, day, month, dow):
        if not self._match_field(dt.second, second, 0, 59): return False
        if not self._match_field(dt.minute, minute, 0, 59): return False
        if not self._match_field(dt.hour, hour, 0, 23): return False
        if not self._match_field(dt.month, month, 1, 12): return False
        day_match = self._match_field(dt.day, day, 1, 31)
        dow_match = self._match_field(dt.weekday() if dt.weekday() < 7 else 0, dow, 0, 7)
        if day != '*' and dow != '*':
            return day_match or dow_match
        if day != '*' and not day_match: return False
        if dow != '*' and not dow_match: return False
        return True

    def _match_field(self, value, spec, min_val, max_val):
        if spec == '*': return True
        if ',' in spec:
            return any(self._match_field(value, s.strip(), min_val, max_val) for s in spec.split(','))
        m = re.match(r'^(\d+)-(\d+)$', spec)
        if m: return int(m.group(1)) <= value <= int(m.group(2))
        m = re.match(r'^(.*?)/(\d+)$', spec)
        if m:
            base, step = m.group(1), int(m.group(2))
            if base == '*': return (value - min_val) % step == 0
            if '-' in base:
                p = base.split('-')
                return int(p[0]) <= value <= int(p[1]) and (value - int(p[0])) % step == 0
            return value == int(base)
        try: return value == int(spec)
        except ValueError: return False

    def _special_to_cron(self, schedule):
        specials = {
            '@yearly':    ('0', '0', '0', '1', '1', '*'),
            '@annually':  ('0', '0', '0', '1', '1', '*'),
            '@monthly':   ('0', '0', '0', '1', '*', '*'),
            '@weekly':    ('0', '0', '0', '*', '*', '0'),
            '@daily':     ('0', '0', '0', '*', '*', '*'),
            '@midnight':  ('0', '0', '0', '*', '*', '*'),
            '@hourly':    ('0', '0', '*', '*', '*', '*'),
        }
        return specials.get(schedule)
