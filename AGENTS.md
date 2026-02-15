# Agent Instructions (build-tool)

This repository is a small Go codebase for an experimental build tool with a
local content-addressed cache and a file-stamp cache.

Cursor / Copilot rules: none found (no `.cursor/rules/`, `.cursorrules`, or
`.github/copilot-instructions.md` at time of writing).

## Quick Start

- Build binary: `go build -o build-tool .`
- Run from a project directory (requires `build-tool.jsonc`): `./build-tool build <task...>`
- Run the bundled C example (from `example/`): `go run .. build main` then `go run .. build run`
- Cache location: `.build-tool/` is created in the current working directory
- Go version: `go.mod` declares `go 1.25.5` (use a compatible toolchain)
- If your Go version differs, prefer a toolchain-aware setup (e.g. `GOTOOLCHAIN=auto`) over editing `go.mod`
- Clean caches/artifacts (when debugging): `rm -rf .build-tool` (run in the directory where the cache was created)

## Build / Lint / Test Commands

### Build

- Build all packages: `go build ./...`
- Build a predictable binary: `go build -o build-tool .`
- Install into `$GOBIN` (optional): `go install .`

### Format

- Format all packages (preferred): `go fmt ./...`
- Format a single file: `gofmt -w main.go`
- If installed, fix imports too: `goimports -w .`

### Lint / Static Checks

- Vet (baseline): `go vet ./...`
- Optional (only if installed): `staticcheck ./...`
- Optional (only if installed): `golangci-lint run`

### Modules

- Tidy go.mod/go.sum after dependency changes: `go mod tidy`
- Download module deps (useful in CI/debugging): `go mod download`

### Tests

There are currently no `*_test.go` files. When tests are added:

- Run all tests: `go test ./...`
- With race detector: `go test -race ./...`

#### Run A Single Test

- One package: `go test ./path/to/pkg -run '^TestName$'`
- All packages: `go test ./... -run '^TestName$'`
- Single subtest: `go test ./... -run '^TestName$/SubtestName$'`
- Disable test caching: `go test ./... -run '^TestName$' -count=1`
- Verbose: `go test ./... -run '^TestName$' -v`
- List tests (find exact names): `go test ./... -list .`

### Example Project (`example/`)

- Compare with make: `make -C example clean && make -C example run`
- Use this tool (run from inside `example/`): `go run .. build run`

## Code Style Guidelines (Go)

Follow standard Go conventions first (Effective Go, Go Code Review Comments),
then match existing patterns in this repo (mostly `package main`).

### Formatting

- Always use `gofmt`-formatted code (`go fmt ./...` is fine).
- Avoid manual alignment; do not wrap lines purely for length.

### Imports

- Let `gofmt`/`goimports` manage grouping and ordering.
- Avoid dot imports; avoid blank imports unless required for side effects.

### Types & APIs

- Prefer small structs and concrete types; avoid interfaces unless needed.
- Introduce named types when they add meaning (e.g. `TaskID`, `Path`).
- Keep exported surface area minimal unless you are intentionally creating a library.

### Naming

- Prefer `MixedCaps` / `mixedCaps`; keep names short but specific.
- Use initialisms consistently (`ID`, `UID`, `GID`, `JSON`, `URL`).
- Use verbs for work (`Load`, `Save`, `Restore`, `Store`, `Compute`).

### Error Handling

- Prefer returning errors over panics.
- Wrap with context using `%w` (e.g. `fmt.Errorf("load stamp cache: %w", err)`).
- Use `errors.Is` / `errors.As` when matching sentinel/typed errors.
- User-facing errors go to stderr (`fmt.Fprintf(os.Stderr, ...)`).

### Logging / Output

- Current style is `fmt.Printf` for status and `fmt.Fprintf(os.Stderr, ...)` for errors.
- Avoid very chatty logging in hot paths unless behind a flag.

### Concurrency

- Protect shared maps/state with a mutex; keep lock scope small.
- Do not hold locks while doing I/O, hashing, or running external commands.
- Prefer `errgroup.Group` for parallel work with error propagation.

### File I/O & Paths

- Use `filepath.Join` / `filepath.FromSlash` for portability.
- Use `0o`-prefixed octal modes (e.g. `0o755`, `0o644`).
- Prefer atomic-ish writes for caches (write temp dir/file, then rename).

### External Commands

- Prefer `exec.Command(...).CombinedOutput()` when output is needed for debugging.
- Use `exec.CommandContext` if adding cancellation/timeouts.
- Never build shell command strings from untrusted input.

## Repo Conventions

- Cache directories live under `.build-tool/` in the current working directory.
- Stamp cache path: `.build-tool/cache/stamps.json`.
- Task cache layout: `.build-tool/cache/tasks/<taskKey>/...`.
- The bundled C example expects to be run from `example/` (tasks use relative paths).

## Gotchas / Debugging Notes

- Tasks are executed via `sh -c <command>` in `main.go`; on Windows this may require a POSIX shell.
- Cache restores hardlink outputs; if you move workspaces across filesystems, hardlinking may fail.
- The file-stamp logic is platform-specific (see `stamp_stat_unix.go` vs `stamp_stat_windows.go`).

## Adding New Tests

- Put tests next to code as `*_test.go`; prefer table-driven tests.
- Keep tests deterministic; avoid filesystem/time dependence unless that is what you are testing.

## Review Checklist

- `go fmt ./...` is clean and imports are gofmt/goimports-ordered
- `go vet ./...` and `go test ./...` succeed
- Errors are wrapped with context using `%w` and printed to stderr when user-facing
- Locks are not held across I/O/hashing/external command execution
- Cache writes/restores avoid partial state (prefer temp + rename / preflight checks)

## Updating This File

- If you add CI, linters, or new agent rules, update this document accordingly.
