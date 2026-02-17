package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

type TaskExecutor struct {
	state *BuildState
	keys  *TaskKeyStore
	memo  *TaskMemo
	log   *Logger

	sandbox bool

	sandboxOnce    sync.Once
	sandboxRootDir string
	sandboxInitErr error
}

func NewTaskExecutor(cacheRoot string, stampCachePath string, log *Logger, sandbox bool) *TaskExecutor {
	return &TaskExecutor{
		state:   NewBuildState(cacheRoot, stampCachePath),
		keys:    NewTaskKeyStore(),
		memo:    NewTaskMemo(),
		log:     log,
		sandbox: sandbox,
	}
}

func (e *TaskExecutor) Load() error {
	return e.state.Load()
}

func (e *TaskExecutor) Save() error {
	return e.state.Save()
}

func (e *TaskExecutor) CleanupSandbox() error {
	if !e.sandbox {
		return nil
	}
	// Don't create a sandbox root just to delete it.
	if e.sandboxRootDir == "" {
		return nil
	}
	return os.RemoveAll(e.sandboxRootDir)
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

	return e.executeTaskRun(taskMap, task, taskKey, taskJSON, e.sandbox)
}

func (e *TaskExecutor) sandboxRoot() (string, error) {
	e.sandboxOnce.Do(func() {
		base := filepath.Join(".build-tool", "sandboxes")
		if err := os.MkdirAll(base, 0o755); err != nil {
			e.sandboxInitErr = fmt.Errorf("create sandbox base: %w", err)
			return
		}

		runDir := filepath.Join(base, fmt.Sprintf("run-%d-%d", os.Getpid(), time.Now().UnixNano()))
		if err := os.MkdirAll(runDir, 0o755); err != nil {
			e.sandboxInitErr = fmt.Errorf("create sandbox run dir: %w", err)
			return
		}
		e.sandboxRootDir = runDir
	})

	if e.sandboxInitErr != nil {
		return "", e.sandboxInitErr
	}
	return e.sandboxRootDir, nil
}

func (e *TaskExecutor) executeTaskRun(taskMap TaskMap, task Task, taskKey string, taskJSON []byte, sandbox bool) error {
	execDir := ""
	cleanup := func() {}

	if sandbox {
		root, err := e.sandboxRoot()
		if err != nil {
			return err
		}

		sandboxDir := filepath.Join(root, fmt.Sprintf("task-%s", sanitizeSandboxName(string(task.ID))))
		// Best-effort clean in case of prior partial runs.
		_ = os.RemoveAll(sandboxDir)
		if err := os.MkdirAll(sandboxDir, 0o755); err != nil {
			return fmt.Errorf("create sandbox dir: %w", err)
		}
		cleanup = func() { _ = os.RemoveAll(sandboxDir) }

		workDir := filepath.Join(sandboxDir, "work")
		if err := os.MkdirAll(workDir, 0o755); err != nil {
			cleanup()
			return fmt.Errorf("create sandbox work dir: %w", err)
		}
		execDir = workDir

		// Stage inputs.
		staged := make(map[string]string) // rel (slash) -> src path
		if len(task.Inputs) > 0 {
			ins, err := ExpandFileSpecs(task.Inputs)
			if err != nil {
				cleanup()
				return fmt.Errorf("expand inputs for task %s: %w", task.ID, err)
			}
			for _, in := range ins {
				rel := filepath.ToSlash(string(in))
				src := filepath.FromSlash(string(in))
				if _, ok := staged[rel]; ok {
					continue
				}
				staged[rel] = src
			}
		}

		// Stage direct dependency outputs.
		for _, depID := range task.Dependencies {
			depTask, ok := taskMap[depID]
			if !ok {
				cleanup()
				return fmt.Errorf("task %s depends on unknown task %s", task.ID, depID)
			}

			depOutputs, depSrcDir, err := e.depOutputsForStaging(depID, depTask)
			if err != nil {
				cleanup()
				return err
			}
			for _, out := range depOutputs {
				rel := filepath.ToSlash(string(out))
				var src string
				if depSrcDir != "" {
					src = filepath.Join(depSrcDir, filepath.FromSlash(string(out)))
				} else {
					src = filepath.FromSlash(string(out))
				}
				// Dependency outputs win over declared inputs.
				staged[rel] = src
			}
		}

		// Copy staged files into the sandbox.
		paths := make([]string, 0, len(staged))
		for rel := range staged {
			paths = append(paths, rel)
		}
		sort.Strings(paths)
		for _, rel := range paths {
			src := staged[rel]
			dst := filepath.Join(workDir, filepath.FromSlash(rel))
			if err := stageFileBySymlink(src, dst); err != nil {
				cleanup()
				return fmt.Errorf("stage %q: %w", rel, err)
			}
		}
	}
	defer cleanup()

	// Execute task.
	e.log.Taskf(task.ID, "$ %s", task.Command)

	cmd := exec.Command("sh", "-c", task.Command)
	if execDir != "" {
		cmd.Dir = execDir
	}
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

	if !sandbox {
		// Workspace mode: keep the old behavior; only cacheable tasks validate/record outputs.
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

	// Sandbox mode: expand outputs in the sandbox and export them.
	var expandedOutputs []Path
	if len(task.Outputs) > 0 {
		expandedOutputs, err = ExpandFileSpecsInDir(execDir, task.Outputs)
		if err != nil {
			return fmt.Errorf("expand outputs for task %s: %w", task.ID, err)
		}
	}

	if task.Cache {
		if err := e.state.StoreFromDir(taskKey, taskJSON, expandedOutputs, execDir); err != nil {
			return fmt.Errorf("cache store error for task %s: %w", task.ID, err)
		}
		if _, err := e.state.Restore(taskKey, task.Outputs); err != nil {
			return fmt.Errorf("cache restore after sandbox for task %s: %w", task.ID, err)
		}
	} else {
		for _, out := range expandedOutputs {
			src := filepath.Join(execDir, filepath.FromSlash(string(out)))
			dst := filepath.FromSlash(string(out))
			if err := copyFile(src, dst); err != nil {
				return fmt.Errorf("export output %q for task %s: %w", out, task.ID, err)
			}
		}
	}

	e.state.UpdateOutputStamps(expandedOutputs)
	return nil
}

// depOutputsForStaging returns the set of outputs to stage for depID.
// If srcDir is non-empty, outputs should be read from srcDir/<output>.
func (e *TaskExecutor) depOutputsForStaging(depID TaskID, depTask Task) (outs []Path, srcDir string, err error) {
	if depTask.Cache {
		depKey, ok := e.keys.Get(depID)
		if !ok {
			return nil, "", fmt.Errorf("missing dependency task key for %s", depID)
		}
		manifestOuts, err := e.state.localCache.ReadManifestOutputs(depKey)
		if err == nil {
			return manifestOuts, filepath.Join(e.state.localCache.taskDir(depKey), "outputs"), nil
		}
		// Fall back to expanding from the workspace.
	}

	if len(depTask.Outputs) == 0 {
		return nil, "", nil
	}
	wsOuts, err := ExpandFileSpecs(depTask.Outputs)
	if err != nil {
		return nil, "", fmt.Errorf("expand outputs for dependency %s: %w", depID, err)
	}
	return wsOuts, "", nil
}

func sanitizeSandboxName(s string) string {
	if s == "" {
		return "task"
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	out := b.String()
	if out == "" {
		return "task"
	}
	return out
}

func stageFileBySymlink(src, dst string) error {
	srcAbs, err := filepath.Abs(src)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	// Best-effort replace.
	_ = os.Remove(dst)

	if err := os.Symlink(srcAbs, dst); err == nil {
		return nil
	}

	// Fallback for platforms/configurations where symlinks are not available.
	return copyFile(srcAbs, dst)
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
