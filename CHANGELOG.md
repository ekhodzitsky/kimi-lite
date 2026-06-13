# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.2.1] - 2026-06-13

### Fixed

- **Process group cleanup** — `shell` and lifecycle `hooks` now kill the entire process group on timeout or cancellation, so child processes spawned by shells (e.g. `sleep`) are terminated instead of outliving the parent.

## [0.2.0] - 2026-06-13

### Added

- **Subagent dispatch** — new `dispatch_subagent` tool lets the LLM delegate focused work to built-in `coder`, `explore`, and `plan` subagents. Subagents run in an ephemeral, cancellable LLM↔tool loop with per-type tool restrictions and no session persistence.
- **Lifecycle hooks** — user-configurable commands run at `session_start`, `turn_start`, `turn_end`, `tool_call`, `tool_result`, `approval_request`, and `approval_decision`. `tool_call` hooks can gate (block) tool execution on non-zero exit.
- **ACP server** — new `kimi-lite acp` subcommand speaks JSON-RPC 2.0 over stdio and supports `initialize`, `session/new`, `session/load`, `session/prompt` (with streaming `session/update` notifications), and `session/cancel`.
- **Runtime observability** — internal `api.MetricsCollector` emits counters and latency histograms from sessions, turns, tools, and LLM calls. The new `--pprof <addr>` flag exposes a `/debug/pprof` server for live profiling, and `go.uber.org/goleak` guards against goroutine leaks in CI.
- **Risk-aware approval gates** — every tool call is scored `low`/`medium`/`high` based on its name and arguments. Paths that escape the sandbox are always high risk, and destructive tools like `shell` default to high. Configurable `permission.risk_rules` override defaults; `permission.risk_threshold` controls the auto-approve cutoff.
- **Better token estimation** — context compression now uses a pluggable `TokenEstimator` with a heuristic that counts ASCII at ~4 chars/token and non-ASCII runes at 1 token each, giving more accurate budgets for mixed-language and code-heavy conversations.
- **CI coverage gate** — new `make coverage-gate` target and GitHub Actions job enforce a 70% minimum statement coverage threshold.
- **Long-running smoke test** — new `tests/long_running_test.go` runs 25 full turn cycles against a fake LLM and verifies no goroutine leaks with `goleak`.
- **Fuzz tests** — added `FuzzHeuristicTokenEstimator` and `FuzzRiskEvaluator` to `internal/core`, plus a `make fuzz` target and CI fuzz-smoke entries.
- **Windows CI build** — added a `windows-latest` build job to GitHub Actions to catch Windows cross-compile regressions.
- **README refresh** — documented the current feature set, quality signals, and development commands.
- **Benchmark regression detection** — added `scripts/benchregression`, a `make bench-regression` target, and a CI job that fails if any benchmark is more than 20% slower than the baseline.
- **Skill discovery** — markdown files in `~/.config/kimi-lite/skills/` are automatically loaded into the system prompt; `behavior.skills` selects specific skills.
- **Video input** — new `read_video` tool extracts metadata and up to 10 base64 PNG key frames from a video file using `ffmpeg`/`ffprobe` when available. The tool is read-only and sandboxed.

### Changed

- Bumped minimum Go version to `1.26.4` and upgraded `golang.org/x/net` to `v0.53.0` to resolve `govulncheck` findings.

### Fixed

- Resolved all pre-existing `golangci-lint` issues: unchecked I/O errors, `gofmt`, `wrapcheck`, `revive`, `gosec`, and `noctx`.
- Hardened `internal/config` tests against real `~/.config/kimi-lite/config.toml` by isolating `$HOME` in environment-variable resolution tests.

## [0.1.0] - 2026-06-10

### Added

- **Core chat loop** — streaming LLM responses with native Bubble Tea TUI.
- **Built-in tools** — `read_file`, `write_file`, `str_replace_file`, `glob`, `grep`, `shell`, `fetch_url` with sandboxed file access and size guards.
- **Approval system** — configurable auto-approval for read-only tools, manual confirmation for destructive operations, `--yolo` flag.
- **Session persistence** — SQLite with WAL mode, schema migrations, resume with `--continue` or `--session`.
- **Context compression** — `/compact` command to summarize history and free context window.
- **MCP integration** — connects through `mcp-guard` proxy for stable Model Context Protocol tools.
- **Fallback LLM** — automatic failover to a secondary provider on primary failure.
- **Git integration** — auto `git status` in context, `/checkpoint` to commit changes.
- **Export / Import** — `kimi-lite export` and `kimi-lite import` for portable session snapshots.
- **Health checks** — `kimi-lite doctor` verifies config, database, LLM connectivity and MCP status.
- **Per-chunk SSE timeout** — streaming reads respect `context.Context` to recover from hung connections.
- **Graceful shutdown** — `sync.WaitGroup` with 10-second timeout for in-flight turns.
- **Security** — SSRF redirect protection, DNS rebinding guard, symlink sandbox, environment isolation, 10 MB response/tool limits.
- **Observability** — `--debug` flag, sanitized error logging, structured `slog` output.
- **Cross-platform** — static binary with `CGO_ENABLED=0`, supports macOS, Linux (glibc & musl), ARM64.

[0.2.1]: https://github.com/ekhodzitsky/kimi-lite/releases/tag/v0.2.1
[0.2.0]: https://github.com/ekhodzitsky/kimi-lite/releases/tag/v0.2.0
[0.1.0]: https://github.com/ekhodzitsky/kimi-lite/releases/tag/v0.1.0
