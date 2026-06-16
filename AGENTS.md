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

Key interfaces and types:
- `LLMClient` — Chat, ChatStream, Models
- `Store` — Session/message/turn persistence
- `ToolExecutor` — Execute, Definitions
- `ApprovalGate` — ShouldAutoApprove
- `MCPClient` — Connect, ListTools, CallTool
- `MCPServerConfig` — direct MCP server configuration (stdio, http, and sse transports)
- `GitProvider` — Status, Diff, IsRepo
- `WebSearcher` — Search

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
- `ListAllSessions` for the cross-directory sessions picker
- Persists multi-modal `Message.ContentParts` via a dedicated `content_parts` JSON column
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

- `SessionManager` — create, resume, list sessions; recovers interrupted tool calls by synthesizing missing tool-result messages on resume
- `TurnManager` — orchestrates input → LLM → tools → output; preserves multi-modal `ToolResult.ContentParts` on tool-result messages
- `BuiltInToolExecutor` — 12 built-in tools (`read_file`, `write_file`, `str_replace_file`, `edit`, `glob`, `grep`, `shell`, `fetch_url`, `list_directory`, `web_search`, `read_video`, `TodoList`) with sandboxed file access; `web_search` is only registered when an `api.WebSearcher` provider is injected
  - Uses `os.OpenRoot` when `SandboxRoot` is configured; falls back to `O_NOFOLLOW` (`openFileNoFollow`) when no sandbox root is set
  - Blocks protected paths and sensitive system/secret trees
  - Performs hardlink-escape checks on sandboxed reads
  - `NewBuiltInToolExecutor` returns `(*BuiltInToolExecutor, error)` and fails if the sandbox root cannot be opened
  - `ValidateFilePath` is an exported helper used by the TUI diff preview
- `CompositeToolExecutor` — routes tool calls across multiple executors
- `ApprovalGate` — auto/manual/yolo approval modes; the TUI approval dialog supports an in-memory diff preview (`d`) for file-edit tools
- `ContextCompressor` — summarizes conversation history via LLM while preserving leading system/identity prompts verbatim and using pair-aware boundaries so assistant/tool-call groups are not split across the summary/recent boundary
- DNS rebinding protection via custom `DialContext` in `netutil.SecureHTTPClient`/`netutil.SecureTransport` (used by `fetch_url` and MCP HTTP transports)

### `internal/tui`
Bubble Tea terminal UI.

- `Model` — root model composing child components
- `input` — multi-line textarea with history; `ctrl+g` opens the current buffer in the external editor (`ui.editor`, `$VISUAL`, `$EDITOR`, or `vi`)
- `viewport` — scrollable output
- `sidebar` — file browser
- `messages` — message rendering (Markdown via Glamour)
- `sessions` — modal session picker with search, pagination, and current/all-directory toggle
- `styles` — Lipgloss themes

### `internal/mcp`
MCP client implementation supporting the legacy `mcp-guard` stdio path, direct
 per-server configuration, and the legacy SSE transport.

- `NewClient(transport)` — creates a client from any `Transport`
- `NewClientFromConfig(cfg)` — legacy stdio client connected to `mcp-guard`
- `NewClientFromServerConfig(cfg, httpClient)` — direct stdio, http, or sse client from `api.MCPServerConfig`
- `NewHTTPTransport(url, headers, bearerEnv, httpClient)` — JSON-RPC over HTTP POST
- `NewSSETransport(url, headers, bearerEnv, httpClient)` — JSON-RPC over Server-Sent Events
- `NewMultiClient(clients, configs)` — aggregates multiple MCP clients, disambiguates duplicate tool names by server key, and routes tool calls
- `Connect()` — performs MCP initialize handshake
- `ListTools()` / `CallTool()` — tool operations
- Graceful degradation if mcp-guard or a configured server is unavailable

### `internal/git`
Git integration via shelling out to `git`.

- `NewProvider(dir)` — creates provider for directory
- `Status()` — `git status` output
- `Diff(path)` — file diff
- `IsRepo()` — checks for `.git`
- `Commit(ctx, message)` — creates a checkpoint commit with `--no-verify` and a local identity

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

kimi-lite supports three MCP integration modes:

1. **Legacy mcp-guard path** (used when `cfg.MCPServers` is empty):
   - Attempts to find `mcp-guard` in PATH
   - If found, starts the subprocess and connects via stdio JSON-RPC
   - If not found, runs with built-in tools only
   - Uses the same TOML config format as mcp-guard

2. **Direct MCP server configuration** (used when `cfg.MCPServers` is populated):
   - Each server is configured via `[mcp_servers.<name>]` tables in `config.toml`
   - Supported transports: `stdio` (`command`, `args`, `env`, `cwd`), `http` (`url`, `headers`, `bearer_token_env_var`), and `sse` (`url`, `headers`, `bearer_token_env_var`)
   - Per-server `enabled`, `startup_timeout_ms`, `tool_timeout_ms`, `enabled_tools`, and `disabled_tools`
   - HTTP and SSE transports use `netutil.SecureHTTPClient()` for SSRF-hardened outbound requests
   - Multiple servers are aggregated by `mcp.MultiClient`; duplicate tool names are prefixed with the server key
   - Unavailable servers are logged and skipped; the app continues with the remaining servers

## Branch and Commit Conventions

Branches and commits must read like a human maintainer wrote them. See ADR-005.

- **Branch names** are descriptive and ID-free, kebab-case:
  - `fix-shell-working-directory`
  - `add-non-interactive-prompt-mode`
  - `improve-read-file-pagination`
- **Commit messages** follow Conventional Commits, stay under 72 characters, and do not contain task IDs:
  ```
  feat: add non-interactive prompt mode

  Add -p/--prompt flag that runs a single user message through the
  agent loop and prints the final response.
  ```
- Internal task identifiers live in the issue tracker, not in branch names or commit subjects.

## CI/CD

- **GitHub Actions**: test on ubuntu + macos with `go test -race`, lint with golangci-lint (config verify + full run), plus gates for `gofmt`, `go mod tidy`, and `govulncheck`
- **GoReleaser**: cross-compilation for linux/darwin amd64/arm64 with SBOM generation and artifact signing
- **Dependabot**: weekly updates for Go modules and GitHub Actions
