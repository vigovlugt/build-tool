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

	executor := NewTaskExecutor(".build-tool/cache", filepath.Join(".build-tool", "cache", "stamps.json"))

	if err := executor.Load(); err != nil {
		return fmt.Errorf("load stamp cache: %w", err)
	}
	defer func() {
		if err := executor.Save(); err != nil {
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

		if err := executor.ExecuteTasks(taskMap, taskIDs); err != nil {
			return err
		}
	}

	return nil
}
