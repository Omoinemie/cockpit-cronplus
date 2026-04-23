package model

// Task represents a single cron task configuration.
type Task struct {
	ID            int               `json:"id"`
	Title         string            `json:"title"`
	Command       string            `json:"command"`
	Schedule      string            `json:"schedule"`
	Enabled       bool              `json:"enabled"`
	Comment       string            `json:"comment"`
	RunUser       string            `json:"run_user"`
	Cwd           string            `json:"cwd"`
	Timeout       int               `json:"timeout"`
	MaxRetries    int               `json:"max_retries"`
	RetryInterval int               `json:"retry_interval"`
	MaxConcurrent int               `json:"max_concurrent"`
	KillPrevious  bool              `json:"kill_previous"`
	EnvVars       map[string]string `json:"env_vars"`
	Tags          string            `json:"tags"`
	LogRetention  int               `json:"log_retention_days"`
	LogMaxEntries int               `json:"log_max_entries"`
	RebootDelay   int               `json:"reboot_delay"`
}

// Defaults fills unset fields with default values.
func (t *Task) Defaults() {
	if t.RunUser == "" {
		t.RunUser = "root"
	}
	if t.MaxConcurrent == 0 {
		t.MaxConcurrent = 1
	}
	if t.RetryInterval == 0 {
		t.RetryInterval = 60
	}
	if t.EnvVars == nil {
		t.EnvVars = map[string]string{}
	}
}
