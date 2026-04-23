package store

import (
	"os"
	"time"
)

// DaemonState persists scheduler runtime state across restarts.
type DaemonState struct {
	StartedAt  string         `json:"started_at"`
	TaskLastRun map[int]int64  `json:"task_last_run"` // task_id → unix timestamp
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

// SaveState writes the daemon state file.
func (s *Store) SaveState(state DaemonState) error {
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
