package model

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// runUserRe only allows characters valid in Linux usernames.
var runUserRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]{0,31}$`)

// cronRange describes the allowed range of one cron field.
type cronRange struct {
	name     string
	min, max int
}

// Six cron fields, in the order the scheduler uses them.
var cronFieldRanges = []cronRange{
	{"second", 0, 59},
	{"minute", 0, 59},
	{"hour", 0, 23},
	{"day", 1, 31},
	{"month", 1, 12},
	{"weekday", 0, 7},
}

// specialSchedules are the named schedules accepted in place of cron fields.
var specialSchedules = map[string]bool{
	"@reboot":   true,
	"@yearly":   true,
	"@annually": true,
	"@monthly":  true,
	"@weekly":   true,
	"@daily":    true,
	"@midnight": true,
	"@hourly":   true,
}

// ValidateRunUser checks if a username is safe for use with `su - <user> -c`.
func ValidateRunUser(user string) error {
	user = strings.TrimSpace(user)
	if user == "" || user == "root" {
		return nil
	}
	if !runUserRe.MatchString(user) {
		return fmt.Errorf("invalid run_user %q: must match ^[a-zA-Z_][a-zA-Z0-9_-]{0,31}$", user)
	}
	return nil
}

// ValidateSchedule checks a cron schedule strictly: field count, syntax and
// value ranges.
//
// The scheduler used to accept anything and silently turn unparsable fields
// into wildcards, so `*/0` ran every second and `0 0 99 * * *` could never
// fire while still burning a full scan on every loop. Rejecting bad input at
// save time (and at parse time in the scheduler) keeps both failure modes out.
func ValidateSchedule(schedule string) error {
	schedule = strings.TrimSpace(schedule)
	if schedule == "" {
		return fmt.Errorf("schedule cannot be empty")
	}
	if strings.HasPrefix(schedule, "@") {
		if !specialSchedules[schedule] {
			return fmt.Errorf("unknown special schedule %q", schedule)
		}
		return nil
	}

	parts := strings.Fields(schedule)
	if len(parts) != 5 && len(parts) != 6 {
		return fmt.Errorf("cron expression must have 5 or 6 fields, got %d", len(parts))
	}
	// A 5-field expression has no seconds field.
	offset := 0
	if len(parts) == 5 {
		offset = 1
	}
	for i, spec := range parts {
		rng := cronFieldRanges[i+offset]
		if err := ValidateCronField(spec, rng.name, rng.min, rng.max); err != nil {
			return err
		}
	}
	return nil
}

// CronFieldRange returns the name and allowed bounds of cron field index i
// (0 = second, 1 = minute, 2 = hour, 3 = day, 4 = month, 5 = weekday).
func CronFieldRange(i int) (name string, min, max int) {
	if i < 0 || i >= len(cronFieldRanges) {
		return "", 0, 0
	}
	r := cronFieldRanges[i]
	return r.name, r.min, r.max
}

// ValidateCronField validates one field against its range.
func ValidateCronField(spec, name string, min, max int) error {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return fmt.Errorf("%s field is empty", name)
	}
	if spec == "*" {
		return nil
	}
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return fmt.Errorf("%s field has an empty list item", name)
		}
		if err := validateCronFieldPart(part, name, min, max); err != nil {
			return err
		}
	}
	return nil
}

func validateCronFieldPart(part, name string, min, max int) error {
	base := part
	if idx := strings.Index(part, "/"); idx >= 0 {
		base = part[:idx]
		stepStr := part[idx+1:]
		parsed, err := strconv.Atoi(stepStr)
		if err != nil {
			return fmt.Errorf("%s field %q: step must be a number", name, part)
		}
		if parsed < 1 {
			return fmt.Errorf("%s field %q: step must be >= 1", name, part)
		}
		if parsed > max-min+1 {
			return fmt.Errorf("%s field %q: step %d is larger than the field range", name, part, parsed)
		}
	}

	if base == "*" {
		return nil
	}

	if strings.Contains(base, "-") {
		parts := strings.SplitN(base, "-", 2)
		lo, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil {
			return fmt.Errorf("%s field %q: range start must be a number", name, part)
		}
		hi, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			return fmt.Errorf("%s field %q: range end must be a number", name, part)
		}
		if lo < min || lo > max || hi < min || hi > max {
			return fmt.Errorf("%s field %q: values must be within %d-%d", name, part, min, max)
		}
		if lo > hi {
			return fmt.Errorf("%s field %q: range start is greater than its end", name, part)
		}
		return nil
	}

	v, err := strconv.Atoi(base)
	if err != nil {
		return fmt.Errorf("%s field %q: not a number, range or *", name, part)
	}
	if v < min || v > max {
		return fmt.Errorf("%s field %q: value must be within %d-%d", name, part, min, max)
	}
	return nil
}

// ValidateTask performs full validation on a task before save.
func ValidateTask(t *Task) error {
	if strings.TrimSpace(t.Command) == "" {
		return fmt.Errorf("command cannot be empty")
	}
	if err := ValidateRunUser(t.RunUser); err != nil {
		return err
	}
	if strings.TrimSpace(t.Schedule) == "" {
		return fmt.Errorf("schedule cannot be empty")
	}
	if err := ValidateSchedule(t.Schedule); err != nil {
		return err
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
	if t.MaxConcurrent < 0 {
		return fmt.Errorf("max_concurrent cannot be negative")
	}
	return nil
}
