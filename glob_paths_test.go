package main

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

var chdirMu sync.Mutex

func withTempWD(t *testing.T, fn func()) {
	t.Helper()

	dir := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}

	chdirMu.Lock()
	defer chdirMu.Unlock()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir temp: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(old)
	})

	fn()
}

func writeFile(t *testing.T, rel string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
		t.Fatalf("MkdirAll %q: %v", rel, err)
	}
	if err := os.WriteFile(rel, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile %q: %v", rel, err)
	}
}

func TestExpandFileSpecs(t *testing.T) {
	withTempWD(t, func() {
		writeFile(t, "a.txt")
		writeFile(t, "b.md")
		writeFile(t, "dir/c.txt")
		writeFile(t, "node_modules/nm.txt")
		writeFile(t, "node_modules/sub/nms.txt")
		writeFile(t, "!keep.txt")
		if err := os.MkdirAll("emptydir/sub", 0o755); err != nil {
			t.Fatalf("MkdirAll emptydir: %v", err)
		}

		tests := []struct {
			name    string
			specs   []Path
			want    []Path
			wantErr bool
		}{
			{
				name:  "single-file",
				specs: []Path{"a.txt"},
				want:  []Path{"a.txt"},
			},
			{
				name:  "glob-star",
				specs: []Path{"*.txt"},
				want:  []Path{"!keep.txt", "a.txt"},
			},
			{
				name:  "glob-doublestar",
				specs: []Path{"**/*.txt"},
				want:  []Path{"!keep.txt", "a.txt", "dir/c.txt", "node_modules/nm.txt", "node_modules/sub/nms.txt"},
			},
			{
				name:  "include-all-exclude-node-modules-doublestar",
				specs: []Path{"**/*", "!node_modules/**"},
				want:  []Path{"!keep.txt", "a.txt", "b.md", "dir/c.txt"},
			},
			{
				name:  "include-all-exclude-node-modules-directory",
				specs: []Path{"**/*", "!node_modules"},
				want:  []Path{"!keep.txt", "a.txt", "b.md", "dir/c.txt"},
			},
			{
				name:  "exclude-only-ok",
				specs: []Path{"!node_modules/**"},
				want:  nil,
			},
			{
				name:  "negated-file-removes",
				specs: []Path{"a.txt", "!a.txt"},
				want:  nil,
			},
			{
				name:  "nonexistent-exclude-ok",
				specs: []Path{"a.txt", "!missing.txt"},
				want:  []Path{"a.txt"},
			},
			{
				name:    "missing-non-glob-errors",
				specs:   []Path{"missing.txt"},
				wantErr: true,
			},
			{
				name:    "glob-no-matches-errors",
				specs:   []Path{"nope*.txt"},
				wantErr: true,
			},
			{
				name:    "glob-matches-only-directories-errors",
				specs:   []Path{"emptydir/**"},
				wantErr: true,
			},
			{
				name:    "dot-directory-errors",
				specs:   []Path{"."},
				wantErr: true,
			},
			{
				name:  "escape-leading-bang",
				specs: []Path{"\\!keep.txt"},
				want:  []Path{"!keep.txt"},
			},
			{
				name:  "dedupe-and-sort",
				specs: []Path{"dir/**/*.txt", "dir/c.txt"},
				want:  []Path{"dir/c.txt"},
			},
			{
				name:  "exclude-glob-removes-included-file",
				specs: []Path{"**/*", "!**/*.md"},
				want:  []Path{"!keep.txt", "a.txt", "dir/c.txt", "node_modules/nm.txt", "node_modules/sub/nms.txt"},
			},
			{
				name:  "negated-pattern-with-dot-slash-normalizes",
				specs: []Path{"**/*", "!./node_modules/**"},
				want:  []Path{"!keep.txt", "a.txt", "b.md", "dir/c.txt"},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, err := ExpandFileSpecs(tt.specs)
				if tt.wantErr {
					if err == nil {
						t.Fatalf("expected error, got nil (got=%v)", got)
					}
					return
				}
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(got) != len(tt.want) {
					t.Fatalf("len mismatch: got %d want %d; got=%v want=%v", len(got), len(tt.want), got, tt.want)
				}
				for i := range got {
					if got[i] != tt.want[i] {
						t.Fatalf("mismatch at %d: got %q want %q; got=%v want=%v", i, got[i], tt.want[i], got, tt.want)
					}
				}
			})
		}
	})
}
