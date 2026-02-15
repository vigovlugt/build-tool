package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

func hasGlobMeta(s string) bool {
	// Keep this intentionally small. We only treat the common glob metacharacters
	// as special.
	return strings.ContainsAny(s, "*?[")
}

func parseSpec(raw string) (pat string, neg bool, err error) {
	if raw == "" {
		return "", false, fmt.Errorf("path must not be empty")
	}

	// Turbo-style negation: a leading '!' means the pattern is excluded.
	// To match a literal leading '!', escape it as "\\!foo" in JSON.
	if strings.HasPrefix(raw, "\\!") {
		raw = strings.TrimPrefix(raw, "\\")
	} else if strings.HasPrefix(raw, "!") {
		neg = true
		raw = strings.TrimPrefix(raw, "!")
		if raw == "" {
			return "", false, fmt.Errorf("negated pattern must not be empty")
		}
	}

	pat = filepath.ToSlash(raw)
	pat = strings.TrimPrefix(pat, "./")
	if pat == "" {
		return "", false, fmt.Errorf("path must not be empty")
	}
	return pat, neg, nil
}

// ExpandFileSpecs expands any glob patterns in specs (including doublestar **)
// into a sorted, de-duplicated list of slash-separated relative file paths.
//
// Non-glob entries are passed through (also normalized to slash separators).
// Glob patterns must be relative to the current working directory.
func ExpandFileSpecs(specs []Path) ([]Path, error) {
	fsys := os.DirFS(".")

	seen := make(map[string]struct{})

	for _, spec := range specs {
		raw := string(spec)
		pat, neg, err := parseSpec(raw)
		if err != nil {
			return nil, err
		}

		// Only glob relative patterns (matches Go's existing behavior where paths
		// are interpreted relative to the current working directory).
		if hasGlobMeta(pat) {
			if filepath.IsAbs(filepath.FromSlash(pat)) {
				return nil, fmt.Errorf("glob pattern must be relative: %q", raw)
			}

			matches, err := doublestar.Glob(fsys, pat)
			if err != nil {
				return nil, fmt.Errorf("glob %q: %w", raw, err)
			}

			sort.Strings(matches)
			added := 0
			for _, m := range matches {
				m = filepath.ToSlash(m)
				m = strings.TrimPrefix(m, "./")
				if m == "" {
					continue
				}

				if neg {
					delete(seen, m)
					continue
				}

				if _, ok := seen[m]; ok {
					continue
				}
				info, err := fs.Stat(fsys, m)
				if err != nil {
					return nil, fmt.Errorf("stat %q (from %q): %w", m, raw, err)
				}
				if info.IsDir() {
					continue
				}
				if !info.Mode().IsRegular() {
					return nil, fmt.Errorf("glob %q matched non-regular path %q", raw, m)
				}

				seen[m] = struct{}{}
				added++
			}
			if !neg && added == 0 {
				return nil, fmt.Errorf("glob %q matched no files", raw)
			}
			continue
		}

		// Non-glob path.
		p := pat
		if neg {
			fi, err := os.Stat(filepath.FromSlash(p))
			if err == nil && fi.IsDir() {
				prefix := strings.TrimSuffix(p, "/") + "/"
				for k := range seen {
					if strings.HasPrefix(k, prefix) {
						delete(seen, k)
					}
				}
				continue
			}
			delete(seen, p)
			continue
		}

		info, err := os.Stat(filepath.FromSlash(p))
		if err != nil {
			return nil, fmt.Errorf("stat %q: %w", raw, err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("path %q is a directory; use a glob like %q", raw, filepath.ToSlash(filepath.Join(p, "**", "*")))
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("path %q is not a regular file", raw)
		}

		seen[p] = struct{}{}
	}

	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]Path, len(keys))
	for i, p := range keys {
		out[i] = Path(p)
	}
	return out, nil
}
