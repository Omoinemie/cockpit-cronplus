"""cronplus executor — run commands with timeout, retry, concurrency."""

import subprocess
import os
import time
import threading
import signal
import logging
import base64

from src.config import append_log, DEFAULT_LOGS

logger = logging.getLogger('cronplus.executor')


def _validate_cwd(cwd):
    """[优化6] 校验工作目录是否合法。"""
    if not cwd:
        return None
    expanded = os.path.expanduser(cwd)
    if not os.path.isdir(expanded):
        logger.warning(f"cwd '{cwd}' does not exist or is not a directory, ignoring")
        return None
    return expanded


def decode_command(cmd):
    """Decode base64 command if needed, with backward compat for plain text."""
    if not cmd:
        return cmd
    try:
        decoded = base64.b64decode(cmd).decode('utf-8')
        # Verify it's actually base64 (not just coincidentally decodable)
        # If re-encoding matches, it was base64
        if base64.b64encode(decoded.encode('utf-8')).decode('ascii') == cmd:
            return decoded
    except Exception:
        pass
    return cmd  # plain text fallback



class Executor:
    # Max time a task can stay in _running before forced cleanup (seconds)
    STALE_TIMEOUT = 3600

    def __init__(self, logs_path=None):
        self._running = {}   # task_id -> {process, start_ts, start_time}
        self._lock = threading.Lock()
        self._logs_path = logs_path or DEFAULT_LOGS

    def get_running(self):
        with self._lock:
            return [
                {'task_id': tid, 'pid': info['process'].pid if info['process'] else None,
                 'started_at': info['start_time'],
                 'duration_ms': int((time.time() - info['start_ts']) * 1000)}
                for tid, info in self._running.items()
            ]

    def is_running(self, task_id):
        with self._lock:
            return task_id in self._running

    def cleanup_stale(self):
        """Remove stale entries from _running where process has died or entry is too old."""
        now = time.time()
        with self._lock:
            stale = []
            for tid, info in self._running.items():
                proc = info.get('process')
                age = now - info.get('start_ts', now)
                # Stale if: process is None and entry > 60s old, or process already exited, or entry > STALE_TIMEOUT
                if proc is None and age > 60:
                    stale.append(tid)
                elif proc is not None and proc.poll() is not None:
                    stale.append(tid)
                elif age > self.STALE_TIMEOUT:
                    stale.append(tid)
            for tid in stale:
                info = self._running.pop(tid)
                age_s = int(now - info.get('start_ts', now))
                logger.warning(f"Executor: cleaned up stale running entry for task [{tid}] (age={age_s}s)")

    def cleanup_zombies(self):
        """[优化1] 回收僵尸进程：使用 os.waitpid 非阻塞回收所有已结束的子进程。

        调用时机：
        - SIGCHLD 信号触发时（异步回收）
        - SIGHUP 手动清理时
        - 调度器定期巡检时（每 5 分钟）
        """
        reclaimed = 0
        try:
            while True:
                pid, status = os.waitpid(-1, os.WNOHANG)
                if pid == 0:
                    break
                reclaimed += 1
                logger.debug(f"Zombie cleanup: reaped pid={pid}, status={status}")
        except ChildProcessError:
            # 没有子进程需要回收，这是正常情况
            pass
        if reclaimed > 0:
            logger.info(f"Zombie cleanup: reclaimed {reclaimed} zombie process(es)")

    def execute(self, task, attempt=1, trigger='auto'):
        """Execute task. Returns (status, output, duration_ms, exit_code) or None if skipped.

        Args:
            trigger: 'auto' (scheduled) or 'manual' (CLI/UI run)
        """
        task_id = task['id']
        command = decode_command(task.get('command', ''))
        # Create a copy with decoded command for downstream use
        task = dict(task, command=command)
        run_user = task.get('run_user', 'root')
        timeout = task.get('timeout', 0)
        max_retries = task.get('max_retries', 0)
        retry_interval = task.get('retry_interval', 60)
        env_vars = task.get('env_vars', {})
        max_concurrent = task.get('max_concurrent', 1)

        with self._lock:
            running_count = sum(1 for tid in self._running if tid == task_id)
            if running_count >= max_concurrent:
                if task.get('kill_previous'):
                    # Kill existing running processes for this task
                    killed = []
                    for key in list(self._running.keys()):
                        if key == task_id:
                            info = self._running.pop(key)
                            proc = info.get('process')
                            if proc and proc.poll() is None:
                                try:
                                    os.killpg(os.getpgid(proc.pid), signal.SIGTERM)
                                    killed.append(proc.pid)
                                except Exception:
                                    pass
                    if killed:
                        logger.info(f"Task {task_id}: killed previous processes (pids={killed})")
                else:
                    logger.warning(f"Task {task_id}: concurrency limit ({max_concurrent}) reached, skipping")
                    return None
            self._running[task_id] = {
                'process': None,
                'start_time': time.strftime('%Y-%m-%d %H:%M:%S'),
                'start_ts': time.time(),
            }

        try:
            return self._run(task, command, run_user, timeout, env_vars,
                             attempt, max_retries, retry_interval, trigger)
        finally:
            with self._lock:
                self._running.pop(task_id, None)

    def _run(self, task, command, run_user, timeout, env_vars,
             attempt, max_retries, retry_interval, trigger='auto'):
        task_id = task['id']
        start_ts = time.time()

        env = os.environ.copy()
        env['CRONPLUS_TASK_ID'] = str(task_id)
        env['CRONPLUS_ATTEMPT'] = str(attempt)
        if isinstance(env_vars, dict):
            env.update(env_vars)

        # [优化4] 任务隔离：校验并使用任务配置的 cwd（工作目录）
        cwd = _validate_cwd(task.get('cwd'))

        task_name = task.get('title') or command[:60]
        logger.info(f"Task [{task_id}] {task_name} — started (user={run_user}, attempt={attempt}, cwd={cwd or 'default'})")

        # Pass command directly to bash — bash handles all quoting internally
        if run_user and run_user != 'root':
            shell_cmd = ['su', '-', run_user, '-c', command]
        else:
            shell_cmd = ['bash', '-c', command]

        try:
            proc = subprocess.Popen(
                shell_cmd, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                env=env, cwd=cwd, preexec_fn=os.setsid
            )
            with self._lock:
                if task_id in self._running:
                    self._running[task_id]['process'] = proc

            try:
                out_bytes, _ = proc.communicate(timeout=timeout if timeout > 0 else None)
                output = out_bytes.decode('utf-8', errors='replace')
                exit_code = proc.returncode
                status = 'success' if exit_code == 0 else 'error'
            except subprocess.TimeoutExpired:
                os.killpg(os.getpgid(proc.pid), signal.SIGTERM)
                try:
                    proc.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    os.killpg(os.getpgid(proc.pid), signal.SIGKILL)
                    proc.wait()
                output = f'[cronplus] 命令超时 ({timeout}s) 已终止'
                exit_code = -1
                status = 'error'

        except Exception as e:
            output = f'[cronplus] 执行异常: {e}'
            exit_code = -1
            status = 'error'

        duration_ms = int((time.time() - start_ts) * 1000)

        icon = '✓' if status == 'success' else '✗'
        logger.info(f"Task [{task_id}] {task_name} — {icon} {status} ({duration_ms}ms, exit={exit_code}, attempt={attempt})")

        # Write log
        log_entry = {
            'task_id': task_id,
            'title': task.get('title', ''),
            'command': command,
            'status': status,
            'output': (output or '')[:50000],
            'duration': duration_ms,
            'run_user': run_user,
            'exit_code': exit_code,
            'attempt': attempt,
            'trigger': trigger,
            'created_at': time.strftime('%Y-%m-%d %H:%M:%S'),
        }
        try:
            append_log(log_entry, self._logs_path)
        except Exception as e:
            logger.error(f"Failed to write log: {e}")
        # Prune old logs per task retention settings
        try:
            from src.config import cleanup_logs
            cleanup_logs()
        except Exception:
            pass

        # Retry
        if status == 'error' and attempt < max_retries:
            logger.info(f"Task {task_id} failed (attempt {attempt}/{max_retries}), retry in {retry_interval}s")
            time.sleep(retry_interval)
            from src.config import get_task
            fresh = get_task(task_id)
            if fresh and fresh.get('enabled', True):
                return self._run(fresh, command, run_user, timeout, env_vars,
                                 attempt + 1, max_retries, retry_interval, trigger)

        logger.info(f"Task {task_id}: {status} ({duration_ms}ms, attempt {attempt})")
        return status, output, duration_ms, exit_code
