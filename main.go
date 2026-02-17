package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
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

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "build-tool.jsonc", "path to build tool config (JSONC)")
	sandbox := flag.Bool("sandbox", false, "run tasks in a sandbox directory under .build-tool")
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		fmt.Printf("Usage: %s [-config build-tool.jsonc] build <task1> <task2> ...\n", os.Args[0])
		fmt.Printf("       %s clean\n", os.Args[0])
		return fmt.Errorf("no tasks specified")
	}

	if args[0] == "clean" {
		if err := os.RemoveAll(".build-tool"); err != nil {
			return fmt.Errorf("remove .build-tool: %w", err)
		}
		fmt.Printf("Removed .build-tool\n")
		return nil
	}

	taskMap, err := LoadTaskMapFromConfig(*configPath)
	if err != nil {
		return fmt.Errorf("load tasks from %q: %w", *configPath, err)
	}

	maxTaskIDLen := 0
	for id := range taskMap {
		if n := len(string(id)); n > maxTaskIDLen {
			maxTaskIDLen = n
		}
	}
	log := NewLogger(os.Stdout, os.Stderr, LoggerOptions{ColorEnabled: DetectColorEnabled(), PrefixWidth: maxTaskIDLen})
	log.Printf("Loaded %d tasks from %s\n", len(taskMap), *configPath)

	executor := NewTaskExecutor(".build-tool/cache", filepath.Join(".build-tool", "cache", "stamps.json"), log, *sandbox)
	defer func() {
		if err := executor.CleanupSandbox(); err != nil {
			log.Errorf("error cleaning sandbox: %v\n", err)
		}
	}()

	if err := executor.Load(); err != nil {
		return fmt.Errorf("load stamp cache: %w", err)
	}
	defer func() {
		if err := executor.Save(); err != nil {
			log.Errorf("error saving stamp cache: %v\n", err)
		}
	}()

	switch args[0] {
	case "build":
		taskIDs := make([]TaskID, len(args)-1)
		for i, arg := range args[1:] {
			taskIDs[i] = TaskID(arg)
		}

		if err := executor.ExecuteTasks(taskMap, taskIDs); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}

	return nil
}
