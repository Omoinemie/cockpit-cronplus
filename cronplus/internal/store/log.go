package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"cronplus/pkg/model"
)

const (
	// maxLogEntriesPerTask is a soft cap used when compacting a task's history
	// (cleanup / prune). The live file is bounded by logMaxBytes instead.
	maxLogEntriesPerTask = 1000

	// Mirrors of model.DefaultSettings(), used when settings.json is missing or
	// unreadable so rotation keeps working.
	defaultLogMaxBytes    = 10 * 1024 * 1024
	defaultLogBackupCount = 5

	// minRotateBytes is a floor for logMaxBytes. settings.json accepts values as
	// low as 1KB; with a 256KB entry that would rotate on every single run and
	// throw the history away almost immediately.
	minRotateBytes = 64 * 1024
)

// logFilePath returns the path for a task's live log file.
func (s *Store) logFilePath(taskID int) string {
	return filepath.Join(s.LogDir, fmt.Sprintf("task_%d.json", taskID))
}

// HasTaskLog reports whether any log file (live or rotated) exists for a task.
// Used to tell "this task never ran" apart from "there is no such task": the
// CLI used to print "No logs" for a mistyped id, which reads like an empty
// history rather than an error.
func (s *Store) HasTaskLog(taskID int) bool {
	if _, err := os.Stat(s.logFilePath(taskID)); err == nil {
		return true
	}
	for i := 1; i <= maxBackupProbe; i++ {
		if _, err := os.Stat(s.logBackupPath(taskID, i)); err == nil {
			return true
		}
	}
	return false
}

// maxBackupProbe bounds the rotated-segment scan in HasTaskLog.
const maxBackupProbe = 16

// logBackupPath returns the path of a rotated segment (1 = newest backup).
func (s *Store) logBackupPath(taskID, index int) string {
	return fmt.Sprintf("%s.%d", s.logFilePath(taskID), index)
}

// decodeLogs parses a task log file. It accepts both formats:
//
//   - JSONL, one object per line (what AppendLog writes now)
//   - a JSON array (files written by older versions, still readable)
//
// Entries that are not valid log objects are skipped instead of failing the
// whole file, so one corrupt record cannot hide the entire history of a task.
func decodeLogs(data []byte) []model.LogEntry {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil
	}
	if strings.HasPrefix(trimmed, "[") {
		var raw []json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil
		}
		logs := make([]model.LogEntry, 0, len(raw))
		for _, item := range raw {
			var e model.LogEntry
			if err := json.Unmarshal(item, &e); err != nil {
				continue
			}
			logs = append(logs, e)
		}
		return logs
	}

	logs := make([]model.LogEntry, 0, strings.Count(trimmed, "\n")+1)
	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e model.LogEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		logs = append(logs, e)
	}
	return logs
}

// marshalLogs renders entries as a JSON array. Used for compacted files and by
// tests that seed a segment.
func marshalLogs(logs []model.LogEntry) ([]byte, error) {
	out, err := json.MarshalIndent(logs, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal log: %w", err)
	}
	return append(out, '\n'), nil
}

// encodeLogLine renders one entry as a single JSON line.
func encodeLogLine(entry model.LogEntry) ([]byte, error) {
	out, err := json.Marshal(entry)
	if err != nil {
		return nil, fmt.Errorf("marshal log entry: %w", err)
	}
	return append(out, '\n'), nil
}

// readLogFile reads a single log segment, returning nil when it is missing,
// empty or unparsable.
func readLogFile(path string) []model.LogEntry {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return nil
	}
	return decodeLogs(data)
}

// logLimits returns the effective rotation size and backup count.
func (s *Store) logLimits() (maxBytes, backupCount int) {
	maxBytes = defaultLogMaxBytes
	backupCount = defaultLogBackupCount
	if settings, err := s.ReadSettings(); err == nil {
		if settings.LogMaxBytes > 0 {
			maxBytes = settings.LogMaxBytes
		}
		if settings.LogBackupCount > 0 {
			backupCount = settings.LogBackupCount
		}
	}
	if maxBytes < minRotateBytes {
		maxBytes = minRotateBytes
	}
	return maxBytes, backupCount
}

// AppendLog appends one entry to the task's log file.
//
// Logs are JSON Lines: the entry is appended with a single write instead of
// reading and rewriting the whole file. With 1000 entries of up to 256KB the
// old read-modify-write cycle rewrote up to 250MB for every single run.
func (s *Store) AppendLog(entry model.LogEntry) error {
	s.logMu.Lock()
	defer s.logMu.Unlock()

	line, err := encodeLogLine(entry)
	if err != nil {
		return err
	}

	path := s.logFilePath(entry.TaskID)

	// Files written by older versions are JSON arrays; appending a JSON line to
	// one would corrupt it. Migrate the first time we touch a task.
	if err := s.ensureJSONLLocked(entry.TaskID); err != nil {
		return err
	}

	maxBytes, backupCount := s.logLimits()

	// Rotate before appending so the live file stays within the budget.
	if info, statErr := os.Stat(path); statErr == nil {
		if info.Size() > 0 && info.Size()+int64(len(line)) > int64(maxBytes) {
			if err := s.rotateTaskLogLocked(entry.TaskID, backupCount); err != nil {
				return err
			}
		}
	}

	if err := os.MkdirAll(s.LogDir, 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err := f.Write(line); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// ensureJSONLLocked rewrites a legacy JSON-array log as JSON Lines. History is
// preserved: rotated segments are merged into the new file, and the next append
// rotates it again once it exceeds logMaxBytes.
func (s *Store) ensureJSONLLocked(taskID int) error {
	if s.migrated == nil {
		s.migrated = make(map[int]bool)
	}
	if s.migrated[taskID] {
		return nil
	}
	path := s.logFilePath(taskID)
	if info, err := os.Stat(path); err == nil && info.Size() > 0 {
		if legacy, err := isLegacyArrayFile(path); err != nil {
			return err
		} else if legacy {
			logs, err := s.readTaskLog(taskID)
			if err != nil {
				return err
			}
			if err := s.writeTaskLogLocked(taskID, logs); err != nil {
				return err
			}
		}
	}
	s.migrated[taskID] = true
	return nil
}

// isLegacyArrayFile reports whether the file starts with '[' (JSON array).
func isLegacyArrayFile(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	buf := make([]byte, 1)
	for {
		n, err := f.Read(buf)
		if n == 1 {
			switch buf[0] {
			case ' ', '\t', '\r', '\n':
				continue
			case '[':
				return true, nil
			default:
				return false, nil
			}
		}
		if err != nil {
			return false, nil
		}
	}
}

// rotateTaskLogLocked moves the live file to .1 and shifts older segments,
// dropping anything beyond backupCount.
func (s *Store) rotateTaskLogLocked(taskID, backupCount int) error {
	path := s.logFilePath(taskID)
	if backupCount <= 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	// Oldest backup falls off the end.
	if err := os.Remove(s.logBackupPath(taskID, backupCount)); err != nil && !os.IsNotExist(err) {
		return err
	}
	for i := backupCount - 1; i >= 1; i-- {
		src := s.logBackupPath(taskID, i)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if err := os.Rename(src, s.logBackupPath(taskID, i+1)); err != nil {
			return err
		}
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return os.Rename(path, s.logBackupPath(taskID, 1))
}

// taskLogSegments lists a task's log files from oldest to newest:
// task_N.json.3, .2, .1, then the live task_N.json.
func (s *Store) taskLogSegments(taskID int) []string {
	var segments []string

	if entries, err := os.ReadDir(s.LogDir); err == nil {
		prefix := fmt.Sprintf("task_%d.json.", taskID)
		type segment struct {
			index int
			path  string
		}
		var found []segment
		for _, e := range entries {
			if e.IsDir() || !strings.HasPrefix(e.Name(), prefix) {
				continue
			}
			index, err := strconv.Atoi(strings.TrimPrefix(e.Name(), prefix))
			if err != nil {
				continue
			}
			found = append(found, segment{index, filepath.Join(s.LogDir, e.Name())})
		}
		// Highest index is the oldest segment.
		sort.Slice(found, func(i, j int) bool { return found[i].index > found[j].index })
		for _, seg := range found {
			segments = append(segments, seg.path)
		}
	}

	return append(segments, s.logFilePath(taskID))
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
	seen := make(map[int]bool)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		id, ok := taskIDFromLogFile(e.Name())
		if !ok || seen[id] {
			// Rotated segments belong to the same task and are read together
			// with its live file, so each task is visited once.
			continue
		}
		seen[id] = true
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

// taskIDFromLogFile parses "task_<id>.json" and its rotated segments
// "task_<id>.json.<n>". Anything else (temp files, daemon log) is rejected.
func taskIDFromLogFile(name string) (int, bool) {
	if !strings.HasPrefix(name, "task_") {
		return 0, false
	}
	rest := strings.TrimPrefix(name, "task_")
	dot := strings.Index(rest, ".")
	if dot <= 0 {
		return 0, false
	}
	id, err := strconv.Atoi(rest[:dot])
	if err != nil {
		return 0, false
	}
	suffix := rest[dot:]
	if suffix != ".json" {
		if !strings.HasPrefix(suffix, ".json.") {
			return 0, false
		}
		if _, err := strconv.Atoi(strings.TrimPrefix(suffix, ".json.")); err != nil {
			return 0, false
		}
	}
	return id, true
}

// ListLogsPerTask returns logs across all tasks, newest first, keeping at most
// perTask entries per task.
//
// The plain `logs --all` limit is a single global window: on a busy host one
// frequently scheduled task can fill it alone, and every other task then shows
// only the couple of entries that happen to fall inside it. A per-task window
// gives every task the same depth regardless of how chatty its neighbours are.
func (s *Store) ListLogsPerTask(perTask int) ([]model.LogEntry, error) {
	all, err := s.ListLogs(0)
	if err != nil {
		return nil, err
	}
	if perTask <= 0 {
		return all, nil
	}
	counts := make(map[int]int, 16)
	kept := make([]model.LogEntry, 0, len(all))
	for _, l := range all {
		if counts[l.TaskID] >= perTask {
			continue
		}
		counts[l.TaskID]++
		kept = append(kept, l)
	}
	return kept, nil
}

func (s *Store) readTaskLog(taskID int) ([]model.LogEntry, error) {
	live := s.logFilePath(taskID)
	var logs []model.LogEntry

	for _, path := range s.taskLogSegments(taskID) {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if len(data) == 0 {
			continue
		}
		decoded := decodeLogs(data)
		if decoded == nil {
			if path == live {
				return nil, fmt.Errorf("parse log file %s: neither a JSON array nor JSON lines of log entries", path)
			}
			// One damaged rotated segment must not hide the rest of the history.
			continue
		}
		logs = append(logs, decoded...)
	}

	if logs == nil {
		logs = []model.LogEntry{}
	}
	return logs, nil
}

// ClearLogs removes all log files, or logs for a specific task.
func (s *Store) ClearLogs(taskID int) error {
	s.logMu.Lock()
	defer s.logMu.Unlock()

	if taskID > 0 {
		return s.removeTaskLogLocked(taskID)
	}
	entries, err := os.ReadDir(s.LogDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		// Removes both task_N.json and its rotated segments task_N.json.K.
		if _, ok := taskIDFromLogFile(e.Name()); ok {
			os.Remove(filepath.Join(s.LogDir, e.Name()))
		}
	}
	return nil
}

// removeTaskLogLocked deletes a task's live log file and all rotated segments.
func (s *Store) removeTaskLogLocked(taskID int) error {
	var firstErr error
	for _, path := range s.taskLogSegments(taskID) {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// writeTaskLogLocked replaces a task's whole history (live file + segments)
// with the given entries, using the JSONL format.
func (s *Store) writeTaskLogLocked(taskID int, logs []model.LogEntry) error {
	if err := s.removeTaskLogLocked(taskID); err != nil {
		return err
	}
	if len(logs) == 0 {
		return nil
	}
	var sb strings.Builder
	for _, l := range logs {
		line, err := encodeLogLine(l)
		if err != nil {
			return err
		}
		sb.Write(line)
	}
	return atomicWrite(s.logFilePath(taskID), []byte(sb.String()))
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

	s.logMu.Lock()
	defer s.logMu.Unlock()

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
			if err := s.writeTaskLogLocked(tid, filtered); err != nil {
				return fmt.Errorf("write filtered logs for task %d: %w", tid, err)
			}
		}
	}
	return nil
}

// PruneFilter selects log entries to delete. An empty filter matches nothing:
// the caller must say what it wants removed, so a mistyped command cannot wipe
// every history by accident.
type PruneFilter struct {
	TaskID  int    `json:"task_id"`
	Status  string `json:"status"`
	User    string `json:"user"`
	Trigger string `json:"trigger"`
	Before  string `json:"before"` // created_at < before  ("YYYY-MM-DD HH:MM:SS")
	After   string `json:"after"`  // created_at > after
	Keep    int    `json:"keep"`   // keep the newest N matching entries per task
	DryRun  bool   `json:"dry_run"`
}

// Matches reports whether an entry is selected by the filter (ignoring Keep).
func (f PruneFilter) Matches(e model.LogEntry) bool {
	if f.TaskID > 0 && e.TaskID != f.TaskID {
		return false
	}
	if f.Status != "" && e.Status != f.Status {
		return false
	}
	if f.User != "" {
		user := e.RunUser
		if user == "" {
			user = "root"
		}
		if user != f.User {
			return false
		}
	}
	if f.Trigger != "" && e.Trigger != f.Trigger {
		return false
	}
	if f.Before != "" && e.CreatedAt >= f.Before {
		return false
	}
	if f.After != "" && e.CreatedAt <= f.After {
		return false
	}
	return true
}

// selectsAnything reports whether the filter carries at least one selector.
func (f PruneFilter) selectsAnything() bool {
	return f.TaskID > 0 || f.Status != "" || f.User != "" || f.Trigger != "" ||
		f.Before != "" || f.After != "" || f.Keep > 0
}

// normalizeTimestamp coerces a caller-supplied time into the exact layout the
// log entries use, so the comparison stays a plain string compare.
//
// Without this, `--before 2026-1-1` produced a value that sorts *before*
// "2026-10-30 10:00:00" and the filter silently matched nothing, while
// `--before 2026-13-01` silently matched everything — both paths reported
// "Deleted 0" or worse, deleted the wrong entries.
func normalizeTimestamp(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	layouts := []struct {
		layout string
		fix    func(string) string
	}{
		{"2006-01-02 15:04:05", nil},
		{"2006-01-02T15:04:05", func(v string) string { return strings.Replace(v, "T", " ", 1) }},
		{"2006-01-02", func(v string) string { return v + " 00:00:00" }},
	}
	for _, l := range layouts {
		t, err := time.Parse(l.layout, s)
		if err != nil {
			continue
		}
		if l.fix != nil {
			return l.fix(s), nil
		}
		return t.Format("2006-01-02 15:04:05"), nil
	}
	return "", fmt.Errorf("%q is not a valid time (expected \"YYYY-MM-DD HH:MM:SS\")", s)
}

// Prune deletes the entries selected by the filter and returns how many were
// removed. With Keep > 0 the newest N matching entries of each task survive.
func (s *Store) Prune(filter PruneFilter) (int, error) {
	if !filter.selectsAnything() {
		return 0, fmt.Errorf("refusing to prune without a filter")
	}
	if filter.Status != "" && filter.Status != "success" && filter.Status != "error" {
		return 0, fmt.Errorf("unknown status %q (expected success or error)", filter.Status)
	}
	var err error
	if filter.Before, err = normalizeTimestamp(filter.Before); err != nil {
		return 0, fmt.Errorf("--before: %w", err)
	}
	if filter.After, err = normalizeTimestamp(filter.After); err != nil {
		return 0, fmt.Errorf("--after: %w", err)
	}

	s.logMu.Lock()
	defer s.logMu.Unlock()

	taskIDs, err := s.logTaskIDs()
	if err != nil {
		return 0, err
	}

	deleted := 0
	for _, tid := range taskIDs {
		if filter.TaskID > 0 && tid != filter.TaskID {
			continue
		}
		logs, err := s.readTaskLog(tid)
		if err != nil || len(logs) == 0 {
			continue
		}

		matched := make([]bool, len(logs))
		matchedCount := 0
		for i, l := range logs {
			if filter.Matches(l) {
				matched[i] = true
				matchedCount++
			}
		}
		if matchedCount == 0 {
			continue
		}

		// Keep the newest N matches: logs are in chronological order, so walk
		// backwards and un-mark the first N matches. Entries with an empty
		// timestamp are treated as oldest.
		if filter.Keep > 0 && matchedCount > filter.Keep {
			keep := filter.Keep
			order := make([]int, 0, matchedCount)
			for i := range logs {
				if matched[i] {
					order = append(order, i)
				}
			}
			sort.SliceStable(order, func(a, b int) bool {
				return logs[order[a]].CreatedAt > logs[order[b]].CreatedAt
			})
			for i := 0; i < keep && i < len(order); i++ {
				matched[order[i]] = false
				matchedCount--
			}
		}

		if matchedCount == 0 {
			continue
		}
		if filter.DryRun {
			deleted += matchedCount
			continue
		}
		kept := make([]model.LogEntry, 0, len(logs)-matchedCount)
		for i, l := range logs {
			if !matched[i] {
				kept = append(kept, l)
			}
		}
		if err := s.writeTaskLogLocked(tid, kept); err != nil {
			return deleted, err
		}
		deleted += matchedCount
	}
	return deleted, nil
}

// logTaskIDs lists the task IDs that have at least one log file.
func (s *Store) logTaskIDs() ([]int, error) {
	entries, err := os.ReadDir(s.LogDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	seen := make(map[int]bool)
	ids := make([]int, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		id, ok := taskIDFromLogFile(e.Name())
		if !ok || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids, nil
}
