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
// Uses field-level skipping for O(1) performance on most schedules.
func NextRunTime(fields [6]string, after time.Time) time.Time {
	current := after.Truncate(time.Second).Add(time.Second)
	maxCheck := current.AddDate(2, 0, 0) // 2 years lookahead

	// Pre-parse fields into structured matchers for efficient iteration
	secMatcher := parseFieldMatcher(fields[0], 0, 59)
	minMatcher := parseFieldMatcher(fields[1], 0, 59)
	hourMatcher := parseFieldMatcher(fields[2], 0, 23)
	dayMatcher := parseFieldMatcher(fields[3], 1, 31)
	monthMatcher := parseFieldMatcher(fields[4], 1, 12)
	dowMatcher := parseFieldMatcher(fields[5], 0, 7)

	// Limit iterations as safety net (should rarely hit this)
	maxIterations := 366 * 24 * 60 // ~1 year of minutes
	iterations := 0

	for current.Before(maxCheck) && iterations < maxIterations {
		iterations++

		// Quick reject: month
		if !monthMatcher.Matches(int(current.Month())) {
			// Jump to next valid month
			nextMonth := monthMatcher.NextAfter(int(current.Month()))
			if nextMonth <= int(current.Month()) {
				// Wrap to next year
				current = time.Date(current.Year()+1, time.Month(monthMatcher.MinValue()), 1, 0, 0, 0, 0, current.Location())
			} else {
				current = time.Date(current.Year(), time.Month(nextMonth), 1, 0, 0, 0, 0, current.Location())
			}
			continue
		}

		// Check day + dow (OR logic when both specified)
		dayMatch := dayMatcher.Matches(current.Day())
		dowVal := int(current.Weekday())
		if dowVal == 0 {
			dowVal = 7
		}
		dowMatch := dowMatcher.Matches(dowVal)

		day := fields[3]
		dow := fields[5]
		if day != "*" && dow != "*" {
			if !dayMatch && !dowMatch {
				current = current.AddDate(0, 0, 1)
				current = time.Date(current.Year(), current.Month(), current.Day(), 0, 0, 0, 0, current.Location())
				continue
			}
		} else {
			if day != "*" && !dayMatch {
				current = current.AddDate(0, 0, 1)
				current = time.Date(current.Year(), current.Month(), current.Day(), 0, 0, 0, 0, current.Location())
				continue
			}
			if dow != "*" && !dowMatch {
				current = current.AddDate(0, 0, 1)
				current = time.Date(current.Year(), current.Month(), current.Day(), 0, 0, 0, 0, current.Location())
				continue
			}
		}

		// Check hour
		if !hourMatcher.Matches(current.Hour()) {
			nextHour := hourMatcher.NextAfter(current.Hour())
			if nextHour <= current.Hour() {
				// Wrap to next day
				current = current.AddDate(0, 0, 1)
				current = time.Date(current.Year(), current.Month(), current.Day(), hourMatcher.MinValue(), 0, 0, 0, current.Location())
			} else {
				current = time.Date(current.Year(), current.Month(), current.Day(), nextHour, 0, 0, 0, current.Location())
			}
			continue
		}

		// Check minute
		if !minMatcher.Matches(current.Minute()) {
			nextMin := minMatcher.NextAfter(current.Minute())
			if nextMin <= current.Minute() {
				// Wrap to next hour
				current = current.Add(time.Duration(60-current.Minute()) * time.Minute)
				current = time.Date(current.Year(), current.Month(), current.Day(), current.Hour(), 0, 0, 0, current.Location())
			} else {
				current = time.Date(current.Year(), current.Month(), current.Day(), current.Hour(), nextMin, 0, 0, current.Location())
			}
			continue
		}

		// Check second
		if !secMatcher.Matches(current.Second()) {
			nextSec := secMatcher.NextAfter(current.Second())
			if nextSec <= current.Second() {
				// Wrap to next minute
				current = current.Add(time.Duration(60-current.Second()) * time.Second)
				current = time.Date(current.Year(), current.Month(), current.Day(), current.Hour(), current.Minute(), 0, 0, current.Location())
			} else {
				current = time.Date(current.Year(), current.Month(), current.Day(), current.Hour(), current.Minute(), nextSec, 0, current.Location())
			}
			continue
		}

		// All fields match!
		return current
	}

	return time.Time{} // no match found
}

// fieldMatcher provides efficient matching and next-value lookup for a cron field.
type fieldMatcher struct {
	isWildcard bool
	minVal     int
	maxVal     int
	values     []int // sorted list of valid values
}

func parseFieldMatcher(spec string, minVal, maxVal int) fieldMatcher {
	fm := fieldMatcher{minVal: minVal, maxVal: maxVal}

	if spec == "*" {
		fm.isWildcard = true
		return fm
	}

	valSet := make(map[int]bool)
	parts := strings.Split(spec, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		expandFieldPart(part, minVal, maxVal, valSet)
	}

	fm.values = sortedKeys(valSet)
	if len(fm.values) == 0 {
		fm.isWildcard = true
	}
	return fm
}

func expandFieldPart(part string, minVal, maxVal int, valSet map[int]bool) {
	// Step: */N or range/N
	if m := stepRe.FindStringSubmatch(part); m != nil {
		step, _ := strconv.Atoi(m[2])
		if step < 1 {
			return
		}
		base := m[1]
		if base == "*" {
			for v := minVal; v <= maxVal; v += step {
				valSet[v] = true
			}
			return
		}
		if strings.Contains(base, "-") {
			parts := strings.SplitN(base, "-", 2)
			lo, _ := strconv.Atoi(parts[0])
			hi, _ := strconv.Atoi(parts[1])
			for v := lo; v <= hi; v += step {
				valSet[v] = true
			}
			return
		}
		v, err := strconv.Atoi(base)
		if err != nil {
			return
		}
		valSet[v] = true
		return
	}

	// Range: N-M
	if m := rangeRe.FindStringSubmatch(part); m != nil {
		lo, _ := strconv.Atoi(m[1])
		hi, _ := strconv.Atoi(m[2])
		for v := lo; v <= hi; v++ {
			valSet[v] = true
		}
		return
	}

	// Single number
	v, err := strconv.Atoi(part)
	if err != nil {
		return
	}
	valSet[v] = true
}

func (fm fieldMatcher) Matches(value int) bool {
	if fm.isWildcard {
		return true
	}
	// Binary search in sorted values
	lo, hi := 0, len(fm.values)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		if fm.values[mid] == value {
			return true
		}
		if fm.values[mid] < value {
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	return false
}

// NextAfter returns the smallest valid value > current, or wraps to MinValue.
func (fm fieldMatcher) NextAfter(current int) int {
	if fm.isWildcard {
		if current < fm.maxVal {
			return current + 1
		}
		return fm.minVal
	}
	for _, v := range fm.values {
		if v > current {
			return v
		}
	}
	return fm.values[0] // wrap
}

func (fm fieldMatcher) MinValue() int {
	if fm.isWildcard {
		return fm.minVal
	}
	if len(fm.values) > 0 {
		return fm.values[0]
	}
	return fm.minVal
}

func sortedKeys(m map[int]bool) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Simple insertion sort (small arrays)
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

var rangeRe = regexp.MustCompile(`^(\d+)-(\d+)$`)
var stepRe = regexp.MustCompile(`^(.*?)/(\d+)$`)

// matchField checks if value matches a single cron field spec (legacy, kept for compatibility).
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
