package model

// LogEntry represents one execution record for a task.
type LogEntry struct {
	RunID   string `json:"run_id"`
	TaskID  int    `json:"task_id"`
	Title   string `json:"title"`
	Command string `json:"command"`
	Status  string `json:"status"` // "success" | "error"
	// Head + tail of the run's combined stdout/stderr, bounded by
	// executor.MaxOutputBytes. Older versions kept only the first 50KB, which
	// dropped the half that says whether the run actually succeeded; the
	// executor now appends a result summary after truncation so the verdict is
	// always present.
	Output    string `json:"output"`
	Duration  int    `json:"duration"` // milliseconds
	ExitCode  int    `json:"exit_code"`
	Attempt   int    `json:"attempt"`
	Trigger   string `json:"trigger"` // "auto" | "manual"
	RunUser   string `json:"run_user"`
	CreatedAt string `json:"created_at"`
}
