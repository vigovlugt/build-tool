package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

type LocalCache struct {
	Root string
}

func NewLocalCache(root string) *LocalCache {
	return &LocalCache{Root: root}
}

func (c *LocalCache) taskDir(taskKey string) string {
	return filepath.Join(c.Root, "tasks", taskKey)
}

func (c *LocalCache) Restore(taskKey string, outputs []Path) (bool, error) {
	tDir := c.taskDir(taskKey)

	manifestPath := filepath.Join(tDir, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}

	var manifest struct {
		TaskKey string          `json:"task_key"`
		Outputs []Path          `json:"outputs"`
		Task    json.RawMessage `json:"task"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return false, err
	}
	outputs = manifest.Outputs
	if len(outputs) == 0 {
		return false, nil
	}

	// Check all cached outputs exist before linking any, to avoid partial restores.
	for _, out := range outputs {
		src := filepath.Join(tDir, "outputs", filepath.FromSlash(string(out)))
		if _, err := os.Stat(src); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return false, nil
			}
			return false, err
		}
	}

	// Hardlink cached outputs to their expected locations. Hardlinks share
	// the same inode and metadata as the cached copy, so file stamps
	// observed by downstream tasks remain stable across restores.
	for _, out := range outputs {
		src := filepath.Join(tDir, "outputs", filepath.FromSlash(string(out)))
		dst := filepath.FromSlash(string(out))

		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return false, err
		}

		// Remove any existing file so the link can be created.
		_ = os.Remove(dst)

		if err := os.Link(src, dst); err != nil {
			return false, err
		}
	}

	return true, nil
}

func (c *LocalCache) Store(taskKey string, taskJSON []byte, outputs []Path) error {
	tDir := c.taskDir(taskKey)
	if err := os.MkdirAll(filepath.Dir(tDir), 0o755); err != nil {
		return err
	}

	tmpDir, err := os.MkdirTemp(filepath.Dir(tDir), "tmp-task-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	sortedOutputs := append([]Path(nil), outputs...)
	sort.Slice(sortedOutputs, func(i, j int) bool { return string(sortedOutputs[i]) < string(sortedOutputs[j]) })

	for _, out := range sortedOutputs {
		src := filepath.FromSlash(string(out))
		if _, err := os.Stat(src); err != nil {
			return fmt.Errorf("output %q missing: %w", out, err)
		}

		dst := filepath.Join(tmpDir, "outputs", filepath.FromSlash(string(out)))
		if err := copyFile(src, dst); err != nil {
			return err
		}
	}

	manifest := struct {
		TaskKey string          `json:"task_key"`
		Outputs []Path          `json:"outputs"`
		Task    json.RawMessage `json:"task"`
	}{
		TaskKey: taskKey,
		Outputs: sortedOutputs,
		Task:    json.RawMessage(taskJSON),
	}

	manifestPath := filepath.Join(tmpDir, "manifest.json")
	mb, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if err := os.WriteFile(manifestPath, mb, 0o644); err != nil {
		return err
	}

	// Best-effort replace.
	_ = os.RemoveAll(tDir)
	return os.Rename(tmpDir, tDir)
}

func copyFile(src, dst string) error {
	sfi, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !sfi.Mode().IsRegular() {
		return fmt.Errorf("source is not a regular file: %s", src)
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, sfi.Mode().Perm())
	if err != nil {
		return err
	}
	defer func() {
		_ = out.Close()
	}()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}

	// Preserve executable bits and other mode flags best-effort.
	_ = os.Chmod(dst, sfi.Mode())
	return nil
}
