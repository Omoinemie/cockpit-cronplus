package model

import (
	"fmt"
	"regexp"
	"strings"
)

// runUserRe only allows characters valid in Linux usernames.
var runUserRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]{0,31}$`)

// ValidateRunUser checks if a username is safe for use with `su - <user> -c`.
func ValidateRunUser(user string) error {
	user = strings.TrimSpace(user)
	if user == "" || user == "root" {
		return nil
	}
	if !runUserRe.MatchString(user) {
		return fmt.Errorf("invalid run_user %q: must match ^[a-zA-Z_][a-zA-Z0-9_-]{0,31}$", user)
	}
	if strings.ContainsAny(user, " \t\n\r;'\"`|&$(){}[]\\!") {
		return fmt.Errorf("invalid run_user %q: contains dangerous characters", user)
	}
	return nil
}

// ValidateSchedule checks if a cron schedule expression is structurally valid.
func ValidateSchedule(schedule string) error {
	schedule = strings.TrimSpace(schedule)
	if schedule == "" {
		return fmt.Errorf("schedule cannot be empty")
	}
	if schedule == "@reboot" {
		return nil
	}
	specials := []string{"@yearly", "@annually", "@monthly", "@weekly", "@daily", "@midnight", "@hourly"}
	for _, s := range specials {
		if schedule == s {
			return nil
		}
	}
	parts := strings.Fields(schedule)
	if len(parts) != 5 && len(parts) != 6 {
		return fmt.Errorf("cron expression must have 5 or 6 fields, got %d", len(parts))
	}
	return nil
}

// ValidateTask performs full validation on a task before save.
func ValidateTask(t *Task) error {
	if t.Command == "" {
		return fmt.Errorf("command cannot be empty")
	}
	if err := ValidateRunUser(t.RunUser); err != nil {
		return err
	}
	if t.Schedule != "" {
		if err := ValidateSchedule(t.Schedule); err != nil {
			return err
		}
	}
	if t.Timeout < 0 {
		return fmt.Errorf("timeout cannot be negative")
	}
	if t.MaxRetries < 0 || t.MaxRetries > 100 {
		return fmt.Errorf("max_retries must be 0-100")
	}
	if t.RetryInterval < 0 {
		return fmt.Errorf("retry_interval cannot be negative")
	}
	return nil
}
