# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

[0.1.0]: https://github.com/ekhodzitsky/kimi-lite/releases/tag/v0.1.0
