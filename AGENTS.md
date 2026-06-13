# AGENTS.md — kimi-lite

## Project Overview

kimi-lite is a production-ready open-source AI coding CLI written in Go. It is a lightweight, fast, native alternative to TypeScript-based AI CLI tools (Kimi Code, Claude Code).

## Architecture

The project follows clean architecture with clear separation of concerns:

```
cmd/kimi-lite/          # CLI entry point (cobra)
internal/
  app/                  # Application layer, DI container
  config/               # Configuration loading (TOML + viper)
  core/                 # Business logic (sessions, turns, tools, approval)
  idgen/                # Shared ID generation
  llm/                  # LLM client (OpenAI-compatible API)
  mcp/                  # MCP client (JSON-RPC over stdio to mcp-guard)
  store/                # SQLite persistence (pure-Go, CGO-free)
  tui/                  # Terminal UI (Bubble Tea)
  git/                  # Git integration
pkg/api/                # Public types and interfaces
```

## Package Responsibilities

### `pkg/api`
Public types and interfaces used across all packages. **This is the contract layer.**

Key interfaces:
- `LLMClient` — Chat, ChatStream, Models
- `Store` — Session/message/turn persistence
- `ToolExecutor` — Execute, Definitions
- `ApprovalGate` — ShouldAutoApprove
- `MCPClient` — Connect, ListTools, CallTool
- `GitProvider` — Status, Diff, IsRepo

### `internal/config`
Configuration loading from TOML files, environment variables, and CLI flags.

- `DefaultConfig()` — returns sensible defaults
- `Loader` — viper-based config loading with `SetConfigFile` support
- `Validate(cfg)` — validates all config fields (timeouts, URLs, paths)
- `EnsureConfigDir()` / `WriteDefaultConfig()` — setup helpers
- `resolveEnvVar` — supports empty-but-set env vars via `os.LookupEnv`

### `internal/store`
SQLite persistence with embedded migrations.

- `NewSQLite(dbPath)` — opens DB, runs migrations
- Implements `api.Store` interface
- Uses `database/sql` + `modernc.org/sqlite` (pure-Go, no CGO)
- Pagination (`LIMIT`) on `GetMessages`, `GetTurns`, `ListSessions`
- Transactional `ReplaceMessages` for atomic compaction

### `internal/llm`
OpenAI-compatible LLM client with SSE streaming.

- `NewClient(cfg, httpClient)` — creates client
- `Chat()` — non-streaming request
- `ChatStream()` — returns `<-chan api.StreamChunk`
- Retry logic with exponential backoff (including 429 rate limits)
- Context cancellation respected
- Bare-client fallback when no custom httpClient is provided

### `internal/core`
Business logic layer.

- `SessionManager` — create, resume, list sessions
- `TurnManager` — orchestrates input → LLM → tools → output
- `BuiltInToolExecutor` — 8 built-in tools (`read_file`, `write_file`, `str_replace_file`, `glob`, `grep`, `shell`, `fetch_url`, `list_directory`) with sandboxed file access
  - Uses `os.OpenRoot` when `SandboxRoot` is configured; falls back to `O_NOFOLLOW` (`openFileNoFollow`) when no sandbox root is set
  - Blocks protected paths and sensitive system/secret trees
  - Performs hardlink-escape checks on sandboxed reads
  - `NewBuiltInToolExecutor` returns `(*BuiltInToolExecutor, error)` and fails if the sandbox root cannot be opened
  - `ValidateFilePath` is an exported helper used by the TUI diff preview
- `CompositeToolExecutor` — routes tool calls across multiple executors
- `ApprovalGate` — auto/manual/yolo approval modes
- `ContextCompressor` — summarizes conversation history via LLM while preserving leading system/identity prompts verbatim and using pair-aware boundaries so assistant/tool-call groups are not split across the summary/recent boundary
- DNS rebinding protection via custom `DialContext` in `newSecureHTTPClient` (used by `fetch_url`)

### `internal/tui`
Bubble Tea terminal UI.

- `Model` — root model composing child components
- `input` — multi-line textarea with history
- `viewport` — scrollable output
- `sidebar` — file browser
- `messages` — message rendering (Markdown via Glamour)
- `styles` — Lipgloss themes

### `internal/mcp`
MCP client connecting to mcp-guard.

- `NewClientFromConfig(cfg)` — creates client with stdio transport
- `Connect()` — performs MCP initialize handshake
- `ListTools()` / `CallTool()` — tool operations
- Graceful degradation if mcp-guard not found

### `internal/git`
Git integration via shelling out to `git`.

- `NewProvider(dir)` — creates provider for directory
- `Status()` — `git status` output
- `Diff(path)` — file diff
- `IsRepo()` — checks for `.git`

### `internal/app`
DI container and application lifecycle.

- `New(cfg)` — wires all dependencies
- `Run(ctx, session)` — starts TUI program
- `Close()` — graceful shutdown
- `SetYolo()` / `SetAutoApprove()` — CLI flag application

## Code Style

### Idiomatic Go
- **Interfaces for all external dependencies** — testability, swappability
- **Context propagation everywhere** — `ctx context.Context` as first param
- **Error wrapping** — `fmt.Errorf("...: %w", err)`
- **No global state** — everything is injected
- **Constructor pattern** — `NewXxx(dep1, dep2) *Xxx`

### TUI Architecture
- Strict Bubble Tea Model-Update-View pattern
- Root model composes child models
- Each component is a separate Bubble Tea model
- Messages flow through `tea.Msg` interface

### Testing
- Table-driven tests with `t.Parallel()`
- Interface mocking for unit tests
- Race detector: `go test -race ./...`
- Coverage is reported in CI; keep core/llm/store/tui well covered
- Known coverage gaps: internal/app, internal/idgen, pkg/api, cmd/kimi-lite

## Common Commands

```bash
# Run all tests with race detector
make test

# Run linter (requires golangci-lint v2; install via `brew install golangci-lint` or `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest`)
make lint

# Build binary
make build

# Cross-compile for all platforms
make cross-compile

# Format and vet
make fmt vet
```

## Adding a New Tool

1. Add tool definition to `BuiltInToolExecutor.Definitions()` in `internal/core/tools.go`
2. Add execution logic in `BuiltInToolExecutor.Execute()` switch
3. Mark as read-only in `NewBuiltInToolExecutor()` if appropriate
4. Add tests in `internal/core/tools_test.go`

## Adding a New TUI Component

1. Create package under `internal/tui/<component>/`
2. Define Model struct with `Init()`, `Update()`, `View()` methods
3. Define custom message types for component events
4. Add component to root `Model` in `internal/tui/model.go`
5. Wire message handling in root `Update()`
6. Add tests testing Update/View logic

## MCP Integration

kimi-lite connects to mcp-guard via stdio JSON-RPC:
1. Attempts to find `mcp-guard` in PATH
2. If found, starts subprocess and connects
3. If not found, runs with built-in tools only
4. Uses same TOML config format as mcp-guard

## CI/CD

- **GitHub Actions**: test on ubuntu + macos with `go test -race`, lint with golangci-lint (config verify + full run), plus gates for `gofmt`, `go mod tidy`, and `govulncheck`
- **GoReleaser**: cross-compilation for linux/darwin amd64/arm64 with SBOM generation and artifact signing
- **Dependabot**: weekly updates for Go modules and GitHub Actions
