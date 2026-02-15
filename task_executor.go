package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"golang.org/x/sync/errgroup"
)

type TaskExecutor struct {
	state *BuildState
	keys  *TaskKeyStore
	memo  *TaskMemo
	log   *Logger
}

func NewTaskExecutor(cacheRoot string, stampCachePath string, log *Logger) *TaskExecutor {
	return &TaskExecutor{
		state: NewBuildState(cacheRoot, stampCachePath),
		keys:  NewTaskKeyStore(),
		memo:  NewTaskMemo(),
		log:   log,
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
			e.log.Taskf(task.ID, "CACHE HIT")
			return nil
		}
	}

	// Execute task
	e.log.Taskf(task.ID, "$ %s", task.Command)

	cmd := exec.Command("sh", "-c", task.Command)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe for task %s: %w", task.ID, err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe for task %s: %w", task.ID, err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start task %s: %w", task.ID, err)
	}

	g := new(errgroup.Group)
	g.Go(func() error { return e.copyTaskOutput(task.ID, stdout) })
	g.Go(func() error { return e.copyTaskOutput(task.ID, stderr) })

	waitErr := cmd.Wait()
	copyErr := g.Wait()
	if copyErr != nil {
		return fmt.Errorf("read output for task %s: %w", task.ID, copyErr)
	}
	if waitErr != nil {
		return fmt.Errorf("execute task %s: %w", task.ID, waitErr)
	}

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

func (e *TaskExecutor) copyTaskOutput(taskID TaskID, r io.Reader) error {
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadString('\n')
		if line != "" {
			line = strings.TrimSuffix(line, "\n")
			line = strings.TrimSuffix(line, "\r")
			e.log.TaskLine(taskID, line)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}
