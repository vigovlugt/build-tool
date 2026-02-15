package main

import (
	"sync"

	"golang.org/x/sync/singleflight"
)

type TaskMemo struct {
	mu    sync.Mutex
	by    map[TaskID]taskExecEntry
	group singleflight.Group
}

type taskExecEntry struct {
	done bool
	err  error
}

func NewTaskMemo() *TaskMemo {
	return &TaskMemo{by: make(map[TaskID]taskExecEntry)}
}

func (m *TaskMemo) tryGet(taskID TaskID) (taskExecEntry, bool) {
	m.mu.Lock()
	entry, ok := m.by[taskID]
	m.mu.Unlock()
	if !ok || !entry.done {
		return taskExecEntry{}, false
	}
	return entry, true
}

func (m *TaskMemo) store(taskID TaskID, err error) {
	m.mu.Lock()
	m.by[taskID] = taskExecEntry{done: true, err: err}
	m.mu.Unlock()
}

func (m *TaskMemo) Do(taskID TaskID, fn func() error) error {
	if entry, ok := m.tryGet(taskID); ok {
		return entry.err
	}

	_, err, _ := m.group.Do(string(taskID), func() (any, error) {
		if entry, ok := m.tryGet(taskID); ok {
			return nil, entry.err
		}

		err := fn()
		m.store(taskID, err)
		return nil, err
	})

	return err
}
