package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"cronplus/internal/control"
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
	case "ps":
		exitCode = cmdPS(args)
	case "logs":
		exitCode = cmdLogs(args)
	case "export":
		exitCode = cmdExport(args)
	case "import":
		exitCode = cmdImport(args)
	case "clear-logs":
		exitCode = cmdClearLogs(args)
	case "prune":
		exitCode = cmdLogsPrune(args)
	case "cleanup":
		exitCode = cmdCleanup(args)
	case "settings":
		exitCode = cmdSettings(args)
	case "version", "-v", "--version":
		fmt.Printf("cronplus v%s\n", version)
	case "help", "-h", "--help":
		printUsage()
		exitCode = 0
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
  restart      Restart the daemon
  reload       Reload config without restart (running tasks are not touched)
  status       Show daemon & task status
  list (ls)    List all tasks
  run <id>     Run a task immediately (streams output, writes the run log)
  ps           Show tasks that are running right now
  stop [id]    Stop a running task, or the daemon when no id is given
  logs [id]    Show execution logs
               (-n N = max entries, --per-task N = per task,
                --no-output = skip output, --run-id ID = one entry)
  prune        Delete selected log entries (see 'cronplus prune --help')
  export       Export tasks to stdout (JSON)
  import <f>   Import tasks from JSON file
  clear-logs   Clear all execution logs
  cleanup      Apply retention rules and clean up stale temp files
  settings     Show/update settings
  help         Show this help
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

// daemonPID returns the daemon's pid, or 0 when it is not running.
//
// systemd is asked first (the packaged service is the normal case), then the
// control socket: without that fallback every command that needs to signal the
// daemon reported "not running" whenever systemd was not supervising it, even
// though the daemon was alive and answering on its socket.
func daemonPID() int {
	cmd := exec.Command("systemctl", "show", "-p", "MainPID", "--value", serviceName)
	// Probing must stay quiet: on a host without systemd (container, PID 1 not
	// systemd) systemctl itself writes "System has not been booted with systemd"
	// to stderr, which used to leak into the output of every cronplus command.
	cmd.Stderr = io.Discard
	if out, err := cmd.Output(); err == nil {
		if pid, _ := strconv.Atoi(strings.TrimSpace(string(out))); pid > 0 {
			return pid
		}
	}
	if resp, err := control.Do(control.SocketPath(store.DefaultConfFile), control.Request{Cmd: "ping"}); err == nil && resp.OK {
		return resp.PID
	}
	return 0
}

func cmdStart(_ []string) int {
	if pid := daemonPID(); pid > 0 {
		fmt.Println("cronplus: already running")
		return 0
	}
	return systemctl("start")
}

func cmdStop(args []string) int {
	// `cronplus stop <id>` stops one task; `cronplus stop` stops the daemon.
	if len(args) > 0 {
		taskID, err := strconv.Atoi(args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid task ID: %s\n", args[0])
			return 1
		}
		resp, err := control.Do(control.SocketPath(store.DefaultConfFile), control.Request{Cmd: "stop", TaskID: taskID})
		if err != nil {
			fmt.Fprintf(os.Stderr, "cronplus: daemon not reachable (%v)\n", err)
			return 1
		}
		if !resp.OK {
			fmt.Fprintf(os.Stderr, "Error: %s\n", resp.Error)
			return 1
		}
		fmt.Printf("Task %d stopped\n", taskID)
		return 0
	}
	if pid := daemonPID(); pid == 0 {
		fmt.Println("cronplus: not running")
		return 0
	}
	return systemctl("stop")
}

func cmdPS(args []string) int {
	jsonOutput := false
	for _, a := range args {
		if a == "--json" || a == "-j" {
			jsonOutput = true
		}
	}
	resp, err := control.Do(control.SocketPath(store.DefaultConfFile), control.Request{Cmd: "ps"})
	if err != nil {
		if jsonOutput {
			fmt.Println("[]")
		} else {
			fmt.Println("cronplus: daemon not reachable")
		}
		return 0
	}
	if jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.Encode(resp.Running)
		return 0
	}
	if len(resp.Running) == 0 {
		fmt.Println("No running tasks")
		return 0
	}
	for _, r := range resp.Running {
		fmt.Printf("  [%d] pid=%d %s started=%s duration=%s run=%s\n",
			r.TaskID, r.PID, r.Trigger, r.StartTime, executor.FormatDuration(r.Duration), r.RunID)
	}
	return 0
}

func cmdRestart(_ []string) int {
	return systemctl("restart")
}

func cmdReload(_ []string) int {
	// Prefer the control socket: `systemctl reload` needs systemd as PID 1,
	// which is not the case in containers or when cronplusd was started by
	// hand, so reload used to fail even though the daemon was running.
	if resp, err := control.Do(control.SocketPath(store.DefaultConfFile), control.Request{Cmd: "reload"}); err == nil && resp.OK {
		fmt.Println("cronplus: config reloaded")
		return 0
	}
	pid := daemonPID()
	if pid == 0 {
		fmt.Println("cronplus: not running")
		return 1
	}
	if err := systemctl("reload"); err != 0 {
		// Last resort: the documented signal path.
		if sigErr := syscall.Kill(pid, syscall.SIGHUP); sigErr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", sigErr)
			return 1
		}
		fmt.Println("cronplus: config reloaded (SIGHUP)")
	}
	return 0
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

	if resp, err := control.Do(control.SocketPath(store.DefaultConfFile), control.Request{Cmd: "ps"}); err == nil {
		fmt.Printf("Running: %d\n", len(resp.Running))
		for _, r := range resp.Running {
			fmt.Printf("  [%d] pid=%d started=%s duration=%s\n",
				r.TaskID, r.PID, r.StartTime, executor.FormatDuration(r.Duration))
		}
	}

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

	// A manual run and a scheduled run of the same task must not overlap. Ask
	// the daemon first: stop the running instance when the task asks for it,
	// refuse otherwise.
	if running, ok := daemonRunning(taskID); ok {
		if !task.KillPrevious {
			fmt.Fprintf(os.Stderr, "Task %d is already running (pid %d).\nStop it first: cronplus stop %d\n",
				taskID, running.PID, taskID)
			return 1
		}
		if resp, err := control.Do(control.SocketPath(store.DefaultConfFile), control.Request{Cmd: "stop", TaskID: taskID}); err != nil || !resp.OK {
			fmt.Fprintf(os.Stderr, "Warning: could not stop the running instance\n")
		}
	}

	// Manual runs honour the global default timeout too; the scheduled path has
	// always done this, so the two disagreed.
	if task.Timeout <= 0 {
		if settings, err := s.ReadSettings(); err == nil && settings.DefaultTimeout > 0 {
			task.Timeout = settings.DefaultTimeout
		}
	}

	title := task.Title
	if title == "" {
		title = task.Command
		if len(title) > 60 {
			title = title[:60]
		}
	}
	runID := executor.NewManualRunID(taskID, task.RunUser)
	fmt.Printf("▶ [%d] %s [manual] run=%s\n", taskID, title, runID)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	// SIGHUP is included on purpose: when the WebUI closes the Cockpit channel
	// it terminates the process with SIGHUP/SIGTERM depending on the transport.
	// Handling it here means "stop" always runs the orderly path — kill the
	// task's process group and still write the log entry — instead of dying
	// silently and leaving a run with no history.
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	stopped := make(chan struct{})
	go func() {
		select {
		case <-sigCh:
			fmt.Fprintf(os.Stderr, "\n[manual] stopping...\n")
			cancel()
		case <-stopped:
		}
	}()

	command := executor.DecodeCommand(task.Command)
	// Same executor as the daemon: identical timeout handling, env sanitising
	// and output truncation. Output is streamed to stdout *and* captured for
	// the log, which used to be stored empty for manual CLI runs.
	status, output, durationMs, exitCode := executor.RunCommand(ctx, *task, command, os.Stdin, os.Stdout, nil)
	signal.Stop(sigCh)
	close(stopped)
	close(sigCh)

	writeRunLog(runID, s, task, command, status, output, durationMs, exitCode)

	icon := "✓"
	if status != "success" {
		icon = "✗"
	}
	fmt.Printf("\n%s [%s] %s (exit=%d, %s) [%s] run=%s\n",
		icon, status, title, exitCode, executor.FormatDuration(durationMs), "manual", runID)

	if status != "success" {
		// Propagate the task's own exit status so callers can rely on
		// `cronplus run <id>` the way they would on the command itself. It used
		// to always return 1, so `cronplus run 2 && ...` could not tell a
		// task exiting 7 from one that never started. A negative status means
		// killed/timed out, which has no portable exit code — use 1 for that.
		if exitCode > 0 && exitCode < 256 {
			return exitCode
		}
		return 1
	}
	return 0
}

// daemonRunning asks the control socket whether a task is currently running.
func daemonRunning(taskID int) (executor.RunningInfo, bool) {
	resp, err := control.Do(control.SocketPath(store.DefaultConfFile), control.Request{Cmd: "ps"})
	if err != nil {
		return executor.RunningInfo{}, false
	}
	for _, r := range resp.Running {
		if r.TaskID == taskID {
			return r, true
		}
	}
	return executor.RunningInfo{}, false
}

// writeRunLog stores a manual run in the task's log. It goes through the store
// so rotation, permissions and the JSONL format stay identical to scheduled
// runs; the WebUI used to keep its own copy of this logic in JavaScript.
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

func cmdExport(_ []string) int {
	s := defaultStore()
	tasks, err := s.ListTasks()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	data := map[string]interface{}{
		"version":    version,
		"exportTime": time.Now().Format(time.RFC3339),
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
	// One read/write cycle for the whole file: CreateTask in a loop re-read and
	// rewrote tasks.conf for every imported task (O(n²) plus n fsyncs).
	var toImport []model.Task
	for _, t := range wrapper.Tasks {
		t.ID = 0 // auto-assign
		toImport = append(toImport, t)
	}
	count, invalid, err := s.CreateTasks(toImport)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error importing tasks: %v\n", err)
		return 1
	}
	for _, e := range invalid {
		fmt.Fprintf(os.Stderr, "Warning: skipped %v\n", e)
	}
	skipped += len(invalid)

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

func cmdLogs(args []string) int {
	s := defaultStore()
	taskID := 0
	limit := 50
	perTask := 0
	jsonOutput := false
	noOutput := false
	runID := ""

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-n", "--limit":
			if i+1 < len(args) {
				limit, _ = strconv.Atoi(args[i+1])
				i++
			}
		case "-p", "--per-task":
			if i+1 < len(args) {
				perTask, _ = strconv.Atoi(args[i+1])
				i++
			}
		case "--json", "-j":
			jsonOutput = true
		case "--no-output":
			// Summary mode: skip the (up to 256KB) output of every entry. The
			// WebUI loads one entry's output only when it is expanded.
			noOutput = true
		case "--run-id":
			// Fetch exactly one entry, output included (used by the WebUI when
			// an entry is expanded).
			if i+1 < len(args) {
				runID = args[i+1]
				i++
			}
		case "--all", "-a":
			taskID = 0
		default:
			if taskID == 0 && !strings.HasPrefix(args[i], "-") {
				taskID, _ = strconv.Atoi(args[i])
			}
		}
	}

	var logs []model.LogEntry
	var err error
	if taskID > 0 {
		// Distinguish "no such task" from "this task has not run yet". Both
		// printed "No logs" before, so a typo looked like an empty history.
		known := false
		if tasks, lerr := s.ListTasks(); lerr == nil {
			for _, t := range tasks {
				if t.ID == taskID {
					known = true
					break
				}
			}
		}
		if !known && !s.HasTaskLog(taskID) {
			fmt.Fprintf(os.Stderr, "Error: task %d not found\n", taskID)
			return 1
		}
	}
	if taskID == 0 && perTask > 0 {
		logs, err = s.ListLogsPerTask(perTask)
	} else {
		logs, err = s.ListLogs(taskID)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	if runID != "" {
		filtered := make([]model.LogEntry, 0, 1)
		for _, l := range logs {
			if l.RunID == runID {
				filtered = append(filtered, l)
			}
		}
		logs = filtered
	}

	// Sort newest first
	sort.Slice(logs, func(i, j int) bool {
		return logs[i].CreatedAt > logs[j].CreatedAt
	})

	if limit > 0 && len(logs) > limit {
		logs = logs[:limit]
	}

	if noOutput {
		for i := range logs {
			logs[i].Output = ""
		}
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
		fmt.Printf("  %s [%s] %s  (%s)\n", icon, l.CreatedAt, title, executor.FormatDuration(l.Duration))
	}
	return 0
}

// cmdLogsPrune deletes selected log entries. The WebUI used to delete a whole
// task's history for any cleanup mode, which threw away entries the preview
// said would be kept.
func cmdLogsPrune(args []string) int {
	filter := store.PruneFilter{}
	jsonOutput := false

	needValue := func(i int, flag string) (string, bool) {
		if i+1 >= len(args) {
			fmt.Fprintf(os.Stderr, "Error: %s needs a value\n", flag)
			return "", false
		}
		return args[i+1], true
	}

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--task":
			v, ok := needValue(i, "--task")
			if !ok {
				return 1
			}
			filter.TaskID, _ = strconv.Atoi(v)
			i++
		case "--status":
			v, ok := needValue(i, "--status")
			if !ok {
				return 1
			}
			filter.Status = v
			i++
		case "--user":
			v, ok := needValue(i, "--user")
			if !ok {
				return 1
			}
			filter.User = v
			i++
		case "--trigger":
			v, ok := needValue(i, "--trigger")
			if !ok {
				return 1
			}
			filter.Trigger = v
			i++
		case "--before":
			v, ok := needValue(i, "--before")
			if !ok {
				return 1
			}
			filter.Before = v
			i++
		case "--after":
			v, ok := needValue(i, "--after")
			if !ok {
				return 1
			}
			filter.After = v
			i++
		case "--keep":
			v, ok := needValue(i, "--keep")
			if !ok {
				return 1
			}
			filter.Keep, _ = strconv.Atoi(v)
			i++
		case "--dry-run":
			filter.DryRun = true
		case "--json":
			jsonOutput = true
		case "-h", "--help":
			printPruneUsage()
			return 0
		default:
			fmt.Fprintf(os.Stderr, "Unknown option: %s\n", args[i])
			printPruneUsage()
			return 1
		}
	}

	deleted, err := defaultStore().Prune(filter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	if jsonOutput {
		out, _ := json.Marshal(map[string]int{"deleted": deleted})
		fmt.Println(string(out))
		return 0
	}
	if filter.DryRun {
		fmt.Printf("Would delete %d entries\n", deleted)
		return 0
	}
	fmt.Printf("Deleted %d entries\n", deleted)
	return 0
}

func printPruneUsage() {
	fmt.Println(`Usage: cronplus prune [filters] [--keep N] [--dry-run] [--json]

Deletes log entries matching ALL given filters. At least one filter is required.

  --task <id>          only this task
  --status <s>         success | error
  --user <u>           only runs of this user
  --trigger <t>        auto | manual
  --before <datetime>  entries older than "YYYY-MM-DD HH:MM:SS"
  --after <datetime>   entries newer than "YYYY-MM-DD HH:MM:SS"
  --keep <n>           keep the newest n matching entries per task
  --dry-run            count only, delete nothing
  --json               {"deleted": n}`)
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

// cmdCleanup actually cleans something.
//
// It used to only send SIGHUP, which meant "cleanup" killed every running task
// (SIGHUP called pool.KillAll) and did no cleaning at all.
func cmdCleanup(_ []string) int {
	s := defaultStore()

	tasks, err := s.ListTasks()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading tasks: %v\n", err)
		return 1
	}
	if err := s.CleanupLogs(tasks); err != nil {
		fmt.Fprintf(os.Stderr, "Log cleanup failed: %v\n", err)
		return 1
	}
	removed, err := s.CleanupTempFiles()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Temp file cleanup failed: %v\n", err)
		return 1
	}
	if err := s.FlushState(); err != nil {
		fmt.Fprintf(os.Stderr, "State flush failed: %v\n", err)
		return 1
	}
	fmt.Printf("Cleanup done: retention applied to %d tasks, %d stale temp files removed\n", len(tasks), removed)

	// Ask the daemon to reload config; this no longer interrupts running tasks.
	if pid := daemonPID(); pid > 0 {
		syscall.Kill(pid, syscall.SIGHUP)
	}
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
		case "taskPageSize":
			v, err := strconv.Atoi(value)
			if err != nil || v < 1 || v > 1000 {
				fmt.Fprintf(os.Stderr, "Invalid taskPageSize: %s (must be 1-1000)\n", value)
				return 1
			}
			settings.TaskPageSize = v
		case "maxConcurrentTasks":
			// The pool size used to be hard-coded to 50 with no way to change it.
			v, err := strconv.Atoi(value)
			if err != nil || v < 1 || v > 1000 {
				fmt.Fprintf(os.Stderr, "Invalid maxConcurrentTasks: %s (must be 1-1000)\n", value)
				return 1
			}
			settings.MaxConcurrentTasks = v
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
