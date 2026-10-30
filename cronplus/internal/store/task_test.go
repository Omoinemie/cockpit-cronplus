package store

import (
	"path/filepath"
	"testing"

	"cronplus/pkg/model"
)

// Deleting a task must take its execution history with it.
//
// The UI used to drop only the config entry, so task_N.json stayed behind:
// the log view kept listing runs of a task that no longer existed, and since
// IDs are assigned as max+1, the next task created after deleting the
// highest-ID task inherited the deleted history.
func TestDeleteTaskRemovesItsLogs(t *testing.T) {
	s := newTestStore(t)

	for _, id := range []int{1, 2} {
		tk := model.Task{ID: id, Title: "t", Command: "true", Schedule: "0 * * * * *"}
		if err := s.CreateTask(&tk); err != nil {
			t.Fatalf("create task %d: %v", id, err)
		}
	}
	for _, id := range []int{1, 2} {
		if err := s.AppendLog(entry(id, "2025-01-01 00:00:01", "out")); err != nil {
			t.Fatalf("append log %d: %v", id, err)
		}
	}

	if err := s.DeleteTask(2); err != nil {
		t.Fatalf("delete task 2: %v", err)
	}

	if logs, err := s.ListLogs(2); err != nil || len(logs) != 0 {
		t.Fatalf("logs of deleted task should be gone, got %d (err=%v)", len(logs), err)
	}
	// The surviving task keeps its own history.
	if logs, err := s.ListLogs(1); err != nil || len(logs) != 1 {
		t.Fatalf("logs of surviving task should stay, got %d (err=%v)", len(logs), err)
	}
	// The file itself is gone, not just emptied.
	if _, err := filepath.Glob(filepath.Join(s.LogDir, "task_2.json*")); err != nil {
		t.Fatalf("glob: %v", err)
	}
	if m, _ := filepath.Glob(filepath.Join(s.LogDir, "task_2.json*")); len(m) != 0 {
		t.Fatalf("task_2 log files should be removed, found %v", m)
	}

	tasks, err := s.ListTasks()
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != 1 {
		t.Fatalf("expected only task 1 to remain, got %+v", tasks)
	}
}

// A new task must not inherit the history of a deleted one that had the same ID.
func TestReusedTaskIDStartsWithEmptyHistory(t *testing.T) {
	s := newTestStore(t)

	first := model.Task{ID: 1, Title: "old", Command: "true", Schedule: "0 * * * * *"}
	if err := s.CreateTask(&first); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.AppendLog(entry(1, "2025-01-01 00:00:01", "old run")); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := s.DeleteTask(1); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// max+1 hands the ID out again now that it was freed.
	second := model.Task{Title: "new", Command: "true", Schedule: "0 * * * * *"}
	if err := s.CreateTask(&second); err != nil {
		t.Fatalf("create second: %v", err)
	}
	if second.ID != 1 {
		t.Skipf("ID was not reused (got %d); assertion below is meaningless", second.ID)
	}

	logs, err := s.ListLogs(second.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(logs) != 0 {
		t.Fatalf("reused ID should start with no history, got %d entries", len(logs))
	}
}
