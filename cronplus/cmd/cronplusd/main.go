package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"cronplus/internal/executor"
	"cronplus/internal/scheduler"
	"cronplus/internal/store"
	"cronplus/pkg/model"
)

var version = "2.0.7"

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

	setupLogging(*debug, *confPath)

	log.Println("========================================")
	log.Printf("cronplusd v%s starting", version)
	log.Printf("Config: %s", *confPath)
	log.Printf("Logs:   %s", *logDir)

	s := store.New(*confPath, *logDir)
	pool := executor.NewPool(s, 50)
	sched := scheduler.New(s, pool)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP, syscall.SIGUSR1)

	go func() {
		for sig := range sigCh {
			switch sig {
			case syscall.SIGTERM, syscall.SIGINT:
				log.Printf("Received %s, shutting down", sig)
				sched.Stop()
				pool.KillAll()
				cancel()
				os.Exit(0)

			case syscall.SIGHUP:
				log.Println("SIGHUP received — full reset")
				pool.KillAll()
				sched.Wakeup()

			case syscall.SIGUSR1:
				stats := sched.GetStats()
				running := pool.Running()
				log.Printf("Status: executed=%d failed=%d last_run=%s running=%d",
					stats.TasksExecuted, stats.TasksFailed, stats.LastRun, len(running))
				for _, r := range running {
					log.Printf("  Running: task [%d] pid=%d started=%s duration=%dms",
						r.TaskID, r.PID, r.StartTime, r.Duration)
				}
			}
		}
	}()

	sched.Start(ctx)
	log.Println("Daemon ready")

	// Keep alive
	select {}
}

func setupLogging(debug bool, confPath string) {
	level := log.LstdFlags
	if debug {
		level = log.LstdFlags | log.Lshortfile
	}
	log.SetFlags(level)
	log.SetOutput(os.Stderr)

	// Read rotation settings
	maxBytes := 10 * 1024 * 1024 // 10MB default
	backupCount := 3
	if s, err := readLogSettings(confPath); err == nil {
		if s.DaemonLogMaxBytes > 0 {
			maxBytes = s.DaemonLogMaxBytes
		}
		if s.DaemonLogBackupCount > 0 {
			backupCount = s.DaemonLogBackupCount
		}
	}

	logPath := filepath.Join(store.DefaultLogDir, "cronplus.log")
	os.MkdirAll(filepath.Dir(logPath), 0755)

	// Rotate if needed
	rotateLog(logPath, maxBytes, backupCount)

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		log.SetOutput(f)
	}
}

func readLogSettings(confPath string) (model.Settings, error) {
	settingsPath := filepath.Join(filepath.Dir(confPath), "settings.json")
	var s model.Settings
	_, err := store.ReadJSON(settingsPath, &s)
	if err != nil {
		return model.DefaultSettings(), err
	}
	return s, nil
}

func rotateLog(path string, maxBytes, backupCount int) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	if int(info.Size()) < maxBytes {
		return
	}
	// Shift backup files: .log.1 → .log.2, .log → .log.1
	for i := backupCount - 1; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", path, i)
		dst := fmt.Sprintf("%s.%d", path, i+1)
		if _, err := os.Stat(src); err == nil {
			os.Rename(src, dst)
		}
	}
	// Current → .1
	os.Rename(path, fmt.Sprintf("%s.1", path))
	// Remove files beyond backup count
	for i := backupCount + 1; ; i++ {
		f := fmt.Sprintf("%s.%d", path, i)
		if _, err := os.Stat(f); os.IsNotExist(err) {
			break
		}
		os.Remove(f)
	}
}
