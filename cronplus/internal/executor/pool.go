package executor

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"cronplus/internal/store"
	"cronplus/pkg/model"
)

// RunningTask tracks a currently executing task process.
type RunningTask struct {
	TaskID    int
	RunID     string
	PID       int
	Cmd       *exec.Cmd
	Cancel    context.CancelFunc
	StartTime time.Time
	Trigger   string
}

// Pool manages concurrent task execution with process isolation.
type Pool struct {
	mu       sync.RWMutex
	running  map[int]*RunningTask
	store    *store.Store
	maxTotal int
}

func NewPool(s *store.Store, maxTotal int) *Pool {
	if maxTotal <= 0 {
		maxTotal = 50
	}
	return &Pool{
		running:  make(map[int]*RunningTask),
		store:    s,
		maxTotal: maxTotal,
	}
}

// Dispatch starts executing a task in a new goroutine with a new process.
func (p *Pool) Dispatch(task model.Task, trigger string) {
	go p.execute(task, trigger)
}

// IsRunning checks if a task is currently executing.
func (p *Pool) IsRunning(taskID int) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	rt, ok := p.running[taskID]
	if !ok {
		return false
	}
	// Verify process is still alive
	if rt.Cmd != nil && rt.Cmd.Process != nil {
		return true
	}
	return false
}

// Kill terminates a running task's process group.
func (p *Pool) Kill(taskID int) {
	p.mu.Lock()
	rt, ok := p.running[taskID]
	if !ok {
		p.mu.Unlock()
		return
	}
	delete(p.running, taskID)
	p.mu.Unlock()

	if rt.Cancel != nil {
		rt.Cancel()
	}
	if rt.Cmd != nil && rt.Cmd.Process != nil {
		// Kill the entire process group
		pgid := rt.Cmd.Process.Pid
		log.Printf("Pool: killing task [%d] process group pgid=%d", taskID, pgid)
		syscall.Kill(-pgid, syscall.SIGTERM)
		// Wait briefly, then SIGKILL
		done := make(chan struct{})
		go func() {
			rt.Cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			syscall.Kill(-pgid, syscall.SIGKILL)
			<-done
		}
	}
}

// KillAll terminates all running tasks.
func (p *Pool) KillAll() {
	p.mu.Lock()
	ids := make([]int, 0, len(p.running))
	for id := range p.running {
		ids = append(ids, id)
	}
	p.mu.Unlock()

	for _, id := range ids {
		p.Kill(id)
	}
	log.Println("Pool: all running tasks killed")
}

// Running returns info about all currently running tasks.
func (p *Pool) Running() []RunningInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var result []RunningInfo
	for _, rt := range p.running {
		result = append(result, RunningInfo{
			TaskID:    rt.TaskID,
			RunID:     rt.RunID,
			PID:       rt.PID,
			StartTime: rt.StartTime.Format("2006-01-02 15:04:05"),
			Duration:  int(time.Since(rt.StartTime).Milliseconds()),
			Trigger:   rt.Trigger,
		})
	}
	return result
}

// RunningInfo is a snapshot of a running task.
type RunningInfo struct {
	TaskID    int    `json:"task_id"`
	RunID     string `json:"run_id"`
	PID       int    `json:"pid"`
	StartTime string `json:"started_at"`
	Duration  int    `json:"duration_ms"`
	Trigger   string `json:"trigger"`
}

// execute runs a single task as a subprocess. Called in a goroutine.
func (p *Pool) execute(task model.Task, trigger string) {
	taskID := task.ID
	runID := NewRunID()

	// Concurrency check
	p.mu.Lock()
	if _, exists := p.running[taskID]; exists {
		if !task.KillPrevious {
			p.mu.Unlock()
			log.Printf("Pool: task [%d] already running, skipping", taskID)
			return
		}
		p.mu.Unlock()
		p.Kill(taskID)
		p.mu.Lock()
	}

	ctx, cancel := context.WithCancel(context.Background())
	rt := &RunningTask{
		TaskID:    taskID,
		RunID:     runID,
		StartTime: time.Now(),
		Cancel:    cancel,
		Trigger:   trigger,
	}
	p.running[taskID] = rt
	p.mu.Unlock()

	defer func() {
		cancel()
		p.mu.Lock()
		if cur, ok := p.running[taskID]; ok && cur == rt {
			delete(p.running, taskID)
		}
		p.mu.Unlock()
	}()

	command := DecodeCommand(task.Command)
	maxRetries := task.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}

	taskName := task.Title
	if taskName == "" {
		taskName = command
		if len(taskName) > 50 {
			taskName = taskName[:50]
		}
	}
	log.Printf("[%s] Task [%d] %s — started [%s]", runID, taskID, taskName, trigger)

	for attempt := 1; attempt <= 1+maxRetries; attempt++ {
		status, outputStr, duration, exitCode := p.runOnce(ctx, task, command, attempt, trigger)

		// Write log
		entry := model.LogEntry{
			RunID:     runID,
			TaskID:    taskID,
			Title:     task.Title,
			Command:   command,
			Status:    status,
			Output:    outputStr,
			Duration:  duration,
			ExitCode:  exitCode,
			Attempt:   attempt,
			Trigger:   trigger,
			RunUser:   task.RunUser,
			CreatedAt: time.Now().Format("2006-01-02 15:04:05"),
		}
		if err := p.store.AppendLog(entry); err != nil {
			log.Printf("[%s] Pool: failed to write log for task [%d]: %v", runID, taskID, err)
		}

		icon := "✓"
		if status != "success" {
			icon = "✗"
		}
		log.Printf("[%s] Task [%d] %s — %s %s (%dms, exit=%d, attempt=%d) [%s]",
			runID, taskID, taskName, icon, status, duration, exitCode, attempt, trigger)

		if status == "success" {
			return
		}
		// Retry if attempts remain
		if attempt <= maxRetries {
			retryInterval := task.RetryInterval
			if retryInterval <= 0 {
				retryInterval = 60
			}
			log.Printf("[%s] Task [%d] retry %d/%d in %ds", runID, taskID, attempt, maxRetries, retryInterval)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(retryInterval) * time.Second):
			}
		}
	}
}

// runOnce executes the command once and returns result.
func (p *Pool) runOnce(ctx context.Context, task model.Task, command string, attempt int, trigger string) (status string, outputStr string, durationMs int, exitCode int) {
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

	// Capture output via pipe
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		status = "error"
		outputStr = fmt.Sprintf("[cronplus] pipe error: %v", err)
		return
	}

	startTime := time.Now()
	if err := cmd.Start(); err != nil {
		status = "error"
		outputStr = fmt.Sprintf("[cronplus] start error: %v", err)
		durationMs = int(time.Since(startTime).Milliseconds())
		return
	}

	pgid := cmd.Process.Pid

	// Read output with timeout
	type readResult struct {
		data []byte
		err  error
	}
	ch := make(chan readResult, 1)
	go func() {
		data, err := io.ReadAll(stdout)
		ch <- readResult{data, err}
	}()

	var output []byte
	timedOut := false

	if task.Timeout > 0 {
		timer := time.NewTimer(time.Duration(task.Timeout) * time.Second)
		select {
		case result := <-ch:
			output = result.data
			timer.Stop()
		case <-timer.C:
			timedOut = true
			// Kill entire process group
			syscall.Kill(-pgid, syscall.SIGTERM)
			time.Sleep(500 * time.Millisecond)
			syscall.Kill(-pgid, syscall.SIGKILL)
			// Still wait for pipe to close
			select {
			case result := <-ch:
				output = result.data
			case <-time.After(3 * time.Second):
			}
		case <-ctx.Done():
			timedOut = true
			syscall.Kill(-pgid, syscall.SIGTERM)
			time.Sleep(500 * time.Millisecond)
			syscall.Kill(-pgid, syscall.SIGKILL)
			select {
			case result := <-ch:
				output = result.data
			case <-time.After(3 * time.Second):
			}
		}
	} else {
		// No timeout, but respect context cancellation
		select {
		case result := <-ch:
			output = result.data
		case <-ctx.Done():
			syscall.Kill(-pgid, syscall.SIGTERM)
			time.Sleep(500 * time.Millisecond)
			syscall.Kill(-pgid, syscall.SIGKILL)
			select {
			case result := <-ch:
				output = result.data
			case <-time.After(3 * time.Second):
			}
		}
	}

	// Wait for process to exit
	cmd.Wait()
	durationMs = int(time.Since(startTime).Milliseconds())

	exitCode = -1
	status = "error"
	if cmd.ProcessState != nil && cmd.ProcessState.Success() {
		exitCode = 0
		status = "success"
	} else if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}

	outputStr = string(output)
	if len(outputStr) > 50000 {
		outputStr = outputStr[:50000] + "\n[cronplus] output truncated (>50KB)\n"
	}

	if timedOut {
		outputStr += fmt.Sprintf("\n[cronplus] command timeout (%ds) terminated\n", task.Timeout)
		status = "error"
		exitCode = -1
	}

	return
}

// buildEnv constructs the environment slice from task env vars.
func buildEnv(envVars map[string]string) []string {
	env := []string{}
	for k, v := range envVars {
		env = append(env, k+"="+v)
	}
	return env
}

// expandPath expands ~ to home directory.
func expandPath(path string) string {
	if len(path) > 0 && path[0] == '~' {
		if home := os.Getenv("HOME"); home != "" {
			return home + path[1:]
		}
	}
	return path
}
