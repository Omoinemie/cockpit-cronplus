package executor

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Limits applied to the output captured for a single run.
//
// A run's output is stored as a JSON string inside the task log, so it cannot
// grow without bound: one chatty task would bloat /opt/cronplus/logs and the
// WebUI would have to ship the whole blob to the browser just to render one
// entry.
//
// Truncation keeps both the HEAD and the TAIL of the output. The old code kept
// only the first 50KB, which cut away exactly the part users need: the error
// message / stack trace / final summary written at the end of a failing run.
// That made a truncated log useless for answering "did this run succeed?".
const (
	// MaxOutputBytes is the maximum number of bytes kept per run.
	MaxOutputBytes = 256 * 1024
	// HeadOutputBytes is how much of the beginning is kept.
	HeadOutputBytes = 64 * 1024
	// TailOutputBytes is how much of the end is kept.
	TailOutputBytes = MaxOutputBytes - HeadOutputBytes
)

// TruncateOutput shortens s to at most MaxOutputBytes bytes, keeping the first
// HeadOutputBytes and the last TailOutputBytes bytes and marking the gap with a
// line users can search for. The cut points are moved to UTF-8 rune boundaries
// so multi-byte characters (CJK output, emoji) are never split in half.
func TruncateOutput(s string) string {
	if len(s) <= MaxOutputBytes {
		return s
	}

	headEnd := HeadOutputBytes
	for headEnd > 0 && !utf8.RuneStart(s[headEnd]) {
		headEnd--
	}
	tailStart := len(s) - TailOutputBytes
	for tailStart < len(s) && !utf8.RuneStart(s[tailStart]) {
		tailStart++
	}
	if tailStart < headEnd {
		tailStart = headEnd
	}

	head := s[:headEnd]
	tail := s[tailStart:]
	omitted := len(s) - len(head) - len(tail)

	return head +
		fmt.Sprintf("\n[cronplus] ---- output truncated: %d bytes omitted (kept first %d and last %d of %d bytes) ----\n",
			omitted, len(head), len(tail), len(s)) +
		tail
}

// AppendResultSummary appends a machine-readable one-liner describing how the
// run ended. It is added after truncation, so the verdict on a run whose output
// was cut is always present at the end of the log entry.
func AppendResultSummary(output string, status string, exitCode int, durationMs int) string {
	if output != "" && !strings.HasSuffix(output, "\n") {
		output += "\n"
	}
	return output + fmt.Sprintf("[cronplus] result: %s (exit=%d, %s)\n",
		status, exitCode, FormatDuration(durationMs))
}

// MaxPlausibleDurationMs is the point past which a stored duration cannot be a
// duration any more. Malformed entries used to be written with a wall-clock
// timestamp (about 1.79e12) as their duration, which showed up in the UI as
// "1793262922261ms" — roughly 56 years for a run that failed instantly.
const MaxPlausibleDurationMs = 1000 * 60 * 60 * 24 * 365 * 3

// FormatDuration renders a duration in milliseconds the way a person reads it:
// 850ms, 4.2s, 1m 07s, 2h 05m, 3d 04h. Values that cannot be a real duration
// (negative, or a timestamp that slipped in as a duration) return "unknown"
// instead of a nonsensical number.
func FormatDuration(ms int) string {
	if ms < 0 || ms > MaxPlausibleDurationMs {
		return "unknown"
	}
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	sec := float64(ms) / 1000
	if sec < 60 {
		if sec < 10 {
			return fmt.Sprintf("%.1fs", sec)
		}
		return fmt.Sprintf("%.0fs", sec)
	}
	days := ms / (1000 * 60 * 60 * 24)
	hours := (ms / (1000 * 60 * 60)) % 24
	minutes := (ms / (1000 * 60)) % 60
	seconds := (ms / 1000) % 60
	if days > 0 {
		return fmt.Sprintf("%dd %02dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %02dm", hours, minutes)
	}
	return fmt.Sprintf("%dm %02ds", minutes, seconds)
}
