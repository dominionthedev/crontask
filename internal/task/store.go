package task

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/dominionthedev/crontask/internal/config"
)

var storeMu sync.Mutex

// Store persists tasks to disk.
type Store struct {
	path string
}

func NewStore() *Store {
	return &Store{path: config.TasksFile()}
}

func (s *Store) Load() (map[string]*Task, error) {
	storeMu.Lock()
	defer storeMu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]*Task{}, nil
		}
		return nil, err
	}

	var raw map[string]*Task
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse tasks file: %w", err)
	}
	if raw == nil {
		raw = map[string]*Task{}
	}
	return raw, nil
}

func (s *Store) Save(tasks map[string]*Task) error {
	storeMu.Lock()
	defer storeMu.Unlock()

	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o644)
}

func (s *Store) Get(name string) (*Task, error) {
	tasks, err := s.Load()
	if err != nil {
		return nil, err
	}
	t, ok := tasks[name]
	if !ok {
		return nil, fmt.Errorf("unknown task %q", name)
	}
	return t, nil
}

func (s *Store) Put(t *Task) error {
	tasks, err := s.Load()
	if err != nil {
		return err
	}
	tasks[t.Name] = t
	return s.Save(tasks)
}

func (s *Store) Delete(name string) error {
	tasks, err := s.Load()
	if err != nil {
		return err
	}
	if _, ok := tasks[name]; !ok {
		return fmt.Errorf("unknown task %q", name)
	}
	delete(tasks, name)
	return s.Save(tasks)
}
