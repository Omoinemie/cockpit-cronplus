package model

// LogEntry represents one execution record for a task.
type LogEntry struct {
	RunID     string `json:"run_id"`
	TaskID    int    `json:"task_id"`
	Title     string `json:"title"`
	Command   string `json:"command"`
	Status    string `json:"status"`     // "success" | "error"
	Output    string `json:"output"`     // truncated to 50KB
	Duration  int    `json:"duration"`   // milliseconds
	ExitCode  int    `json:"exit_code"`
	Attempt   int    `json:"attempt"`
	Trigger   string `json:"trigger"`    // "auto" | "manual"
	RunUser   string `json:"run_user"`
	CreatedAt string `json:"created_at"`
}
