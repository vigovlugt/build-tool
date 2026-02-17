package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/tailscale/hujson"
)

type buildConfig struct {
	Tasks map[TaskID]taskConfig `json:"tasks"`
}

type taskConfig struct {
	Inputs  []Path `json:"inputs,omitempty"`
	Outputs []Path `json:"outputs,omitempty"`
	Command string `json:"command"`
	Cache   *bool  `json:"cache,omitempty"`
}

func LoadTaskMapFromConfig(configPath string) (TaskMap, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("config file not found: %w", err)
		}
		return nil, fmt.Errorf("read config file %q: %w", configPath, err)
	}

	jsonData, err := hujson.Standardize(data)
	if err != nil {
		return nil, fmt.Errorf("standardize JSONC: %w", err)
	}

	dec := json.NewDecoder(bytes.NewReader(jsonData))
	dec.DisallowUnknownFields()

	var cfg buildConfig
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode JSON: %w", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("decode JSON: trailing data")
	}

	if cfg.Tasks == nil {
		return nil, fmt.Errorf("missing required \"tasks\" object")
	}

	taskMap := make(TaskMap, len(cfg.Tasks))
	for id, tc := range cfg.Tasks {
		if strings.TrimSpace(string(id)) == "" {
			return nil, fmt.Errorf("task id must not be empty")
		}

		cmd := strings.TrimSpace(tc.Command)
		if cmd == "" {
			return nil, fmt.Errorf("task %s: command must not be empty", id)
		}

		cache := true
		if tc.Cache != nil {
			cache = *tc.Cache
		}

		// Dependencies are declared inline in inputs by prefixing them with ':'
		// e.g. ":compile".
		inputs := make([]Path, 0, len(tc.Inputs))
		deps := make([]TaskID, 0)
		for _, in := range tc.Inputs {
			raw := string(in)
			if strings.HasPrefix(raw, "\\:") {
				// Escaped leading ':'; treat as a literal file path beginning with ':'.
				inputs = append(inputs, Path(strings.TrimPrefix(raw, "\\")))
				continue
			}
			if strings.HasPrefix(raw, ":") {
				dep := strings.TrimSpace(strings.TrimPrefix(raw, ":"))
				if dep == "" {
					return nil, fmt.Errorf("task %s: dependency input must not be empty", id)
				}
				deps = append(deps, TaskID(dep))
				continue
			}
			inputs = append(inputs, in)
		}

		taskMap[id] = Task{
			ID:           id,
			Inputs:       inputs,
			Outputs:      tc.Outputs,
			Dependencies: deps,
			Command:      cmd,
			Cache:        cache,
		}
	}

	for id, task := range taskMap {
		for _, dep := range task.Dependencies {
			if _, ok := taskMap[dep]; !ok {
				return nil, fmt.Errorf("task %s depends on unknown task %s", id, dep)
			}
		}
	}

	return taskMap, nil
}
