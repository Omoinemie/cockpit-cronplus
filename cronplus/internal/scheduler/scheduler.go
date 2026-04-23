package scheduler

import (
	"context"
	"log"
	"os"
	"sync/atomic"
	"time"

	"cronplus/internal/store"
	"cronplus/pkg/model"
)

// Pool is the interface the executor pool must satisfy.
type Pool interface {
	Dispatch(task model.Task, trigger string)
	Kill(taskID int)
	KillAll()
	IsRunning(taskID int) bool
}

// Scheduler manages the main scheduling loop.
type Scheduler struct {
	store         *store.Store
	pool          Pool
	running       atomic.Bool
	taskLastRun   map[int]time.Time
	lastMtime     int64
	cachedTasks   []model.Task
	rebootFired   bool
	stats         Stats
	lastNextTaskID int    // track last logged "Next:" task to avoid spam
	lastNextTime  string // track last logged "Next:" time
}

// Stats holds scheduler statistics.
type Stats struct {
	TasksExecuted int    `json:"tasks_executed"`
	TasksFailed   int    `json:"tasks_failed"`
	LastRun       string `json:"last_run"`
	StartedAt     string `json:"started_at"`
}

func New(s *store.Store, pool Pool) *Scheduler {
	return &Scheduler{
		store:       s,
		pool:        pool,
		taskLastRun: make(map[int]time.Time),
		rebootFired: !store.RebootDetected(), // if marker exists, reboot already fired
	}
}

// Start begins the scheduling loop in a background goroutine.
func (s *Scheduler) Start(ctx context.Context) {
	if !s.running.CompareAndSwap(false, true) {
		return
	}
	s.stats.StartedAt = time.Now().Format(time.RFC3339)
	go s.loop(ctx)
	log.Println("Scheduler started")
}

// Stop signals the scheduler to stop.
func (s *Scheduler) Stop() {
	s.running.Store(false)
	log.Println("Scheduler stopped")
}

// Wakeup forces a config reload and reschedule.
func (s *Scheduler) Wakeup() {
	s.lastMtime = 0
	s.cachedTasks = nil
	s.taskLastRun = make(map[int]time.Time)
	s.lastNextTaskID = 0
	s.lastNextTime = ""
	log.Println("Scheduler: cache cleared, will re-read config")
}

// GetStats returns current scheduler statistics.
func (s *Scheduler) GetStats() Stats {
	return s.stats
}

func (s *Scheduler) loop(ctx context.Context) {
	var lastCleanup time.Time

	for s.running.Load() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Periodic stale cleanup (every 5 min)
		if time.Since(lastCleanup) > 5*time.Minute {
			lastCleanup = time.Now()
		}

		tasks, err := s.loadTasks()
		if err != nil {
			log.Printf("Scheduler: load tasks error: %v", err)
			driftSleep(ctx, 5*time.Second)
			continue
		}
		if len(tasks) == 0 {
			driftSleep(ctx, 1*time.Second)
			continue
		}

		now := time.Now().Truncate(time.Second)
		type candidate struct {
			at   time.Time
			task model.Task
			cron [6]string
		}
		var candidates []candidate

		for _, task := range tasks {
			if !task.Enabled {
				continue
			}

			// Handle @reboot
			if task.Schedule == "@reboot" {
				if !s.rebootFired {
					s.rebootFired = true
					if task.RebootDelay > 0 {
						log.Printf("Task [%d] @reboot: scheduled in %ds", task.ID, task.RebootDelay)
						go func(t model.Task) {
							time.Sleep(time.Duration(t.RebootDelay) * time.Second)
							s.pool.Dispatch(t, "auto")
							s.stats.TasksExecuted++
							s.stats.LastRun = time.Now().Format(time.RFC3339)
						}(task)
					} else {
						log.Printf("Task [%d] @reboot: firing now", task.ID)
						s.pool.Dispatch(task, "auto")
						s.stats.TasksExecuted++
						s.stats.LastRun = time.Now().Format(time.RFC3339)
					}
				}
				continue
			}

			if s.pool.IsRunning(task.ID) {
				if task.KillPrevious {
					s.pool.Kill(task.ID)
				} else {
					continue
				}
			}

			fields, err := ParseSchedule(task.Schedule)
			if err != nil {
				log.Printf("Task [%d] bad schedule %q: %v", task.ID, task.Schedule, err)
				continue
			}

			lastRun := s.taskLastRun[task.ID]
			if lastRun.IsZero() {
				lastRun = now.Add(-time.Second)
			}
			nxt := NextRunTime(fields, lastRun)
			if !nxt.IsZero() {
				candidates = append(candidates, candidate{at: nxt, task: task, cron: fields})
			}
		}

		if len(candidates) == 0 {
			driftSleep(ctx, 1*time.Second)
			continue
		}

		// Sort by trigger time
		for i := 0; i < len(candidates)-1; i++ {
			for j := i + 1; j < len(candidates); j++ {
				if candidates[j].at.Before(candidates[i].at) {
					candidates[i], candidates[j] = candidates[j], candidates[i]
				}
			}
		}

		// Execute all candidates whose time <= now
		executed := false
		for _, c := range candidates {
			if c.at.After(now) {
				break
			}
			taskID := c.task.ID
			s.taskLastRun[taskID] = c.at

			if s.pool.IsRunning(taskID) {
				if c.task.KillPrevious {
					s.pool.Kill(taskID)
				} else {
					continue
				}
			}

			log.Printf("Scheduler: triggering task [%d] %s", taskID,
				func() string {
					if c.task.Title != "" {
						return c.task.Title
					}
					if len(c.task.Command) > 60 {
						return c.task.Command[:60]
					}
					return c.task.Command
				}())

			s.pool.Dispatch(c.task, "auto")
			s.stats.TasksExecuted++
			s.stats.LastRun = time.Now().Format(time.RFC3339)
			s.lastNextTaskID = 0 // reset so next task gets logged
			s.lastNextTime = ""
			executed = true
		}

		// Calculate sleep until next candidate
		var nextWait time.Duration
		for _, c := range candidates {
			if c.at.After(now) {
				wait := time.Until(c.at)
				if wait > 0 {
					nextWait = wait
					break
				}
			}
		}

		if nextWait > 0 && !executed {
			// Log next task info — only when the task or time changes
			for _, c := range candidates {
				if c.at.After(now) {
					nextTimeStr := c.at.Format("2006-01-02 15:04:05")
					if c.task.ID != s.lastNextTaskID || nextTimeStr != s.lastNextTime {
						taskName := c.task.Title
						if taskName == "" {
							taskName = c.task.Command
							if len(taskName) > 40 {
								taskName = taskName[:40]
							}
						}
						log.Printf("Next: task [%d] '%s' at %s (in %.0fs)",
							c.task.ID, taskName,
							nextTimeStr,
							time.Until(c.at).Seconds())
						s.lastNextTaskID = c.task.ID
						s.lastNextTime = nextTimeStr
					}
					break
				}
			}
			driftSleep(ctx, min(nextWait, 1*time.Second))
		} else {
			driftSleep(ctx, 500*time.Millisecond)
		}
	}
}

func (s *Scheduler) loadTasks() ([]model.Task, error) {
	info, err := os.Stat(s.store.ConfPath)
	if err != nil {
		mtime := int64(0)
		if s.lastMtime != mtime || s.cachedTasks == nil {
			tasks, err := s.store.ListTasks()
			if err != nil {
				return nil, err
			}
			s.cachedTasks = tasks
			s.lastMtime = mtime
			if len(tasks) > 0 {
				log.Printf("Config loaded: %d tasks", len(tasks))
			}
		}
		return s.cachedTasks, nil
	}

	mtime := info.ModTime().UnixNano()
	if mtime != s.lastMtime || s.cachedTasks == nil {
		tasks, err := s.store.ListTasks()
		if err != nil {
			// Keep cached tasks on error
			if s.cachedTasks == nil {
				return nil, err
			}
			log.Printf("Config reload error (keeping cached): %v", err)
			return s.cachedTasks, nil
		}
		s.cachedTasks = tasks
		s.lastMtime = mtime
		log.Printf("Config reloaded: %d tasks", len(tasks))
	}
	return s.cachedTasks, nil
}

// driftSleep sleeps with drift compensation — aligns to the next second boundary.
func driftSleep(ctx context.Context, max time.Duration) {
	now := time.Now()
	wait := time.Second - time.Duration(now.Nanosecond())*time.Nanosecond
	if wait < 50*time.Millisecond {
		wait = time.Second
	}
	if wait > max {
		wait = max
	}
	select {
	case <-ctx.Done():
	case <-time.After(wait):
	}
}
