package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"cronplus/pkg/model"
	"time"
)

// logFilePath returns the path for a task's log file.
func (s *Store) logFilePath(taskID int) string {
	return filepath.Join(s.LogDir, fmt.Sprintf("task_%d.json", taskID))
}

// AppendLog appends a log entry to the task's log file.
func (s *Store) AppendLog(entry model.LogEntry) error {
	s.logMu.Lock()
	defer s.logMu.Unlock()

	path := s.logFilePath(entry.TaskID)

	var logs []model.LogEntry
	data, err := os.ReadFile(path)
	if err == nil && len(data) > 0 {
		json.Unmarshal(data, &logs)
	}
	if logs == nil {
		logs = []model.LogEntry{}
	}

	logs = append(logs, entry)

	// Keep max 1000 entries per task
	const maxEntries = 1000
	if len(logs) > maxEntries {
		logs = logs[len(logs)-maxEntries:]
	}

	out, err := json.MarshalIndent(logs, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal log: %w", err)
	}
	out = append(out, '\n')
	return atomicWrite(path, out)
}

// ListLogs reads logs for a specific task. If taskID <= 0, reads all tasks.
func (s *Store) ListLogs(taskID int) ([]model.LogEntry, error) {
	if taskID > 0 {
		return s.readTaskLog(taskID)
	}
	// Read all task log files
	entries, err := os.ReadDir(s.LogDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []model.LogEntry{}, nil
		}
		return nil, err
	}
	var all []model.LogEntry
	for _, e := range entries {
		var id int
		if _, err := fmt.Sscanf(e.Name(), "task_%d.json", &id); err != nil {
			continue
		}
		logs, err := s.readTaskLog(id)
		if err != nil {
			continue
		}
		all = append(all, logs...)
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].CreatedAt > all[j].CreatedAt
	})
	return all, nil
}

func (s *Store) readTaskLog(taskID int) ([]model.LogEntry, error) {
	path := s.logFilePath(taskID)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []model.LogEntry{}, nil
		}
		return nil, err
	}
	var logs []model.LogEntry
	if err := json.Unmarshal(data, &logs); err != nil {
		return nil, err
	}
	if logs == nil {
		logs = []model.LogEntry{}
	}
	return logs, nil
}

// ClearLogs removes all log files, or logs for a specific task.
func (s *Store) ClearLogs(taskID int) error {
	if taskID > 0 {
		path := s.logFilePath(taskID)
		return os.Remove(path)
	}
	// Remove all task_*.json files
	entries, err := os.ReadDir(s.LogDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			os.Remove(filepath.Join(s.LogDir, e.Name()))
		}
	}
	return nil
}

// CleanupLogs prunes logs based on per-task retention settings.
func (s *Store) CleanupLogs(tasks []model.Task) error {
	retention := map[int]struct{ days, max int }{}
	for _, t := range tasks {
		if t.LogRetention > 0 || t.LogMaxEntries > 0 {
			retention[t.ID] = struct{ days, max int }{t.LogRetention, t.LogMaxEntries}
		}
	}
	if len(retention) == 0 {
		return nil
	}

	now := time.Now()
	for tid, rule := range retention {
		logs, err := s.readTaskLog(tid)
		if err != nil || len(logs) == 0 {
			continue
		}
		var filtered []model.LogEntry
		for _, l := range logs {
			// Max entries: keep last N
			// Retention days: remove old
			keep := true
			if rule.days > 0 {
				t, err := time.Parse("2006-01-02 15:04:05", l.CreatedAt)
				if err == nil && now.Sub(t).Hours() > float64(rule.days*24) {
					keep = false
				}
			}
			if keep {
				filtered = append(filtered, l)
			}
		}
		// Max entries filter
		if rule.max > 0 && len(filtered) > rule.max {
			filtered = filtered[len(filtered)-rule.max:]
		}
		if len(filtered) != len(logs) {
			out, _ := json.MarshalIndent(filtered, "", "  ")
			out = append(out, '\n')
			atomicWrite(s.logFilePath(tid), out)
		}
	}
	return nil
}

// DeleteTaskLog removes the log file for a specific task.
func (s *Store) DeleteTaskLog(taskID int) error {
	path := s.logFilePath(taskID)
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
