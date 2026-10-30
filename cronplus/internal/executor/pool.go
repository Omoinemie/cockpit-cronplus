package executor

import (
	"context"
	"log"
	"os/exec"
	"sync"
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
	mu           sync.RWMutex
	running      map[int]*RunningTask
	store        *store.Store
	maxTotal     int
	taskRunCount map[int]int
}

func NewPool(s *store.Store, maxTotal int) *Pool {
	if maxTotal <= 0 {
		maxTotal = 50
	}
	return &Pool{
		running:      make(map[int]*RunningTask),
		store:        s,
		maxTotal:     maxTotal,
		taskRunCount: make(map[int]int),
	}
}

// Dispatch starts executing a task in a new goroutine with a new process.
func (p *Pool) Dispatch(task model.Task, trigger string) {
	p.mu.RLock()
	running := len(p.running)
	p.mu.RUnlock()
	if running >= p.maxTotal {
		log.Printf("Pool: max concurrency reached (%d/%d), skipping task [%d]", running, p.maxTotal, task.ID)
		return
	}
	go p.execute(task, trigger)
}

// IsRunning checks if a task is currently executing.
func (p *Pool) IsRunning(taskID int) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	_, ok := p.running[taskID]
	return ok
}

// Kill terminates a running task's process group and reports whether the task
// was running.
func (p *Pool) Kill(taskID int) bool {
	p.mu.Lock()
	rt, ok := p.running[taskID]
	if ok {
		delete(p.running, taskID)
	}
	p.mu.Unlock()
	if !ok {
		return false
	}
	if rt.Cancel != nil {
		rt.Cancel()
	}
	// The process group itself is terminated by runOnce's cancellation branch
	// (SIGTERM, then SIGKILL after a grace period). Sending signals here as well
	// and calling cmd.Wait() would race with runOnce's own Wait(): exec.Cmd is
	// not safe for concurrent use, and the duplicate Wait was silently stealing
	// the exit status.
	log.Printf("Pool: stopping task [%d] (run %s)", taskID, rt.RunID)
	return true
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
	result := make([]RunningInfo, 0, len(p.running))
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

	// Concurrency checks come first. The run sequence used to be incremented
	// before them, so a run that was skipped (already running / over the
	// concurrency limit) still consumed a slot of max_runs and could disable the
	// task without it ever executing.
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

	maxConcurrent := task.MaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}
	currentCount := p.taskRunCount[taskID]
	if currentCount >= maxConcurrent {
		p.mu.Unlock()
		log.Printf("Pool: task [%d] max_concurrent reached (%d/%d), skipping", taskID, currentCount, maxConcurrent)
		return
	}
	p.taskRunCount[taskID] = currentCount + 1
	p.mu.Unlock()

	// From here the run owns a concurrency slot.
	var runID string
	if trigger == "manual" {
		// Manual runs don't participate in sequential numbering
		runID = NewManualRunID(taskID, task.RunUser)
	} else {
		// Auto runs: increment sequence, generate #taskID-user-NNNN
		task.RunSeq++
		runID = NewSeqRunID(taskID, task.RunUser, task.RunSeq)
		if err := p.store.UpdateTaskRunSeq(taskID, task.RunSeq); err != nil {
			log.Printf("[%s] Pool: failed to update RunSeq for task [%d]: %v", runID, taskID, err)
		}
		if task.MaxRuns > 0 && task.RunSeq >= task.MaxRuns {
			log.Printf("[%s] Task [%d] reached max runs (%d), disabling", runID, taskID, task.MaxRuns)
			if err := p.store.ToggleTaskEnabled(taskID, false); err != nil {
				log.Printf("[%s] Pool: failed to disable task [%d]: %v", runID, taskID, err)
			}
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	rt := &RunningTask{
		TaskID:    taskID,
		RunID:     runID,
		StartTime: time.Now(),
		Cancel:    cancel,
		Trigger:   trigger,
	}

	p.mu.Lock()
	p.running[taskID] = rt
	p.mu.Unlock()

	defer func() {
		cancel()
		p.mu.Lock()
		if cur, ok := p.running[taskID]; ok && cur == rt {
			delete(p.running, taskID)
		}
		if p.taskRunCount[taskID] > 0 {
			p.taskRunCount[taskID]--
		}
		p.mu.Unlock()
	}()

	command := DecodeCommand(task.Command)
	maxRetries := task.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}

	if task.Timeout <= 0 {
		if settings, err := p.store.ReadSettings(); err == nil && settings.DefaultTimeout > 0 {
			task.Timeout = settings.DefaultTimeout
		}
	}

	taskName := task.Title
	if taskName == "" {
		taskName = command
		if len(taskName) > 50 {
			taskName = taskName[:50]
		}
	}
	log.Printf("[%s] Task [%d] %s — started [%s]", runID, taskID, taskName, trigger)

	// Expose the process to the pool as soon as it exists, so "ps" shows a real
	// PID and a stop request can act on the process group.
	onStart := func(cmd *exec.Cmd) {
		p.mu.Lock()
		rt.Cmd = cmd
		if cmd.Process != nil {
			rt.PID = cmd.Process.Pid
		}
		p.mu.Unlock()
	}

	for attempt := 1; attempt <= 1+maxRetries; attempt++ {
		status, outputStr, duration, exitCode := p.runOnce(ctx, task, command, attempt, trigger, onStart)

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
		log.Printf("[%s] Task [%d] %s — %s %s (%s, exit=%d, attempt=%d) [%s]",
			runID, taskID, taskName, icon, status, FormatDuration(duration), exitCode, attempt, trigger)

		if status == "success" {
			return
		}
		// A stopped or timed-out run must not be retried.
		if ctx.Err() != nil || exitCode == -1 {
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
func (p *Pool) runOnce(ctx context.Context, task model.Task, command string, attempt int, trigger string, onStart ...func(*exec.Cmd)) (status string, outputStr string, durationMs int, exitCode int) {
	var hook func(*exec.Cmd)
	if len(onStart) > 0 {
		hook = onStart[0]
	}
	return RunCommand(ctx, task, command, nil, nil, hook)
}
