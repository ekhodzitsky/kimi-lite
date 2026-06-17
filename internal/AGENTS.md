# Internal Package Guide

> Scoped rules for `internal/`. For language-level Go conventions and the
> verification checklist, see the root `AGENTS.md`.
>
> **Version:** 2.0
> **Last updated:** 2026-06-17

## Scope

This file covers `internal/*` and `pkg/api`. For public API contract rules,
see `pkg/api/AGENTS.md`.

## Package Map

| Package | Responsibility |
|---|---|
| `app` | DI container and application lifecycle: `New(cfg)`, `Run(ctx, session)`, `Close()`, CLI flag helpers (`SetYolo`, `SetAutoApprove`), system prompt building. |
| `config` | TOML/env/flag configuration loading, validation, defaults, and config-dir helpers. |
| `core` | Business logic: sessions, turns, tools, approval, risk, context compression, language/status helpers. See `internal/core/AGENTS.md`. |
| `git` | Git integration via `git` CLI: status, diff, repo detection, checkpoint commits. |
| `idgen` | Shared ID generation. |
| `llm` | OpenAI-compatible LLM client with non-streaming and SSE streaming chat, retry logic, and context cancellation. |
| `mcp` | MCP client supporting stdio, HTTP, and SSE transports. See `internal/mcp/AGENTS.md`. |
| `netutil` | SSRF-hardened HTTP clients and network helpers. |
| `observability` | In-memory metrics collection and profiling helpers. |
| `store` | SQLite persistence via `modernc.org/sqlite` (CGO-free), embedded migrations, message/turn/session CRUD, compaction. |
| `tui` | Bubble Tea terminal UI. See `internal/tui/AGENTS.md`. |

## `pkg/api`

Public types and interfaces used across all packages. This is the contract
layer. Read `pkg/api/AGENTS.md` before changing exported identifiers.

Key interfaces:

- `LLMClient` — Chat, ChatStream, Models.
- `Store` — Session/message/turn persistence.
- `ToolExecutor` — Execute, Definitions, IsReadOnly.
- `ApprovalGate` — ShouldAutoApprove.
- `MCPClient` — Connect, ListTools, CallTool.
- `MCPServerConfig` — Direct MCP server configuration (stdio, http, sse).
- `GitProvider` — Status, Diff, IsRepo, Commit.
- `WebSearcher` — Search.
- `TokenEstimator` — Estimate.
- `MetricsCollector` / `HookRunner` — observability and lifecycle hooks.
- `TurnEventStatus` — transient status message event for the TUI.
- `TurnEventToolProgress` — live output chunk from a running tool call.
- `ToolProgressCallback` — context callback used by tools to stream live output.
- `ContentPart` / `ImageURL` / `ImageData` — multimodal message content parts.
- `TurnManager` — turn orchestration; recent additions include plan mode,
  mid-stream steering, and multimodal content-parts entry points.

## Package Details

### `internal/app`

- `New(cfg)` wires all dependencies.
- `Run(ctx, session)` starts the TUI program.
- `Close()` performs graceful shutdown.
- `SetYolo()` / `SetAutoApprove()` apply CLI flags.
- `systemPrompt()` builds the agentic system prompt, including a compact
  workspace tree and appended skill context.
- `buildWorkspaceTree()` generates the workspace tree shown in the system prompt.

### `internal/config`

- `DefaultConfig()` returns sensible defaults.
- `Loader` loads config via viper, with `SetConfigFile` support.
- `Validate(cfg)` validates timeouts, URLs, and paths.
- `EnsureConfigDir()` / `WriteDefaultConfig()` are setup helpers.
- `resolveEnvVar` supports empty-but-set env vars via `os.LookupEnv`.

### `internal/store`

- `NewSQLite(dbPath)` opens the DB and runs migrations.
- Implements `api.Store`.
- Uses `database/sql` + `modernc.org/sqlite` (pure-Go, no CGO).
- Pagination (`LIMIT`) on `GetMessages`, `GetTurns`, `ListSessions`.
- `ListAllSessions` for the cross-directory sessions picker.
- Persists multi-modal `Message.ContentParts` via a JSON column.
- Transactional `ReplaceMessages` for atomic compaction.

### `internal/llm`

- `NewClient(cfg, httpClient)` creates the client.
- `Chat()` and `ChatStream()` for non-streaming and streaming requests.
- `SetAttachmentRoots([]string)` restricts local file paths that may be inlined
  as base64 data URLs to the configured roots; files above 10 MB are not read.
- Exponential backoff retries, including 429 rate limits.
- Context cancellation respected.
- Bare-client fallback when no custom `httpClient` is provided.

### `internal/git`

- `NewProvider(dir)` creates a provider for the directory.
- `Status()`, `Diff(path)`, `IsRepo()`.
- `Commit(ctx, message)` creates a checkpoint commit with `--no-verify` and a
  local identity.

### `internal/observability`

- `NewCollector()` creates an in-memory metrics collector.
- `IncCounter`, `RecordLatency`, `RecordError`.
- Used by `TurnManager` and the `--pprof` server.

### `internal/netutil`

- DNS rebinding protection via custom `DialContext`.
- `SecureHTTPClient()` and `SecureTransport()` are used by `fetch_url` and MCP
  HTTP/SSE transports.

### `internal/idgen`

- Shared ID generation utilities.
