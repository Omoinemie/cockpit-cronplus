package store

import (
	"os"
	"sync"
	"time"
)

// DaemonState persists scheduler runtime state across restarts.
type DaemonState struct {
	StartedAt  string         `json:"started_at"`
	TaskLastRun map[int]int64  `json:"task_last_run"` // task_id → unix timestamp
}

// stateMu protects concurrent access to DaemonState.
// The TaskLastRun map is not safe for concurrent access without this.
var stateMu sync.Mutex

// LoadState reads the daemon state file.
func (s *Store) LoadState() (DaemonState, error) {
	stateMu.Lock()
	defer stateMu.Unlock()

	var state DaemonState
	_, err := ReadJSON(s.StatePath, &state)
	if state.TaskLastRun == nil {
		state.TaskLastRun = map[int]int64{}
	}
	return state, err
}

// SaveState writes the daemon state file.
func (s *Store) SaveState(state DaemonState) error {
	stateMu.Lock()
	defer stateMu.Unlock()

	return writeJSON(s.StatePath, state)
}

// SetTaskLastRun updates a single task's last-run timestamp atomically.
func (s *Store) SetTaskLastRun(taskID int, ts int64) error {
	stateMu.Lock()
	defer stateMu.Unlock()

	var state DaemonState
	ReadJSON(s.StatePath, &state)
	if state.TaskLastRun == nil {
		state.TaskLastRun = map[int]int64{}
	}
	state.TaskLastRun[taskID] = ts
	return writeJSON(s.StatePath, state)
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
	os.WriteFile(marker, []byte(time.Now().Format(time.RFC3339)), 0644)
	return true
}
