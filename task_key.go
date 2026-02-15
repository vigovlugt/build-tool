package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/crypto/blake2b"
)

type taskKeyInput struct {
	Path   string `json:"path"`
	Digest string `json:"digest"`
}

type taskKeyPayload struct {
	Version      int            `json:"v"`
	Command      string         `json:"command"`
	Dependencies []string       `json:"dependencies"`
	Outputs      []string       `json:"outputs"`
	Inputs       []taskKeyInput `json:"inputs"`
}

// TODO: remove JSON payload, just binary encoding
func marshalTaskPayload(p taskKeyPayload) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(p); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// ComputeTaskKey returns a content hash (CAS) of a canonical JSON
// representation of the task. When a non-nil FileStampCache is provided,
// files whose metadata has not changed since the last hash are not re-read.
func ComputeTaskKey(task Task, depTaskKeys []string, stamps *FileStampCache) (string, []byte, error) {
	depKeys := append([]string(nil), depTaskKeys...)
	sort.Strings(depKeys)

	outputSpecs := make([]string, 0, len(task.Outputs))
	for _, out := range task.Outputs {
		s := filepath.ToSlash(string(out))
		s = strings.TrimPrefix(s, "./")
		outputSpecs = append(outputSpecs, s)
	}
	sort.Strings(outputSpecs)

	expandedInputs, err := ExpandFileSpecs(task.Inputs)
	if err != nil {
		return "", nil, fmt.Errorf("expand inputs: %w", err)
	}

	inputs := append([]Path(nil), expandedInputs...)
	sort.Slice(inputs, func(i, j int) bool { return string(inputs[i]) < string(inputs[j]) })

	tInputs := make([]taskKeyInput, 0, len(inputs))
	for _, in := range inputs {
		p := filepath.FromSlash(string(in))

		// Fast path: reuse cached digest when file metadata is unchanged.
		if stamps != nil {
			if d, ok := stamps.Lookup(p); ok {
				tInputs = append(tInputs, taskKeyInput{Path: string(in), Digest: d})
				continue
			}
		}

		d, err := hashFile(p)
		if err != nil {
			return "", nil, fmt.Errorf("hash input %q: %w", in, err)
		}

		// Record the freshly computed digest in the stamp cache.
		if stamps != nil {
			stamps.Update(p, d)
		}

		tInputs = append(tInputs, taskKeyInput{Path: string(in), Digest: d})
	}

	p := taskKeyPayload{
		Version:      1,
		Command:      task.Command,
		Dependencies: depKeys,
		Outputs:      outputSpecs,
		Inputs:       tInputs,
	}

	taskJSON, err := marshalTaskPayload(p)
	if err != nil {
		return "", nil, err
	}

	sum := blake2b.Sum256(taskJSON)
	return hex.EncodeToString(sum[:]), taskJSON, nil
}

func hashFile(path string) (string, error) {
	fmt.Printf("Hashing input file %s\n", path)
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher, err := blake2b.New256(nil)
	if err != nil {
		return "", err
	}

	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}
