package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cronplus/pkg/model"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	return New(filepath.Join(dir, "tasks.conf"), filepath.Join(dir, "logs"))
}

func writeTestSettings(t *testing.T, s *Store, settings model.Settings) {
	t.Helper()
	if err := s.WriteSettings(settings); err != nil {
		t.Fatalf("write settings: %v", err)
	}
}

func entry(taskID int, createdAt, output string) model.LogEntry {
	return model.LogEntry{
		RunID:     "run-" + createdAt,
		TaskID:    taskID,
		Title:     "task",
		Command:   "true",
		Status:    "success",
		Output:    output,
		CreatedAt: createdAt,
	}
}

func TestAppendAndReadLog(t *testing.T) {
	s := newTestStore(t)

	for i, ts := range []string{"2025-01-01 00:00:01", "2025-01-01 00:00:02"} {
		if err := s.AppendLog(entry(1, ts, strings.Repeat("x", 10))); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	logs, err := s.ListLogs(1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(logs))
	}
	if logs[0].CreatedAt != "2025-01-01 00:00:01" || logs[1].CreatedAt != "2025-01-01 00:00:02" {
		t.Fatalf("entries out of order: %s, %s", logs[0].CreatedAt, logs[1].CreatedAt)
	}
}

// Rotating on size must not lose history: older entries stay readable through
// the rotated segments, in chronological order.
func TestAppendLogRotatesAndKeepsHistory(t *testing.T) {
	s := newTestStore(t)
	settings := model.DefaultSettings()
	settings.LogMaxBytes = 64 * 1024
	settings.LogBackupCount = 5
	writeTestSettings(t, s, settings)

	payload := strings.Repeat("a", 40*1024)
	for _, ts := range []string{"2025-01-01 00:00:01", "2025-01-01 00:00:02", "2025-01-01 00:00:03"} {
		if err := s.AppendLog(entry(7, ts, payload)); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	// The live file must stay within the configured budget …
	if info, err := os.Stat(s.logFilePath(7)); err != nil {
		t.Fatalf("stat live log: %v", err)
	} else if info.Size() > int64(settings.LogMaxBytes+len(payload)+4096) {
		t.Fatalf("live log not bounded: %d bytes", info.Size())
	}
	// … while the rotated segments still exist.
	if _, err := os.Stat(s.logBackupPath(7, 1)); err != nil {
		t.Fatalf("expected rotated segment: %v", err)
	}

	logs, err := s.ListLogs(7)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(logs) != 3 {
		t.Fatalf("expected 3 entries across segments, got %d", len(logs))
	}
	for i, want := range []string{"2025-01-01 00:00:01", "2025-01-01 00:00:02", "2025-01-01 00:00:03"} {
		if logs[i].CreatedAt != want {
			t.Fatalf("entry %d: got %s, want %s", i, logs[i].CreatedAt, want)
		}
	}
}

func TestClearLogsRemovesRotatedSegments(t *testing.T) {
	s := newTestStore(t)
	settings := model.DefaultSettings()
	settings.LogMaxBytes = 64 * 1024
	writeTestSettings(t, s, settings)

	payload := strings.Repeat("b", 40*1024)
	for i := 0; i < 3; i++ {
		if err := s.AppendLog(entry(3, "2025-01-01 00:00:0"+string(rune('1'+i)), payload)); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	if _, err := os.Stat(s.logBackupPath(3, 1)); err != nil {
		t.Fatalf("expected rotated segment before clear: %v", err)
	}

	if err := s.ClearLogs(3); err != nil {
		t.Fatalf("clear: %v", err)
	}
	logs, err := s.ListLogs(3)
	if err != nil {
		t.Fatalf("list after clear: %v", err)
	}
	if len(logs) != 0 {
		t.Fatalf("expected no logs after clear, got %d", len(logs))
	}
	if _, err := os.Stat(s.logBackupPath(3, 1)); !os.IsNotExist(err) {
		t.Fatalf("rotated segment survived clear: %v", err)
	}
}

// A global limit starves quiet tasks; the per-task window must give every task
// the same depth.
func TestListLogsPerTaskWindow(t *testing.T) {
	s := newTestStore(t)

	// Task 1 is chatty, task 2 is quiet.
	for _, ts := range []string{"2025-01-01 10:00:01", "2025-01-01 10:00:02", "2025-01-01 10:00:03", "2025-01-01 10:00:04", "2025-01-01 10:00:05"} {
		if err := s.AppendLog(entry(1, ts, "")); err != nil {
			t.Fatalf("append task 1: %v", err)
		}
	}
	for _, ts := range []string{"2025-01-01 09:00:00", "2025-01-01 09:30:00"} {
		if err := s.AppendLog(entry(2, ts, "")); err != nil {
			t.Fatalf("append task 2: %v", err)
		}
	}

	logs, err := s.ListLogsPerTask(2)
	if err != nil {
		t.Fatalf("per-task: %v", err)
	}
	perTask := map[int]int{}
	for _, l := range logs {
		perTask[l.TaskID]++
	}
	if perTask[1] != 2 || perTask[2] != 2 {
		t.Fatalf("expected 2 entries per task, got %+v", perTask)
	}
	// Newest first overall.
	if logs[0].CreatedAt != "2025-01-01 10:00:05" {
		t.Fatalf("expected newest entry first, got %s", logs[0].CreatedAt)
	}
}

// A rotated segment must not be read twice and must not stop the task from
// being listed when the live file is empty.
func TestListLogsAllSkipsSegmentsWhenScanningDirectory(t *testing.T) {
	s := newTestStore(t)

	if err := s.AppendLog(entry(4, "2025-01-01 00:00:01", "")); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := os.WriteFile(s.logBackupPath(4, 1), []byte(`[]`+"\n"), 0600); err != nil {
		t.Fatalf("write segment: %v", err)
	}

	all, err := s.ListLogs(0)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(all))
	}
}

// History that only lives in a rotated segment must still show up.
func TestListLogsAllReadsSegmentWithoutLiveFile(t *testing.T) {
	s := newTestStore(t)

	out, err := marshalLogs([]model.LogEntry{entry(9, "2025-01-01 00:00:01", "old")})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(s.logBackupPath(9, 1), out, 0600); err != nil {
		t.Fatalf("write segment: %v", err)
	}

	all, err := s.ListLogs(0)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 1 || all[0].TaskID != 9 {
		t.Fatalf("expected the rotated entry, got %+v", all)
	}
}
