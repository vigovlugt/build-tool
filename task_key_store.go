package main

import (
	"fmt"
	"sync"
)

type TaskKeyStore struct {
	mu sync.Mutex
	by map[TaskID]string
}

func NewTaskKeyStore() *TaskKeyStore {
	return &TaskKeyStore{by: make(map[TaskID]string)}
}

func (s *TaskKeyStore) Get(taskID TaskID) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k, ok := s.by[taskID]
	return k, ok
}

func (s *TaskKeyStore) Set(taskID TaskID, taskKey string) {
	s.mu.Lock()
	s.by[taskID] = taskKey
	s.mu.Unlock()
}

func (s *TaskKeyStore) GetDepKeys(task Task) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	depKeys := make([]string, 0, len(task.Dependencies))
	for _, dep := range task.Dependencies {
		k, ok := s.by[dep]
		if !ok {
			return nil, fmt.Errorf("missing dependency task key for %s", dep)
		}
		depKeys = append(depKeys, k)
	}
	return depKeys, nil
}
