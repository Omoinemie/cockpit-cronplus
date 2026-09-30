package model

// Settings holds global daemon settings.
type Settings struct {
	Language             string `json:"language"`
	Theme                string `json:"theme"`
	AutoRefreshInterval  int    `json:"autoRefreshInterval"`
	LogMaxBytes          int    `json:"logMaxBytes"`
	LogBackupCount       int    `json:"logBackupCount"`
	DefaultRunUser       string `json:"defaultRunUser"`
	DefaultTimeout       int    `json:"defaultTimeout"`
	DefaultMaxRetries    int    `json:"defaultMaxRetries"`
	DefaultRetryInterval int    `json:"defaultRetryInterval"`
	LogPageSize          int    `json:"logPageSize"`
	TaskPageSize         int    `json:"taskPageSize"`
	DaemonLogLevel       string `json:"daemonLogLevel"`
	DaemonLogLines       int    `json:"daemonLogLines"`
	DaemonLogInterval    int    `json:"daemonLogInterval"`
	DaemonLogMaxBytes    int    `json:"daemonLogMaxBytes"`
	DaemonLogBackupCount int    `json:"daemonLogBackupCount"`
}

// DefaultSettings returns settings with default values.
func DefaultSettings() Settings {
	return Settings{
		Language:             "zh-CN",
		Theme:                "auto",
		AutoRefreshInterval:  15,
		LogMaxBytes:          10 * 1024 * 1024,
		LogBackupCount:       5,
		DefaultRunUser:       "root",
		DefaultTimeout:       0,
		DefaultMaxRetries:    0,
		DefaultRetryInterval: 60,
		LogPageSize:          20,
		TaskPageSize:         20,
		DaemonLogLevel:       "all",
		DaemonLogLines:       100,
		DaemonLogInterval:    2,
		DaemonLogMaxBytes:    10 * 1024 * 1024,
		DaemonLogBackupCount: 3,
	}
}
