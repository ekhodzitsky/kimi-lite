# ADR 001: Go + Bubble Tea Architecture

## Status
Accepted

## Context
We need to build a terminal-based AI coding CLI that is fast, reliable, and native. The main alternatives are TypeScript-based tools (Kimi Code, Claude Code) which have significant drawbacks:

- Large runtime dependencies (Node.js/Bun)
- Slow startup times (500ms+)
- Forced alt-screen terminal mode
- Unreliable cancellation
- Complex cross-platform distribution

## Decision
Build kimi-lite in Go using the Bubble Tea TUI framework.

## Rationale

### Why Go?
- **Single static binary** — zero runtime dependencies
- **Fast startup** — <50ms cold start
- **Cross-compilation** — native builds for Linux, macOS, Windows from any platform
- **musl/Alpine compatible** — static linking works out of the box
- **Standard library** — robust HTTP, JSON, context, concurrency primitives
- **Cancellation** — `context.Context` is first-class and reliable

### Why Bubble Tea?
- **Idiomatic Go** — pure Go, no external runtime
- **Model-Update-View** — familiar pattern, testable
- **Composable** — child components compose into root model
- **Native terminal** — no forced alt-screen, scrollback works
- **Rich ecosystem** — bubbles (textarea, viewport, list), lipgloss (styling), glamour (Markdown)

## Consequences

### Positive
- Sub-50ms startup time
- <15 MB binary
- Reliable cancellation via context
- Works on Alpine Linux, WSL, minimal containers
- Easy distribution (single binary, Homebrew, go install)

### Negative
- TUI is terminal-only (no web UI without additional work)
- Bubble Tea learning curve for complex layouts
- Limited mouse support compared to web-based UIs
