package scheduler

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Special schedules mapped to 6-field cron equivalents.
var specials = map[string][6]string{
	"@reboot": {},
}

// ParseSchedule parses a 5 or 6 field cron expression into [second, minute, hour, day, month, dow].
func ParseSchedule(schedule string) ([6]string, error) {
	if strings.HasPrefix(schedule, "@") {
		cron, ok := specials[schedule]
		if !ok {
			return [6]string{}, fmt.Errorf("unknown special schedule: %s", schedule)
		}
		if schedule == "@reboot" {
			return [6]string{}, fmt.Errorf("@reboot has no cron fields")
		}
		return cron, nil
	}

	parts := strings.Fields(schedule)
	switch len(parts) {
	case 6:
		return [6]string{parts[0], parts[1], parts[2], parts[3], parts[4], parts[5]}, nil
	case 5:
		return [6]string{"0", parts[0], parts[1], parts[2], parts[3], parts[4]}, nil
	default:
		return [6]string{}, fmt.Errorf("cron expression must have 5 or 6 fields, got %d", len(parts))
	}
}

// NextRunTime finds the next time after 'after' that matches the cron fields.
func NextRunTime(fields [6]string, after time.Time) time.Time {
	current := after.Truncate(time.Second).Add(time.Second)
	maxCheck := current.AddDate(2, 0, 0) // 2 years lookahead

	for current.Before(maxCheck) {
		if matchCron(current, fields) {
			return current
		}
		current = current.Add(time.Second)
	}
	return time.Time{} // no match found
}

// matchCron checks if dt matches all 6 cron fields.
func matchCron(dt time.Time, fields [6]string) bool {
	if !matchField(dt.Second(), fields[0], 0, 59) {
		return false
	}
	if !matchField(dt.Minute(), fields[1], 0, 59) {
		return false
	}
	if !matchField(dt.Hour(), fields[2], 0, 23) {
		return false
	}
	if !matchField(int(dt.Month()), fields[4], 1, 12) {
		return false
	}

	dayMatch := matchField(dt.Day(), fields[3], 1, 31)
	dowVal := int(dt.Weekday())
	if dowVal == 0 {
		dowVal = 7 // convert Sunday=0 to 7 for cron compatibility
	}
	dowMatch := matchField(dowVal, fields[5], 0, 7)

	day := fields[3]
	dow := fields[5]
	if day != "*" && dow != "*" {
		return dayMatch || dowMatch
	}
	if day != "*" && !dayMatch {
		return false
	}
	if dow != "*" && !dowMatch {
		return false
	}
	return true
}

var rangeRe = regexp.MustCompile(`^(\d+)-(\d+)$`)
var stepRe = regexp.MustCompile(`^(.*?)/(\d+)$`)

// matchField checks if value matches a single cron field spec.
func matchField(value int, spec string, minVal, maxVal int) bool {
	if spec == "*" {
		return true
	}
	// Comma-separated parts
	if strings.Contains(spec, ",") {
		for _, s := range strings.Split(spec, ",") {
			if matchField(value, strings.TrimSpace(s), minVal, maxVal) {
				return true
			}
		}
		return false
	}
	// Step: */N or range/N
	if m := stepRe.FindStringSubmatch(spec); m != nil {
		step, _ := strconv.Atoi(m[2])
		if step < 1 {
			return false
		}
		base := m[1]
		if base == "*" {
			return (value-minVal)%step == 0
		}
		if strings.Contains(base, "-") {
			parts := strings.SplitN(base, "-", 2)
			lo, _ := strconv.Atoi(parts[0])
			hi, _ := strconv.Atoi(parts[1])
			return value >= lo && value <= hi && (value-lo)%step == 0
		}
		v, err := strconv.Atoi(base)
		if err != nil {
			return false
		}
		return value == v
	}
	// Range: N-M
	if m := rangeRe.FindStringSubmatch(spec); m != nil {
		lo, _ := strconv.Atoi(m[1])
		hi, _ := strconv.Atoi(m[2])
		return value >= lo && value <= hi
	}
	// Single number
	v, err := strconv.Atoi(spec)
	if err != nil {
		return false
	}
	return value == v
}
