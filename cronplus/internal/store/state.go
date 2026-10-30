package store

import (
	"log"
	"os"
	"time"
)

// DaemonState persists scheduler runtime state across restarts.
type DaemonState struct {
	StartedAt   string        `json:"started_at"`
	TaskLastRun map[int]int64 `json:"task_last_run"` // task_id → unix timestamp
}

// LoadState reads the daemon state file.
func (s *Store) LoadState() (DaemonState, error) {
	var state DaemonState
	_, err := ReadJSON(s.StatePath, &state)
	if state.TaskLastRun == nil {
		state.TaskLastRun = map[int]int64{}
	}
	return state, err
}

// SaveState writes the daemon state file and refreshes the in-memory copy.
func (s *Store) SaveState(state DaemonState) error {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()

	if state.TaskLastRun == nil {
		state.TaskLastRun = map[int]int64{}
	}
	s.state = &state
	s.stateDirty = false
	return writeJSON(s.StatePath, state)
}

// SetTaskLastRun records a task's last-run timestamp.
//
// The timestamp is kept in memory and flushed by FlushState. Persisting on every
// single run meant reading and rewriting state.json (plus fsync) for each
// scheduled execution, and concurrent runs raced on the read-modify-write.
func (s *Store) SetTaskLastRun(taskID int, ts int64) error {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()

	if s.state == nil {
		var state DaemonState
		if _, err := ReadJSON(s.StatePath, &state); err != nil {
			log.Printf("Store: failed to read state for SetTaskLastRun: %v", err)
		}
		if state.TaskLastRun == nil {
			state.TaskLastRun = map[int]int64{}
		}
		s.state = &state
	}
	if s.state.TaskLastRun == nil {
		s.state.TaskLastRun = map[int]int64{}
	}
	s.state.TaskLastRun[taskID] = ts
	s.stateDirty = true
	return nil
}

// FlushState writes pending state changes to disk. It is a no-op when nothing
// changed, so calling it on a timer is cheap.
func (s *Store) FlushState() error {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()

	if s.state == nil || !s.stateDirty {
		return nil
	}
	if err := writeJSON(s.StatePath, *s.state); err != nil {
		return err
	}
	s.stateDirty = false
	return nil
}

// RebootDetected checks if the system rebooted by testing /run/cronplus.
// Returns true once after a reboot, then creates the marker file.
func RebootDetected() bool {
	const marker = "/run/cronplus"
	_, err := os.Stat(marker)
	if err == nil {
		return false // marker exists, not a reboot
	}
	// Create marker
	os.WriteFile(marker, []byte(time.Now().Format(time.RFC3339)), 0600)
	return true
}
