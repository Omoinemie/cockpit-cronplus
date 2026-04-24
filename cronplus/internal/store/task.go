package store

import (
	"fmt"
	"cronplus/pkg/model"
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
	ReadJSON(s.ConfPath, &tasks)
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

// ToggleTask flips the enabled state of a task.
func (s *Store) ToggleTask(id int) (*model.Task, error) {
	s.taskMu.Lock()
	defer s.taskMu.Unlock()

	var tasks []model.Task
	if _, err := ReadJSON(s.ConfPath, &tasks); err != nil {
		return nil, err
	}
	for i := range tasks {
		if tasks[i].ID == id {
			tasks[i].Enabled = !tasks[i].Enabled
			if err := writeJSON(s.ConfPath, tasks); err != nil {
				return nil, err
			}
			return &tasks[i], nil
		}
	}
	return nil, fmt.Errorf("task %d not found", id)
}
