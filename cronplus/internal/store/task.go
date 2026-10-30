package store

import (
	"cronplus/pkg/model"
	"fmt"
)

// ListTasks reads all tasks from the config file.
func (s *Store) ListTasks() ([]model.Task, error) {
	s.taskMu.RLock()
	defer s.taskMu.RUnlock()

	var tasks []model.Task
	_, err := ReadJSON(s.ConfPath, &tasks)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	if tasks == nil {
		tasks = []model.Task{}
	}
	return tasks, nil
}

// GetTask returns a single task by ID.
func (s *Store) GetTask(id int) (*model.Task, error) {
	tasks, err := s.ListTasks()
	if err != nil {
		return nil, err
	}
	for i := range tasks {
		if tasks[i].ID == id {
			return &tasks[i], nil
		}
	}
	return nil, fmt.Errorf("task %d not found", id)
}

// CreateTask appends a new task, auto-assigning ID.
func (s *Store) CreateTask(t *model.Task) error {
	s.taskMu.Lock()
	defer s.taskMu.Unlock()

	// Validate before write
	if err := model.ValidateTask(t); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	var tasks []model.Task
	if _, err := ReadJSON(s.ConfPath, &tasks); err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	if tasks == nil {
		tasks = []model.Task{}
	}

	maxID := 0
	for _, existing := range tasks {
		if existing.ID > maxID {
			maxID = existing.ID
		}
	}
	t.ID = maxID + 1
	t.Defaults()
	tasks = append(tasks, *t)
	return writeJSON(s.ConfPath, tasks)
}

// UpdateTask replaces a task by ID.
func (s *Store) UpdateTask(id int, t *model.Task) error {
	s.taskMu.Lock()
	defer s.taskMu.Unlock()

	// Validate before write
	if err := model.ValidateTask(t); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	var tasks []model.Task
	if _, err := ReadJSON(s.ConfPath, &tasks); err != nil {
		return err
	}
	for i := range tasks {
		if tasks[i].ID == id {
			t.ID = id
			tasks[i] = *t
			return writeJSON(s.ConfPath, tasks)
		}
	}
	return fmt.Errorf("task %d not found", id)
}

// DeleteTask removes a task by ID.
func (s *Store) DeleteTask(id int) error {
	s.taskMu.Lock()
	defer s.taskMu.Unlock()

	var tasks []model.Task
	if _, err := ReadJSON(s.ConfPath, &tasks); err != nil {
		return err
	}
	filtered := make([]model.Task, 0, len(tasks))
	found := false
	for _, t := range tasks {
		if t.ID == id {
			found = true
			continue
		}
		filtered = append(filtered, t)
	}
	if !found {
		return fmt.Errorf("task %d not found", id)
	}
	return writeJSON(s.ConfPath, filtered)
}

// UpdateTaskRunSeq updates just the run_seq field of a task (atomic partial update).
func (s *Store) UpdateTaskRunSeq(id int, seq int) error {
	s.taskMu.Lock()
	defer s.taskMu.Unlock()

	var tasks []model.Task
	if _, err := ReadJSON(s.ConfPath, &tasks); err != nil {
		return err
	}
	for i := range tasks {
		if tasks[i].ID == id {
			tasks[i].RunSeq = seq
			tasks[i].RunCount = seq
			return writeJSON(s.ConfPath, tasks)
		}
	}
	return fmt.Errorf("task %d not found", id)
}

// ToggleTaskEnabled sets the enabled state of a task to a specific value.
func (s *Store) ToggleTaskEnabled(id int, enabled bool) error {
	s.taskMu.Lock()
	defer s.taskMu.Unlock()

	var tasks []model.Task
	if _, err := ReadJSON(s.ConfPath, &tasks); err != nil {
		return err
	}
	for i := range tasks {
		if tasks[i].ID == id {
			tasks[i].Enabled = enabled
			return writeJSON(s.ConfPath, tasks)
		}
	}
	return fmt.Errorf("task %d not found", id)
}

// CreateTasks inserts several tasks with a single read/write cycle.
//
// Import used to call CreateTask in a loop, and every call re-read and rewrote
// the whole tasks.conf: O(n²) for n imported tasks.
func (s *Store) CreateTasks(items []model.Task) (created int, skipped []error, err error) {
	s.taskMu.Lock()
	defer s.taskMu.Unlock()

	var tasks []model.Task
	if _, readErr := ReadJSON(s.ConfPath, &tasks); readErr != nil {
		return 0, nil, fmt.Errorf("read config: %w", readErr)
	}
	if tasks == nil {
		tasks = []model.Task{}
	}

	maxID := 0
	for _, existing := range tasks {
		if existing.ID > maxID {
			maxID = existing.ID
		}
	}

	for i := range items {
		t := items[i]
		if verr := model.ValidateTask(&t); verr != nil {
			skipped = append(skipped, fmt.Errorf("task %q: %w", t.Title, verr))
			continue
		}
		maxID++
		t.ID = maxID
		t.Defaults()
		tasks = append(tasks, t)
		created++
	}

	if created == 0 {
		return 0, skipped, nil
	}
	if err := writeJSON(s.ConfPath, tasks); err != nil {
		return 0, skipped, err
	}
	return created, skipped, nil
}
