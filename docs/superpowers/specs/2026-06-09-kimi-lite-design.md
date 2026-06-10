# kimi-lite Design Document

## Overview
kimi-lite is a lightweight, fast, native AI coding CLI written in Go. Alternative to TypeScript-based AI CLI tools.

## Philosophy
- Single static binary, <15 MB, zero runtime dependencies
- Startup <50ms
- Native terminal control — no forced alt-screen
- Reliable cancellation via context.Context
- Cross-platform — Alpine Linux, WSL, musl
- MCP via mcp-guard proxy

## Architecture

```
cmd/kimi-lite/              # entry point
internal/
  app/                      # DI container
  tui/                      # Terminal UI (Bubble Tea)
  core/                     # Business logic
  llm/                      # LLM client
  mcp/                      # MCP client
  store/                    # SQLite persistence
  config/                   # Configuration
  git/                      # Git integration
pkg/
  api/                      # Public types
tests/
  integration/              # e2e tests
```

## Tech Stack
- Go 1.23+
- bubbletea, bubbles, lipgloss, glamour — TUI
- cobra, viper — CLI/config
- mattn/go-sqlite3 — persistence
- chi/v5 — HTTP router (future)
- log/slog — logging

## Features

### MVP
1. Chat REPL with multiline, history, streaming
2. Built-in tools: ReadFile, Glob, Grep, WriteFile, StrReplaceFile, Shell, FetchURL
3. Approval system (read-only auto, write asks; --yolo, --auto)
4. Session persistence in SQLite
5. Context management (/compact, /clear)

### v2
6. MCP integration via mcp-guard
7. Subagents (/btw)
8. Plan mode
9. Goal mode
10. Git integration

## Standards
- Interfaces for all deps
- Context propagation
- Error wrapping
- No global state
- Constructor pattern
- Table-driven tests, 75%+ coverage
- go test -race
