# Contributing to kimi-lite

Thank you for your interest in contributing! This document covers the basics of building, testing, and submitting changes.

## Build, Test, and Lint

```bash
make test          # Run all tests with race detection
make lint          # Run golangci-lint (requires v2)
make build         # Build binary
make cross-compile # Cross-compile for all platforms
make fmt           # Format code
make vet           # Run go vet
```

> **Note:** `make lint` requires **golangci-lint v2**. The `.golangci.yml` config is v2-only. Install it via:
> ```bash
> brew install golangci-lint
> # or
> go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
> ```

## Style Rules

- **Interfaces for all external dependencies** — testability, swappability
- **Context propagation everywhere** — `ctx context.Context` as first param
- **Error wrapping** — `fmt.Errorf("...: %w", err)`
- **No global state** — everything is injected
- **Constructor pattern** — `NewXxx(dep1, dep2) *Xxx`
- Table-driven tests with `t.Parallel()`
- Run `go test -race ./...` before opening a PR

## PR Expectations

- Keep changes minimal and focused
- Follow the existing Go coding style
- Do not change test logic unless the task explicitly requires it
- Ensure `go build ./...`, `go vet ./...`, and `go test -race ./...` all pass
- Update documentation (README, AGENTS.md, CHANGELOG) if your change affects user-facing behavior

## Adding a New Tool

1. Add the tool definition to `BuiltInToolExecutor.Definitions()` in `internal/core/tools.go`
2. Add execution logic in `BuiltInToolExecutor.Execute()` switch
3. Mark as read-only in `NewBuiltInToolExecutor()` if appropriate
4. Add tests in `internal/core/tools_test.go`

## Adding a New TUI Component

1. Create a package under `internal/tui/<component>/`
2. Define a `Model` struct with `Init()`, `Update()`, `View()` methods
3. Define custom message types for component events
4. Add the component to the root `Model` in `internal/tui/model.go`
5. Wire message handling in root `Update()`
6. Add tests testing `Update`/`View` logic
