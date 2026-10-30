package scheduler

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"cronplus/pkg/model"
)

// Special schedules mapped to 6-field cron equivalents.
var specials = map[string][6]string{
	"@reboot":   {},
	"@yearly":   {"0", "0", "0", "1", "1", "*"},
	"@annually": {"0", "0", "0", "1", "1", "*"},
	"@monthly":  {"0", "0", "0", "1", "*", "*"},
	"@weekly":   {"0", "0", "0", "*", "*", "0"},
	"@daily":    {"0", "0", "0", "*", "*", "*"},
	"@midnight": {"0", "0", "0", "*", "*", "*"},
	"@hourly":   {"0", "0", "*", "*", "*", "*"},
}

// ParseSchedule parses a 5 or 6 field cron expression into
// [second, minute, hour, day, month, dow]. Fields are validated strictly, so
// an unparsable spec returns an error instead of silently degrading to "*".
func ParseSchedule(schedule string) ([6]string, error) {
	schedule = strings.TrimSpace(schedule)
	if schedule == "" {
		return [6]string{}, fmt.Errorf("empty schedule")
	}
	if strings.HasPrefix(schedule, "@") {
		if schedule == "@reboot" {
			return [6]string{}, fmt.Errorf("@reboot has no cron fields")
		}
		cron, ok := specials[schedule]
		if !ok {
			return [6]string{}, fmt.Errorf("unknown special schedule: %s", schedule)
		}
		return cron, nil
	}

	parts := strings.Fields(schedule)
	var fields [6]string
	switch len(parts) {
	case 6:
		fields = [6]string{parts[0], parts[1], parts[2], parts[3], parts[4], parts[5]}
	case 5:
		fields = [6]string{"0", parts[0], parts[1], parts[2], parts[3], parts[4]}
	default:
		return [6]string{}, fmt.Errorf("cron expression must have 5 or 6 fields, got %d", len(parts))
	}

	if err := model.ValidateSchedule(strings.Join(fields[:], " ")); err != nil {
		return [6]string{}, err
	}
	return fields, nil
}

// Compiled is a cron expression parsed once and reused on every scheduling
// loop iteration (the loop runs twice a second, so re-parsing every task each
// time was pure waste).
type Compiled struct {
	Fields   [6]string
	matchers [6]fieldMatcher
	bad      bool
}

// Compile validates and pre-parses a schedule.
func Compile(schedule string) (*Compiled, error) {
	fields, err := ParseSchedule(schedule)
	if err != nil {
		return nil, err
	}
	c := &Compiled{Fields: fields}
	for i := range fields {
		_, minVal, maxVal := model.CronFieldRange(i)
		c.matchers[i] = parseFieldMatcher(fields[i], minVal, maxVal)
		if c.matchers[i].never {
			c.bad = true
		}
	}
	return c, nil
}

// Next returns the next matching time after `after`, or the zero time when the
// expression can never match.
func (c *Compiled) Next(after time.Time) time.Time {
	return nextRunTime(c.Fields, c.matchers, after)
}

// NextFromAnchor calculates the next run time using an anchor-based approach
// for step patterns, and verifies the result really matches every field.
func (c *Compiled) NextFromAnchor(after time.Time, baseTimeStr string) time.Time {
	if baseTimeStr == "" || c.bad {
		return c.Next(after)
	}
	interval, hasStep := stepInterval(c.Fields)
	if !hasStep {
		return c.Next(after)
	}
	anchor, err := time.ParseInLocation("2006-01-02 15:04:05", baseTimeStr, after.Location())
	if err != nil {
		return c.Next(after)
	}

	candidate := anchor
	if !anchor.After(after) {
		elapsed := after.Sub(anchor)
		periods := elapsed / interval
		candidate = anchor.Add(periods * interval)
		if !candidate.After(after) {
			candidate = anchor.Add((periods + 1) * interval)
		}
	}
	// The anchor maths only knows about the step field. A schedule such as
	// `0 */5 9-17 * * *` must not fire at 18:05, so the candidate is checked
	// against the other fields and falls back to the full search when it fails.
	if c.matches(candidate) {
		return candidate
	}
	return c.Next(after)
}

// matches reports whether t satisfies every field of the expression.
func (c *Compiled) matches(t time.Time) bool {
	if !c.matchers[0].Matches(t.Second()) ||
		!c.matchers[1].Matches(t.Minute()) ||
		!c.matchers[2].Matches(t.Hour()) ||
		!c.matchers[3].Matches(t.Day()) ||
		!c.matchers[4].Matches(int(t.Month())) {
		return false
	}
	if c.matchers[5].Matches(int(t.Weekday())) {
		return true
	}
	// Both 0 and 7 mean Sunday.
	return t.Weekday() == time.Sunday && c.matchers[5].Matches(7)
}

// NextRunTime finds the next time after 'after' that matches the cron fields.
// Uses field-level skipping for O(1) performance on most schedules.
func NextRunTime(fields [6]string, after time.Time) time.Time {
	c, err := Compile(strings.Join(fields[:], " "))
	if err != nil {
		return time.Time{}
	}
	return c.Next(after)
}

func nextRunTime(fields [6]string, matchers [6]fieldMatcher, after time.Time) time.Time {
	if matchers[0].never || matchers[1].never || matchers[2].never ||
		matchers[3].never || matchers[4].never || matchers[5].never {
		return time.Time{}
	}

	secMatcher := matchers[0]
	minMatcher := matchers[1]
	hourMatcher := matchers[2]
	dayMatcher := matchers[3]
	monthMatcher := matchers[4]
	dowMatcher := matchers[5]

	current := after.Truncate(time.Second).Add(time.Second)
	maxCheck := current.AddDate(2, 0, 0) // 2 years lookahead

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
		dowVal := int(current.Weekday()) // 0=Sunday, 1=Monday, ..., 6=Saturday
		dowMatch := dowMatcher.Matches(dowVal)
		if !dowMatch && dowVal == 0 {
			// Also check 7 for Sunday (cron convention: both 0 and 7 = Sunday)
			dowMatch = dowMatcher.Matches(7)
		}

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
	never      bool // spec was unparsable: match nothing rather than everything
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
	// An empty value set must never be treated as "*": that turned a typo like
	// `*/0` into "every second".
	if len(fm.values) == 0 {
		fm.never = true
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
		// "a/b" means "from a, every b" (Vixie cron), not "only a".
		// Treating it as a single value made `5/10 * * * * *` fire once every
		// hour at second 5 instead of every 10 seconds.
		for x := v; x <= maxVal; x += step {
			valSet[x] = true
		}
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
	if fm.never {
		return false
	}
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

// stepInterval extracts the step value and unit from a cron field spec.
// Returns (interval_seconds, true) if it's a step pattern, (0, false) otherwise.
func stepInterval(fields [6]string) (time.Duration, bool) {
	for i, spec := range fields {
		if m := stepRe.FindStringSubmatch(spec); m != nil {
			step, _ := strconv.Atoi(m[2])
			if step < 1 {
				continue
			}
			base := m[1]
			if base != "*" && !strings.Contains(base, "-") {
				continue
			}
			switch i {
			case 0:
				return time.Duration(step) * time.Second, true
			case 1:
				return time.Duration(step) * time.Minute, true
			case 2:
				return time.Duration(step) * time.Hour, true
			case 3:
				return time.Duration(step) * 24 * time.Hour, true
			case 4:
				return time.Duration(step) * 30 * 24 * time.Hour, true
			}
		}
	}
	return 0, false
}

// NextRunTimeFromAnchor calculates the next run time using an anchor-based
// approach for step patterns. When a task has a CronBaseTime set and its
// schedule contains a step pattern (*/N), the next run is computed as:
//
//	anchor + ceil((now - anchor) / interval) * interval
//
// This ensures consistent intervals regardless of month/day boundaries.
// The candidate is checked against the remaining fields; when it does not match
// (e.g. `0 */5 9-17 * * *` at 18:05) the full search is used instead.
func NextRunTimeFromAnchor(fields [6]string, after time.Time, baseTimeStr string) time.Time {
	c, err := Compile(strings.Join(fields[:], " "))
	if err != nil {
		return time.Time{}
	}
	return c.NextFromAnchor(after, baseTimeStr)
}
