#!/usr/bin/env python3
"""cronplus daemon — reads config, schedules tasks, writes logs."""

import logging
import os
import signal
import sys
import time
from logging.handlers import RotatingFileHandler

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from src.config import DEFAULT_CONF, DEFAULT_LOGS, read_settings
from src.executor import Executor
from src.scheduler import Scheduler
from src.version import VERSION

LOG_FILE = '/opt/cronplus/logs/cronplus.log'

logger = logging.getLogger('cronplus')


def _get_log_config():
    """Read log rotation config from settings.json, with env var fallback."""
    settings = read_settings()
    max_bytes = int(os.environ.get('CRONPLUS_LOG_MAX_BYTES', 0) or 0) or settings.get('logMaxBytes', 10 * 1024 * 1024)
    backup_count = int(os.environ.get('CRONPLUS_LOG_BACKUP_COUNT', 0) or 0) or settings.get('logBackupCount', 5)
    return max_bytes, backup_count


def setup_logging(debug=False):
    level = logging.DEBUG if debug else logging.INFO
    fmt = '%(asctime)s [%(name)s] %(levelname)s: %(message)s'
    handlers = [logging.StreamHandler(sys.stderr)]
    try:
        os.makedirs(os.path.dirname(LOG_FILE), exist_ok=True)
        max_bytes, backup_count = _get_log_config()
        file_handler = RotatingFileHandler(
            LOG_FILE,
            maxBytes=max_bytes,
            backupCount=backup_count,
            encoding='utf-8',
        )
        handlers.append(file_handler)
        logger.debug(f"Log rotation: maxBytes={max_bytes}, backupCount={backup_count}")
    except (PermissionError, OSError):
        pass
    logging.basicConfig(level=level, format=fmt, handlers=handlers)


def main():
    import argparse
    parser = argparse.ArgumentParser(description='cronplus daemon')
    parser.add_argument('--conf', default=DEFAULT_CONF, help='Config file path')
    parser.add_argument('--logs', default=DEFAULT_LOGS, help='Logs file path')
    parser.add_argument('-d', '--debug', action='store_true')
    args = parser.parse_args()

    setup_logging(debug=args.debug)

    logger.info("=" * 40)
    logger.info(f"cronplus daemon v{VERSION} starting")
    logger.info(f"Config: {args.conf}")
    logger.info(f"Logs:   {args.logs}")

    executor = Executor(logs_path=args.logs)
    scheduler = Scheduler(executor, conf_path=args.conf)

    # Handle signals
    def on_sigterm(signum, frame):
        logger.info("SIGTERM received, shutting down")
        scheduler.stop()
        sys.exit(0)

    def on_sighup(signum, frame):
        logger.info("SIGHUP received — full reset: killing stuck processes, clearing caches")
        # 先杀掉所有卡住的进程
        executor.kill_all_running()
        executor.cleanup_stale()
        executor.cleanup_zombies()
        # 清除调度器缓存并重新读取配置
        scheduler.wakeup()

    def on_sigchld(signum, frame):
        """[优化1] SIGCHLD 异步回收：子进程退出时立即回收，防止僵尸进程堆积。"""
        executor.cleanup_zombies()

    signal.signal(signal.SIGTERM, on_sigterm)
    signal.signal(signal.SIGINT, on_sigterm)
    signal.signal(signal.SIGHUP, on_sighup)
    signal.signal(signal.SIGCHLD, on_sigchld)

    scheduler.start()

    logger.info("Daemon ready")

    # Keep alive
    try:
        while True:
            time.sleep(3600)
    except KeyboardInterrupt:
        on_sigterm(signal.SIGINT, None)


if __name__ == '__main__':
    main()
