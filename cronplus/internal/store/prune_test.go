package store

import (
	"os"
	"strings"
	"testing"

	"cronplus/pkg/model"
)

func statusEntry(taskID int, ts, status string) model.LogEntry {
	e := entry(taskID, ts, "out")
	e.Status = status
	return e
}

// Pruning must delete exactly the selected entries. The WebUI used to delete the
// whole task history for every cleanup mode, silently losing entries the preview
// said would be kept.
func TestPruneByStatusOnlyRemovesMatching(t *testing.T) {
	s := newTestStore(t)

	seed := []model.LogEntry{
		statusEntry(1, "2025-01-01 00:00:01", "success"),
		statusEntry(1, "2025-01-01 00:00:02", "error"),
		statusEntry(1, "2025-01-01 00:00:03", "success"),
		statusEntry(2, "2025-01-01 00:00:04", "error"),
	}
	for _, e := range seed {
		if err := s.AppendLog(e); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	deleted, err := s.Prune(PruneFilter{Status: "error"})
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("deleted %d entries, want 2", deleted)
	}

	all, err := s.ListLogs(0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 entries left, got %d", len(all))
	}
	for _, e := range all {
		if e.Status != "success" {
			t.Fatalf("a matching entry survived: %+v", e)
		}
	}
}

func TestPruneByBeforeDate(t *testing.T) {
	s := newTestStore(t)
	for _, ts := range []string{"2025-01-01 00:00:00", "2025-02-01 00:00:00", "2025-03-01 00:00:00"} {
		if err := s.AppendLog(entry(5, ts, "")); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	deleted, err := s.Prune(PruneFilter{Before: "2025-02-01 00:00:00"})
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted %d, want 1", deleted)
	}
	logs, _ := s.ListLogs(5)
	if len(logs) != 2 || logs[0].CreatedAt != "2025-02-01 00:00:00" {
		t.Fatalf("wrong survivors: %+v", logs)
	}
}

func TestPruneKeepNewestPerTask(t *testing.T) {
	s := newTestStore(t)
	for i := 1; i <= 4; i++ {
		ts := "2025-01-01 00:00:0" + string(rune('0'+i))
		if err := s.AppendLog(entry(1, ts, "")); err != nil {
			t.Fatalf("append: %v", err)
		}
		if err := s.AppendLog(entry(2, ts, "")); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	deleted, err := s.Prune(PruneFilter{Keep: 2})
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if deleted != 4 {
		t.Fatalf("deleted %d, want 4", deleted)
	}
	for _, tid := range []int{1, 2} {
		logs, _ := s.ListLogs(tid)
		if len(logs) != 2 {
			t.Fatalf("task %d kept %d entries, want 2", tid, len(logs))
		}
		if logs[0].CreatedAt != "2025-01-01 00:00:03" || logs[1].CreatedAt != "2025-01-01 00:00:04" {
			t.Fatalf("task %d kept the wrong entries: %v", tid, logs)
		}
	}
}

func TestPruneDryRunChangesNothing(t *testing.T) {
	s := newTestStore(t)
	if err := s.AppendLog(statusEntry(1, "2025-01-01 00:00:01", "error")); err != nil {
		t.Fatalf("append: %v", err)
	}
	deleted, err := s.Prune(PruneFilter{Status: "error", DryRun: true})
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("dry run counted %d, want 1", deleted)
	}
	if logs, _ := s.ListLogs(1); len(logs) != 1 {
		t.Fatal("dry run deleted an entry")
	}
}

func TestPruneRefusesEmptyFilter(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Prune(PruneFilter{}); err == nil {
		t.Fatal("empty filter accepted")
	}
	if _, err := s.Prune(PruneFilter{DryRun: true}); err == nil {
		t.Fatal("filter with only dry-run accepted")
	}
}

// New entries must be appended as single JSON lines; the old read-modify-write
// rewrote the whole file for every run.
func TestAppendLogUsesJSONLines(t *testing.T) {
	s := newTestStore(t)
	for _, ts := range []string{"2025-01-01 00:00:01", "2025-01-01 00:00:02"} {
		if err := s.AppendLog(entry(8, ts, "x")); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	data, err := os.ReadFile(s.logFilePath(8))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 JSONL lines, got %d: %q", len(lines), data)
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, "{") {
			t.Fatalf("line is not a JSON object: %q", line)
		}
	}
}

// Log files written by older versions are JSON arrays; they must stay readable,
// and the next append must not corrupt them.
func TestLegacyJSONArrayIsStillReadable(t *testing.T) {
	s := newTestStore(t)
	legacy, err := marshalLogs([]model.LogEntry{
		entry(6, "2025-01-01 00:00:01", "old-1"),
		entry(6, "2025-01-01 00:00:02", "old-2"),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(s.logFilePath(6), legacy, 0600); err != nil {
		t.Fatalf("write legacy: %v", err)
	}

	logs, err := s.ListLogs(6)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(logs) != 2 || logs[0].Output != "old-1" {
		t.Fatalf("legacy file not read correctly: %+v", logs)
	}

	// Appending to a legacy array file would produce invalid JSON unless the
	// reader tolerates both shapes; the array is read, then rewritten as JSONL
	// by the next compaction. Appending keeps the two old entries readable.
	if err := s.AppendLog(entry(6, "2025-01-01 00:00:03", "new")); err != nil {
		t.Fatalf("append to legacy: %v", err)
	}
	logs, err = s.ListLogs(6)
	if err != nil {
		t.Fatalf("list after append: %v", err)
	}
	if len(logs) != 3 {
		t.Fatalf("expected 3 entries after append, got %d", len(logs))
	}
}
