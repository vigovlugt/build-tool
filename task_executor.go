package main

import (
	"fmt"
	"os/exec"

	"golang.org/x/sync/errgroup"
)

type TaskExecutor struct {
	state *BuildState
	keys  *TaskKeyStore
	memo  *TaskMemo
}

func NewTaskExecutor(cacheRoot string, stampCachePath string) *TaskExecutor {
	return &TaskExecutor{
		state: NewBuildState(cacheRoot, stampCachePath),
		keys:  NewTaskKeyStore(),
		memo:  NewTaskMemo(),
	}
}

func (e *TaskExecutor) Load() error {
	return e.state.Load()
}

func (e *TaskExecutor) Save() error {
	return e.state.Save()
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

func (e *TaskExecutor) executeTask(taskMap TaskMap, task Task) error {
	return e.memo.Do(task.ID, func() error {
		return e.doExecuteTask(taskMap, task)
	})
}

func (e *TaskExecutor) doExecuteTask(taskMap TaskMap, task Task) error {
	// Execute dependencies in parallel.
	if len(task.Dependencies) > 0 {
		if err := e.ExecuteTasks(taskMap, task.Dependencies); err != nil {
			return err
		}
	}

	depKeys, err := e.keys.GetDepKeys(task)
	if err != nil {
		return err
	}

	taskKey, taskJSON, err := e.state.ComputeKey(task, depKeys)
	if err != nil {
		return fmt.Errorf("compute task key for task %s: %w", task.ID, err)
	}
	e.keys.Set(task.ID, taskKey)

	// Lookup from cache
	if task.Cache {
		hit, err := e.state.Restore(taskKey, task.Outputs)
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
		var expandedOutputs []Path
		if len(task.Outputs) > 0 {
			expandedOutputs, err = ExpandFileSpecs(task.Outputs)
			if err != nil {
				return fmt.Errorf("expand outputs for task %s: %w", task.ID, err)
			}
		}

		if err := e.state.Store(taskKey, taskJSON, expandedOutputs); err != nil {
			return fmt.Errorf("cache store error for task %s: %w", task.ID, err)
		}

		e.state.UpdateOutputStamps(expandedOutputs)
	}

	return nil
}
