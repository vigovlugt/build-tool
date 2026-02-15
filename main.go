package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"golang.org/x/sync/errgroup"
)

type TaskID string
type Path string

type Task struct {
	ID           TaskID
	Inputs       []Path
	Outputs      []Path
	Dependencies []TaskID
	Command      string
	Cache        bool // default: true
}

type TaskMap map[TaskID]Task

func NewTaskMap(tasks []Task) TaskMap {
	taskMap := make(TaskMap)
	for _, task := range tasks {
		taskMap[task.ID] = task
	}
	return taskMap
}

// Example tasks, based on `example/Makefile`.
var exampleCTasks = []Task{
	{
		ID:      TaskID("util.o"),
		Inputs:  []Path{"util.h", "util.c"},
		Outputs: []Path{"util.o"},
		Command: "gcc -Wall -Wextra -c util.c",
		Cache:   true,
	},
	{
		ID:      TaskID("main.o"),
		Inputs:  []Path{"util.h", "main.c"},
		Outputs: []Path{"main.o"},
		Command: "gcc -Wall -Wextra -c main.c",
		Cache:   true,
	},
	{
		ID:           TaskID("main"),
		Inputs:       []Path{"util.o", "main.o"},
		Outputs:      []Path{"main"},
		Dependencies: []TaskID{TaskID("util.o"), TaskID("main.o")},
		Command:      "gcc util.o main.o -o main",
		Cache:        true,
	},
	{
		ID:           TaskID("run"),
		Dependencies: []TaskID{TaskID("main")},
		Command:      "./main",
	},
	{
		ID:      TaskID("clean"),
		Command: "rm -f *.o main",
	},
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	taskMap := NewTaskMap(exampleCTasks)
	fmt.Printf("Loaded %d tasks\n", len(taskMap))

	if err := stampCache.Load(); err != nil {
		return fmt.Errorf("load stamp cache: %w", err)
	}
	defer func() {
		if err := stampCache.Save(); err != nil {
			fmt.Fprintf(os.Stderr, "error saving stamp cache: %v\n", err)
		}
	}()

	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		fmt.Println("Usage: go run main.go build <task1> <task2> ...")
		return fmt.Errorf("no tasks specified")
	}

	if args[0] == "build" {
		taskIDs := make([]TaskID, len(args)-1)
		for i, arg := range args[1:] {
			taskIDs[i] = TaskID(arg)
		}

		if err := executeTasks(taskMap, taskIDs); err != nil {
			return err
		}
	}

	return nil
}

var (
	localCache = NewLocalCache(".build-tool/cache")
	stampCache = NewFileStampCache(filepath.Join(".build-tool", "cache", "stamps.json"))

	taskKeyMu sync.Mutex
	taskKeyBy = map[TaskID]string{}

	taskOnceMu sync.Mutex
	taskOnce   = map[TaskID]*taskOnceEntry{}
)

type taskOnceEntry struct {
	once sync.Once
	err  error
}

// updateOutputStamps hashes output files and records their stamps so that
// downstream tasks (which may consume these outputs as inputs) get stamp cache
// hits instead of re-hashing.
func updateOutputStamps(outputs []Path) {
	for _, out := range outputs {
		p := filepath.FromSlash(string(out))
		d, err := hashFile(p)
		if err != nil {
			continue
		}
		stampCache.Update(p, d)
	}
}

func computeAndCacheKey(task Task) (string, []byte, error) {
	taskKeyMu.Lock()

	depKeys := make([]string, 0, len(task.Dependencies))
	for _, dep := range task.Dependencies {
		k, ok := taskKeyBy[dep]
		if !ok {
			taskKeyMu.Unlock()
			return "", nil, fmt.Errorf("missing dependency task key for %s", dep)
		}
		depKeys = append(depKeys, k)
	}
	taskKeyMu.Unlock()

	taskKey, taskJSON, err := ComputeTaskKey(task, depKeys, stampCache)
	if err != nil {
		return "", nil, fmt.Errorf("compute task key: %w", err)
	}

	taskKeyMu.Lock()
	taskKeyBy[task.ID] = taskKey
	taskKeyMu.Unlock()
	return taskKey, taskJSON, nil
}

func executeTasks(taskMap TaskMap, taskIDs []TaskID) error {
	g := new(errgroup.Group)

	for _, id := range taskIDs {
		task, exists := taskMap[id]
		if !exists {
			return fmt.Errorf("task %s not found", id)
		}

		g.Go(func() error {
			return executeTask(taskMap, task)
		})
	}

	return g.Wait()
}

func executeTask(taskMap TaskMap, task Task) error {
	// Use sync.Once to ensure each task is only executed once,
	// even when multiple tasks depend on it concurrently.
	taskOnceMu.Lock()
	entry, ok := taskOnce[task.ID]
	if !ok {
		entry = &taskOnceEntry{}
		taskOnce[task.ID] = entry
	}
	taskOnceMu.Unlock()

	entry.once.Do(func() {
		entry.err = doExecuteTask(taskMap, task)
	})

	return entry.err
}

func doExecuteTask(taskMap TaskMap, task Task) error {
	// Execute dependencies in parallel.
	if len(task.Dependencies) > 0 {
		if err := executeTasks(taskMap, task.Dependencies); err != nil {
			return err
		}
	}

	taskKey, taskJSON, err := computeAndCacheKey(task)
	if err != nil {
		return fmt.Errorf("compute task key for task %s: %w", task.ID, err)
	}

	// Lookup from cache
	if task.Cache {
		hit, err := localCache.Restore(taskKey, task.Outputs)
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
		if err := localCache.Store(taskKey, taskJSON, task.Outputs); err != nil {
			return fmt.Errorf("cache store error for task %s: %w", task.ID, err)
		}
	}

	updateOutputStamps(task.Outputs)

	return nil
}
