package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"cronplus/internal/control"
	"cronplus/internal/executor"
	"cronplus/internal/scheduler"
	"cronplus/internal/store"
	"cronplus/pkg/model"
)

var version = "dev" // overwritten by -ldflags at build time

func main() {
	confPath := flag.String("conf", store.DefaultConfFile, "Config file path")
	logDir := flag.String("logs", store.DefaultLogDir, "Logs directory path")
	debug := flag.Bool("d", false, "Enable debug logging")
	showVersion := flag.Bool("v", false, "Show version")
	flag.Parse()

	if *showVersion {
		fmt.Printf("cronplusd v%s\n", version)
		os.Exit(0)
	}

	logWriter := setupLogging(*debug, *confPath, *logDir)

	// Single instance guard.
	//
	// Two daemons reading the same tasks.conf means every schedule fires twice,
	// and the second one steals the control socket from the first (Serve
	// replaces a stale socket file), so `cronplus ps/stop` silently talks to the
	// wrong process. systemd normally prevents this, but a manually started
	// `cronplusd` next to the service did not.
	lockFile, lockErr := acquireLock(filepath.Join(filepath.Dir(*confPath), "cronplus.lock"))
	if lockErr != nil {
		fmt.Fprintf(os.Stderr, "cronplusd: %v\n", lockErr)
		os.Exit(1)
	}
	defer lockFile.Close()

	log.Println("========================================")
	log.Printf("cronplusd v%s starting", version)
	log.Printf("Config: %s", *confPath)
	log.Printf("Logs:   %s", *logDir)

	s := store.New(*confPath, *logDir)
	settings, _ := s.ReadSettings()

	poolSize := settings.MaxConcurrentTasks
	if poolSize <= 0 {
		poolSize = 50
	}
	pool := executor.NewPool(s, poolSize)
	sched := scheduler.New(s, pool)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Control socket: lets `cronplus ps` and `cronplus stop <id>` reach a
	// running task without restarting the service.
	stopControl, err := control.Serve(control.SocketPath(*confPath), func(req control.Request) control.Response {
		switch req.Cmd {
		case "ping":
			// PID included so the CLI can locate the daemon even where systemd
			// is not managing it (containers, `cronplusd` started by hand).
			return control.Response{OK: true, Version: version, PID: os.Getpid()}
		case "ps":
			return control.Response{OK: true, Running: pool.Running()}
		case "reload":
			// Same work as SIGHUP, but reachable where systemd is not the
			// init system (`cronplus reload` used to fail there with
			// "System has not been booted with systemd").
			sched.Wakeup()
			if logWriter != nil {
				logWriter.Reload()
			}
			log.Printf("Control: reload requested")
			return control.Response{OK: true}
		case "stop":
			if req.TaskID <= 0 {
				return control.Response{OK: false, Error: "task_id required"}
			}
			stopped := pool.Kill(req.TaskID)
			if !stopped {
				return control.Response{OK: false, Error: fmt.Sprintf("task %d is not running", req.TaskID)}
			}
			log.Printf("Control: task [%d] stop requested", req.TaskID)
			return control.Response{OK: true, Stopped: true}
		default:
			return control.Response{OK: false, Error: "unknown command"}
		}
	})
	if err != nil {
		log.Printf("Control socket unavailable: %v", err)
	} else {
		log.Printf("Control socket: %s", control.SocketPath(*confPath))
	}

	// Persist scheduler state on a timer instead of on every run.
	flushDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := s.FlushState(); err != nil {
					log.Printf("State flush error: %v", err)
				}
			case <-flushDone:
				return
			}
		}
	}()

	shutdown := func(reason string) {
		log.Printf("Received %s, shutting down gracefully", reason)
		sched.Stop()
		pool.KillAll()
		close(flushDone)
		if err := s.FlushState(); err != nil {
			log.Printf("State flush error: %v", err)
		}
		if stopControl != nil {
			stopControl()
		}
		cancel()
		log.Println("Daemon stopped")
		os.Exit(0)
	}

	// Signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP,
		syscall.SIGUSR1, syscall.SIGUSR2)

	go func() {
		for sig := range sigCh {
			switch sig {
			case syscall.SIGTERM, syscall.SIGINT:
				shutdown(sig.String())

			case syscall.SIGHUP:
				// Reload only. This used to call pool.KillAll(), so saving a task
				// in the WebUI or running `systemctl reload` killed every
				// running task and logged it as a failure.
				log.Println("SIGHUP received — reloading config")
				sched.Wakeup()
				if logWriter != nil {
					logWriter.Reload()
				}

			case syscall.SIGUSR2:
				// Explicit "kill everything" signal, for operators who need it.
				log.Println("SIGUSR2 received — killing all running tasks")
				pool.KillAll()
				sched.Wakeup()

			case syscall.SIGUSR1:
				stats := sched.GetStats()
				running := pool.Running()
				log.Printf("Status: executed=%d failed=%d last_run=%s running=%d",
					stats.TasksExecuted, stats.TasksFailed, stats.LastRun, len(running))
				for _, r := range running {
					log.Printf("  Running: task [%d] pid=%d started=%s duration=%s",
						r.TaskID, r.PID, r.StartTime, executor.FormatDuration(r.Duration))
				}
			}
		}
	}()

	sched.Start(ctx)
	log.Println("Daemon ready")

	// Keep alive
	select {}
}

// acquireLock takes an exclusive, non-blocking lock on path. The lock is held
// for the lifetime of the process (the kernel releases it on exit, including
// SIGKILL), so no stale state can block a restart.
func acquireLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("cannot open lock file %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		pid, _ := os.ReadFile(path)
		f.Close()
		return nil, fmt.Errorf("already running (lock %s held by pid %s)", path, strings.TrimSpace(string(pid)))
	}
	// Best effort: record the pid so the message above is useful.
	f.Truncate(0)
	f.Seek(0, io.SeekStart)
	fmt.Fprintf(f, "%d\n", os.Getpid())
	return f, nil
}

// rotatingWriter keeps the daemon log bounded while the daemon keeps running.
// Rotation used to happen only once, at startup, so a long-lived daemon grew
// its log without limit and daemonLogMaxBytes was effectively ignored.
type rotatingWriter struct {
	mu          sync.Mutex
	path        string
	maxBytes    int
	backupCount int
	file        *os.File
	size        int64
	confPath    string
}

func newRotatingWriter(path, confPath string, maxBytes, backupCount int) *rotatingWriter {
	w := &rotatingWriter{
		path:        path,
		confPath:    confPath,
		maxBytes:    maxBytes,
		backupCount: backupCount,
	}
	w.open()
	return w
}

func (w *rotatingWriter) open() {
	f, err := os.OpenFile(w.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	w.file = f
	if info, err := f.Stat(); err == nil {
		w.size = info.Size()
	}
}

// Reload re-reads the rotation settings (called on SIGHUP).
func (w *rotatingWriter) Reload() {
	settings, err := readLogSettings(w.confPath)
	if err != nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if settings.DaemonLogMaxBytes > 0 {
		w.maxBytes = settings.DaemonLogMaxBytes
	}
	if settings.DaemonLogBackupCount > 0 {
		w.backupCount = settings.DaemonLogBackupCount
	}
}

func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		w.open()
		if w.file == nil {
			return len(p), nil
		}
	}
	if w.maxBytes > 0 && w.size+int64(len(p)) > int64(w.maxBytes) {
		w.rotateLocked()
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *rotatingWriter) rotateLocked() {
	w.file.Close()
	w.file = nil
	rotateLog(w.path, 0, w.backupCount)
	w.size = 0
	w.open()
}

func setupLogging(debug bool, confPath, logDir string) *rotatingWriter {
	level := log.LstdFlags
	if debug {
		level = log.LstdFlags | log.Lshortfile
	}
	log.SetFlags(level)
	log.SetOutput(os.Stderr)

	maxBytes := 10 * 1024 * 1024 // 10MB default
	backupCount := 3
	if s, err := readLogSettings(confPath); err == nil {
		if s.DaemonLogMaxBytes > 0 {
			maxBytes = s.DaemonLogMaxBytes
		}
		if s.DaemonLogBackupCount > 0 {
			backupCount = s.DaemonLogBackupCount
		}
		if s.DaemonLogLevel == "debug" {
			log.SetFlags(log.LstdFlags | log.Lshortfile)
		}
	}

	logPath := filepath.Join(logDir, "cronplus.log")
	os.MkdirAll(filepath.Dir(logPath), 0700)

	w := newRotatingWriter(logPath, confPath, maxBytes, backupCount)
	if w.file != nil {
		log.SetOutput(w)
	}
	return w
}

func readLogSettings(confPath string) (model.Settings, error) {
	settingsPath := filepath.Join(filepath.Dir(confPath), "settings.json")
	settings := model.DefaultSettings()
	_, err := store.ReadJSON(settingsPath, &settings)
	if err != nil {
		return model.DefaultSettings(), err
	}
	return settings, nil
}

// rotateLog shifts path → path.1 → path.2 … and drops anything past
// backupCount. maxBytes is only used by the startup call.
func rotateLog(path string, maxBytes, backupCount int) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	if maxBytes > 0 && int(info.Size()) < maxBytes {
		return
	}
	if backupCount <= 0 {
		os.Remove(path)
		return
	}
	os.Remove(fmt.Sprintf("%s.%d", path, backupCount))
	for i := backupCount - 1; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", path, i)
		dst := fmt.Sprintf("%s.%d", path, i+1)
		if _, err := os.Stat(src); err == nil {
			os.Rename(src, dst)
		}
	}
	os.Rename(path, fmt.Sprintf("%s.1", path))
}
