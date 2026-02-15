package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sync"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"
)

type TaskExecutor struct {
	localCache *LocalCache
	stampCache *FileStampCache

	taskKeyMu sync.Mutex
	taskKeyBy map[TaskID]string

	taskExecMu sync.Mutex
	taskExecBy map[TaskID]taskExecEntry

	taskExecGroup singleflight.Group
}

type taskExecEntry struct {
	done bool
	err  error
}

func NewTaskExecutor(cacheRoot string, stampCachePath string) *TaskExecutor {
	return &TaskExecutor{
		localCache: NewLocalCache(cacheRoot),
		stampCache: NewFileStampCache(stampCachePath),
		taskKeyBy:  make(map[TaskID]string),
		taskExecBy: make(map[TaskID]taskExecEntry),
	}
}

func (e *TaskExecutor) Load() error {
	return e.stampCache.Load()
}

func (e *TaskExecutor) Save() error {
	return e.stampCache.Save()
}

// updateOutputStamps hashes output files and records their stamps so that
// downstream tasks (which may consume these outputs as inputs) get stamp cache
// hits instead of re-hashing.
func (e *TaskExecutor) updateOutputStamps(outputs []Path) {
	for _, out := range outputs {
		p := filepath.FromSlash(string(out))
		d, err := hashFile(p)
		if err != nil {
			continue
		}
		e.stampCache.Update(p, d)
	}
}

func (e *TaskExecutor) computeAndCacheKey(task Task) (string, []byte, error) {
	e.taskKeyMu.Lock()

	depKeys := make([]string, 0, len(task.Dependencies))
	for _, dep := range task.Dependencies {
		k, ok := e.taskKeyBy[dep]
		if !ok {
			e.taskKeyMu.Unlock()
			return "", nil, fmt.Errorf("missing dependency task key for %s", dep)
		}
		depKeys = append(depKeys, k)
	}
	e.taskKeyMu.Unlock()

	taskKey, taskJSON, err := ComputeTaskKey(task, depKeys, e.stampCache)
	if err != nil {
		return "", nil, fmt.Errorf("compute task key: %w", err)
	}

	e.taskKeyMu.Lock()
	e.taskKeyBy[task.ID] = taskKey
	e.taskKeyMu.Unlock()
	return taskKey, taskJSON, nil
}

func (e *TaskExecutor) ExecuteTasks(taskMap TaskMap, taskIDs []TaskID) error {
	g := new(errgroup.Group)

	for _, id := range taskIDs {
		task, exists := taskMap[id]
		if !exists {
			return fmt.Errorf("task %s not found", id)
		}
		t := task

		g.Go(func() error {
			return e.executeTask(taskMap, t)
		})
	}

	return g.Wait()
}

func (e *TaskExecutor) tryGetTaskResult(taskID TaskID) (taskExecEntry, bool) {
	e.taskExecMu.Lock()
	entry, ok := e.taskExecBy[taskID]
	e.taskExecMu.Unlock()
	if !ok || !entry.done {
		return taskExecEntry{}, false
	}
	return entry, true
}

func (e *TaskExecutor) storeTaskResult(taskID TaskID, err error) {
	e.taskExecMu.Lock()
	e.taskExecBy[taskID] = taskExecEntry{done: true, err: err}
	e.taskExecMu.Unlock()
}

func (e *TaskExecutor) executeTask(taskMap TaskMap, task Task) error {
	// Use singleflight to coalesce concurrent executions of the same task.
	// We also memoize the result so a task only runs once per process.
	if entry, ok := e.tryGetTaskResult(task.ID); ok {
		return entry.err
	}

	_, err, _ := e.taskExecGroup.Do(string(task.ID), func() (any, error) {
		// Another goroutine may have completed the task while we were waiting.
		if entry, ok := e.tryGetTaskResult(task.ID); ok {
			return nil, entry.err
		}

		err := e.doExecuteTask(taskMap, task)
		e.storeTaskResult(task.ID, err)

		return nil, err
	})

	return err
}

func (e *TaskExecutor) doExecuteTask(taskMap TaskMap, task Task) error {
	// Execute dependencies in parallel.
	if len(task.Dependencies) > 0 {
		if err := e.ExecuteTasks(taskMap, task.Dependencies); err != nil {
			return err
		}
	}

	taskKey, taskJSON, err := e.computeAndCacheKey(task)
	if err != nil {
		return fmt.Errorf("compute task key for task %s: %w", task.ID, err)
	}

	// Lookup from cache
	if task.Cache {
		hit, err := e.localCache.Restore(taskKey, task.Outputs)
		if err != nil {
			return fmt.Errorf("cache restore: %w", err)
		}

		if hit {
			fmt.Printf("CACHE HIT %s\n", task.ID)
			return nil
		}
	}

	// Execute task
	fmt.Printf("Executing task %s: %s\n", task.ID, task.Command)
	cmd := exec.Command("sh", "-c", task.Command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("execute task %s: %w", task.ID, err)
	}
	fmt.Printf("Output of task %s:\n%s\n", task.ID, string(output))

	// Store in cache
	if task.Cache {
		if err := e.localCache.Store(taskKey, taskJSON, task.Outputs); err != nil {
			return fmt.Errorf("cache store error for task %s: %w", task.ID, err)
		}
	}

	e.updateOutputStamps(task.Outputs)

	return nil
}
