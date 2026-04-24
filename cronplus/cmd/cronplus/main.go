package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"cronplus/internal/executor"
	"cronplus/internal/store"
	"cronplus/pkg/model"
)

var version = "dev" // overwritten by -ldflags at build time
const serviceName = "cronplus.service"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(0)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	var exitCode int
	switch cmd {
	case "start":
		exitCode = cmdStart(args)
	case "stop":
		exitCode = cmdStop(args)
	case "restart":
		exitCode = cmdRestart(args)
	case "reload":
		exitCode = cmdReload(args)
	case "status":
		exitCode = cmdStatus(args)
	case "list", "ls":
		exitCode = cmdList(args)
	case "run":
		exitCode = cmdRun(args)
	case "logs":
		exitCode = cmdLogs(args)
	case "export":
		exitCode = cmdExport(args)
	case "import":
		exitCode = cmdImport(args)
	case "clear-logs":
		exitCode = cmdClearLogs(args)
	case "cleanup":
		exitCode = cmdCleanup(args)
	case "settings":
		exitCode = cmdSettings(args)
	case "version", "-v", "--version":
		fmt.Printf("cronplus v%s\n", version)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		printUsage()
		exitCode = 1
	}
	os.Exit(exitCode)
}

func printUsage() {
	fmt.Println(`Usage: cronplus <command> [args]

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
  cronplus restart`)
}

func defaultStore() *store.Store {
	return store.New("", "")
}

func systemctl(action string) int {
	cmd := exec.Command("systemctl", action, serviceName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		return 1
	}
	return 0
}

func daemonPID() int {
	cmd := exec.Command("systemctl", "show", "-p", "MainPID", "--value", serviceName)
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	if pid <= 0 {
		return 0
	}
	return pid
}

func cmdStart(_ []string) int {
	if pid := daemonPID(); pid > 0 {
		fmt.Println("cronplus: already running")
		return 0
	}
	return systemctl("start")
}

func cmdStop(_ []string) int {
	if pid := daemonPID(); pid == 0 {
		fmt.Println("cronplus: not running")
		return 0
	}
	return systemctl("stop")
}

func cmdRestart(_ []string) int {
	return systemctl("restart")
}

func cmdReload(_ []string) int {
	if pid := daemonPID(); pid == 0 {
		fmt.Println("cronplus: not running")
		return 1
	}
	return systemctl("reload")
}

func cmdStatus(_ []string) int {
	pid := daemonPID()
	if pid > 0 {
		fmt.Printf("cronplus: running (pid %d)\n", pid)
	} else {
		fmt.Println("cronplus: not running")
	}

	s := defaultStore()
	tasks, err := s.ListTasks()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading tasks: %v\n", err)
		return 1
	}
	enabled := 0
	for _, t := range tasks {
		if t.Enabled {
			enabled++
		}
	}
	fmt.Printf("Tasks: %d total, %d enabled\n", len(tasks), enabled)

	// Count logs across all tasks
	allLogs, _ := s.ListLogs(0)
	success, failed := 0, 0
	for _, l := range allLogs {
		if l.Status == "success" {
			success++
		} else {
			failed++
		}
	}
	fmt.Printf("Logs: %d entries (%d success, %d failed)\n", len(allLogs), success, failed)

	if _, err := os.Stat("/run/cronplus"); err == nil {
		fmt.Println("Reboot marker: present (will skip @reboot tasks on next start)")
	} else {
		fmt.Println("Reboot marker: absent (will fire @reboot tasks on next start)")
	}
	return 0
}

func cmdList(_ []string) int {
	s := defaultStore()
	tasks, err := s.ListTasks()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	if len(tasks) == 0 {
		fmt.Println("No tasks configured")
		return 0
	}
	for _, t := range tasks {
		icon := "✓"
		if !t.Enabled {
			icon = "✗"
		}
		title := t.Title
		if title == "" {
			title = t.Command
			if len(title) > 50 {
				title = title[:50]
			}
		}
		fmt.Printf("  %s [%d] %s  (%s)\n", icon, t.ID, title, t.Schedule)
	}
	return 0
}

func cmdRun(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: cronplus run <task_id>")
		return 1
	}
	taskID, err := strconv.Atoi(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid task ID: %s\n", args[0])
		return 1
	}

	s := defaultStore()
	task, err := s.GetTask(taskID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	title := task.Title
	if title == "" {
		title = task.Command
		if len(title) > 60 {
			title = title[:60]
		}
	}
	runID := executor.NewRunID()
	fmt.Printf("▶ [%d] %s [manual] run=%s\n", taskID, title, runID)

	// Execute command directly with real-time streaming output
	command := executor.DecodeCommand(task.Command)
	var cmd *exec.Cmd
	if task.RunUser != "" && task.RunUser != "root" {
		cmd = exec.Command("su", "-", task.RunUser, "-c", command)
	} else {
		cmd = exec.Command("bash", "-c", command)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if task.Cwd != "" {
		cmd.Dir = expandPath(task.Cwd)
	}
	cmd.Env = buildEnv(task.EnvVars)

	// Stream stdout + stderr in real time
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	startTime := time.Now()
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting command: %v\n", err)
		return 1
	}

	pgid := cmd.Process.Pid

	// Signal handler: forward SIGINT/SIGTERM to child process group
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	done := make(chan error, 1)

	go func() {
		done <- cmd.Wait()
	}()

	go func() {
		for sig := range sigCh {
			fmt.Fprintf(os.Stderr, "\n[manual] received %s, stopping...\n", sig)
			syscall.Kill(-pgid, syscall.SIGTERM)
		}
	}()

	// Wait for completion, timeout, or signal
	var timedOut bool
	var runErr error

	if task.Timeout > 0 {
		select {
		case runErr = <-done:
			// finished
		case <-time.After(time.Duration(task.Timeout) * time.Second):
			timedOut = true
			fmt.Fprintf(os.Stderr, "\n[manual] timeout (%ds), killing process group...\n", task.Timeout)
			syscall.Kill(-pgid, syscall.SIGTERM)
			time.Sleep(500 * time.Millisecond)
			syscall.Kill(-pgid, syscall.SIGKILL)
			<-done
		}
	} else {
		runErr = <-done
	}

	signal.Stop(sigCh)
	close(sigCh)

	durationMs := int(time.Since(startTime).Milliseconds())

	if timedOut {
		fmt.Fprintf(os.Stderr, "[%s] error: command timeout (%ds) terminated\n", runID, task.Timeout)
		writeRunLog(runID, s, task, command, "error", fmt.Sprintf("[cronplus] command timeout (%ds) terminated\n", task.Timeout), durationMs, -1)
		return 1
	}

	exitCode := 0
	status := "success"
	if runErr != nil {
		status = "error"
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	icon := "✓"
	if status != "success" {
		icon = "✗"
	}
	fmt.Printf("\n%s [%s] %s (exit=%d, %dms) [%s] run=%s\n", icon, status, title, exitCode, durationMs, "manual", runID)
	writeRunLog(runID, s, task, command, status, "", durationMs, exitCode)

	if status != "success" {
		return 1
	}
	return 0
}

func writeRunLog(runID string, s *store.Store, task *model.Task, command, status, output string, durationMs, exitCode int) {
	entry := model.LogEntry{
		RunID:     runID,
		TaskID:    task.ID,
		Title:     task.Title,
		Command:   command,
		Status:    status,
		Output:    output,
		Duration:  durationMs,
		ExitCode:  exitCode,
		Attempt:   1,
		Trigger:   "manual",
		RunUser:   task.RunUser,
		CreatedAt: time.Now().Format("2006-01-02 15:04:05"),
	}
	if err := s.AppendLog(entry); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write log: %v\n", err)
	}
}

func buildEnv(envVars map[string]string) []string {
	safe, rejected := executor.SanitizeEnvVars(envVars)
	if len(rejected) > 0 {
		fmt.Fprintf(os.Stderr, "[security] Blocked dangerous env vars: %v\n", rejected)
	}
	env := os.Environ()
	for k, v := range safe {
		env = append(env, k+"="+v)
	}
	return env
}

func expandPath(path string) string {
	if len(path) > 0 && path[0] == '~' {
		if home := os.Getenv("HOME"); home != "" {
			return home + path[1:]
		}
	}
	return path
}

func cmdLogs(args []string) int {
	s := defaultStore()
	taskID := 0
	limit := 50
	jsonOutput := false
	showAll := false

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-n", "--limit":
			if i+1 < len(args) {
				limit, _ = strconv.Atoi(args[i+1])
				i++
			}
		case "--json", "-j":
			jsonOutput = true
		case "--all", "-a":
			showAll = true
			taskID = 0
		default:
			if taskID == 0 && !strings.HasPrefix(args[i], "-") {
				taskID, _ = strconv.Atoi(args[i])
			}
		}
	}

	if showAll {
		taskID = 0
	}

	logs, err := s.ListLogs(taskID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	// Sort newest first
	sort.Slice(logs, func(i, j int) bool {
		return logs[i].CreatedAt > logs[j].CreatedAt
	})

	if limit > 0 && len(logs) > limit {
		logs = logs[:limit]
	}

	if jsonOutput {
		// Reverse to chronological for JSON
		for i := len(logs)/2 - 1; i >= 0; i-- {
			opp := len(logs) - 1 - i
			logs[i], logs[opp] = logs[opp], logs[i]
		}
		enc := json.NewEncoder(os.Stdout)
		enc.Encode(logs)
		return 0
	}

	if len(logs) == 0 {
		fmt.Println("No logs")
		return 0
	}

	// Reverse to show newest last (chronological)
	for i := len(logs)/2 - 1; i >= 0; i-- {
		opp := len(logs) - 1 - i
		logs[i], logs[opp] = logs[opp], logs[i]
	}

	for _, l := range logs {
		icon := "✓"
		if l.Status != "success" {
			icon = "✗"
		}
		title := l.Title
		if title == "" {
			title = l.Command
			if len(title) > 50 {
				title = title[:50]
			}
		}
		fmt.Printf("  %s [%s] %s  (%dms)\n", icon, l.CreatedAt, title, l.Duration)
	}
	return 0
}

func cmdExport(_ []string) int {
	s := defaultStore()
	tasks, err := s.ListTasks()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	data := map[string]interface{}{
		"version":    version,
		"exportTime": "now",
		"source":     "cronplus",
		"tasks":      tasks,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(data)
	return 0
}

func cmdImport(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: cronplus import <file>")
		return 1
	}

	// Validate file path — must be a regular file, not symlink/dev/proc
	fi, err := os.Lstat(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		return 1
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		fmt.Fprintln(os.Stderr, "Error: symlinks are not allowed for import")
		return 1
	}
	if !fi.Mode().IsRegular() {
		fmt.Fprintln(os.Stderr, "Error: import target must be a regular file")
		return 1
	}

	// Limit import file size to 10MB
	const maxImportSize = 10 * 1024 * 1024
	if fi.Size() > maxImportSize {
		fmt.Fprintf(os.Stderr, "Error: import file too large (%d bytes, max %d)\n", fi.Size(), maxImportSize)
		return 1
	}

	data, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		return 1
	}
	var wrapper struct {
		Version    string       `json:"version"`
		Source     string       `json:"source"`
		ExportTime string       `json:"exportTime"`
		Tasks      []model.Task `json:"tasks"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing JSON: %v\n", err)
		return 1
	}

	// Validate source field if present
	if wrapper.Source != "" && wrapper.Source != "cronplus" {
		fmt.Fprintf(os.Stderr, "Warning: import source is %q, expected 'cronplus'\n", wrapper.Source)
	}

	s := defaultStore()
	count := 0
	skipped := 0
	for _, t := range wrapper.Tasks {
		if t.Command == "" {
			skipped++
			continue
		}
		// Validate schedule
		if t.Schedule != "" && t.Schedule != "@reboot" {
			parts := strings.Fields(t.Schedule)
			if len(parts) != 5 && len(parts) != 6 {
				fmt.Fprintf(os.Stderr, "Warning: task %q has invalid schedule %q, skipping\n", t.Title, t.Schedule)
				skipped++
				continue
			}
		}
		// Validate run_user
		if err := model.ValidateRunUser(t.RunUser); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: task %q: %v, skipping\n", t.Title, err)
			skipped++
			continue
		}
		t.ID = 0 // auto-assign
		if err := s.CreateTask(&t); err != nil {
			fmt.Fprintf(os.Stderr, "Error importing task: %v\n", err)
			continue
		}
		count++
	}
	fmt.Printf("Imported %d tasks", count)
	if skipped > 0 {
		fmt.Printf(" (%d skipped)", skipped)
	}
	fmt.Println()

	// Signal daemon to reload
	if pid := daemonPID(); pid > 0 {
		syscall.Kill(pid, syscall.SIGHUP)
	}
	return 0
}

func cmdClearLogs(args []string) int {
	s := defaultStore()
	taskID := 0
	if len(args) > 0 {
		taskID, _ = strconv.Atoi(args[0])
	}
	if err := s.ClearLogs(taskID); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	if taskID > 0 {
		fmt.Printf("Logs cleared for task %d\n", taskID)
	} else {
		fmt.Println("All logs cleared")
	}
	return 0
}

func cmdCleanup(_ []string) int {
	pid := daemonPID()
	if pid == 0 {
		fmt.Println("cronplus: not running, nothing to clean up")
		return 0
	}
	syscall.Kill(pid, syscall.SIGHUP)
	fmt.Println("Cleanup signal sent (SIGHUP)")
	return 0
}

func cmdSettings(args []string) int {
	s := defaultStore()

	action := "show"
	if len(args) > 0 {
		action = args[0]
	}

	switch action {
	case "show":
		settings, err := s.ReadSettings()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return 1
		}
		data, _ := json.MarshalIndent(settings, "", "  ")
		fmt.Println(string(data))
		return 0
	case "set":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: cronplus settings set <key> <value>")
			return 1
		}
		settings, _ := s.ReadSettings()
		key, value := args[1], args[2]

		switch key {
		case "language":
			allowed := []string{"en", "zh-CN", "ja", "ko", "de", "es", "fr", "pt-BR", "ru"}
			valid := false
			for _, a := range allowed {
				if value == a {
					valid = true
					break
				}
			}
			if !valid {
				fmt.Fprintf(os.Stderr, "Invalid language: %s (allowed: %v)\n", value, allowed)
				return 1
			}
			settings.Language = value
		case "theme":
			if value != "auto" && value != "dark" && value != "light" {
				fmt.Fprintf(os.Stderr, "Invalid theme: %s (allowed: auto, dark, light)\n", value)
				return 1
			}
			settings.Theme = value
		case "defaultRunUser":
			if err := model.ValidateRunUser(value); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				return 1
			}
			settings.DefaultRunUser = value
		case "defaultTimeout":
			v, err := strconv.Atoi(value)
			if err != nil || v < 0 || v > 86400 {
				fmt.Fprintf(os.Stderr, "Invalid timeout: %s (must be 0-86400 seconds)\n", value)
				return 1
			}
			settings.DefaultTimeout = v
		case "defaultMaxRetries":
			v, err := strconv.Atoi(value)
			if err != nil || v < 0 || v > 100 {
				fmt.Fprintf(os.Stderr, "Invalid max_retries: %s (must be 0-100)\n", value)
				return 1
			}
			settings.DefaultMaxRetries = v
		case "defaultRetryInterval":
			v, err := strconv.Atoi(value)
			if err != nil || v < 0 || v > 86400 {
				fmt.Fprintf(os.Stderr, "Invalid retry_interval: %s (must be 0-86400 seconds)\n", value)
				return 1
			}
			settings.DefaultRetryInterval = v
		case "autoRefreshInterval":
			v, err := strconv.Atoi(value)
			if err != nil || v < 1 || v > 3600 {
				fmt.Fprintf(os.Stderr, "Invalid autoRefreshInterval: %s (must be 1-3600 seconds)\n", value)
				return 1
			}
			settings.AutoRefreshInterval = v
		case "logPageSize":
			v, err := strconv.Atoi(value)
			if err != nil || v < 1 || v > 1000 {
				fmt.Fprintf(os.Stderr, "Invalid logPageSize: %s (must be 1-1000)\n", value)
				return 1
			}
			settings.LogPageSize = v
		case "logMaxBytes":
			v, err := strconv.Atoi(value)
			if err != nil || v < 1024 || v > 100*1024*1024 {
				fmt.Fprintf(os.Stderr, "Invalid logMaxBytes: %s (must be 1KB-100MB)\n", value)
				return 1
			}
			settings.LogMaxBytes = v
		case "logBackupCount":
			v, err := strconv.Atoi(value)
			if err != nil || v < 0 || v > 100 {
				fmt.Fprintf(os.Stderr, "Invalid logBackupCount: %s (must be 0-100)\n", value)
				return 1
			}
			settings.LogBackupCount = v
		default:
			fmt.Fprintf(os.Stderr, "Unknown setting: %s\n", key)
			return 1
		}
		if err := s.WriteSettings(settings); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return 1
		}
		fmt.Printf("Set %s = %s\n", key, value)
		return 0
	case "reset":
		if err := s.WriteSettings(model.DefaultSettings()); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return 1
		}
		fmt.Println("Settings reset to defaults")
		return 0
	default:
		fmt.Fprintf(os.Stderr, "Unknown settings action: %s\n", action)
		return 1
	}
}
