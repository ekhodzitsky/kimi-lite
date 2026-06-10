# kimi-lite

[![CI](https://github.com/ekhodzitsky/kimi-lite/actions/workflows/ci.yml/badge.svg)](https://github.com/ekhodzitsky/kimi-lite/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/ekhodzitsky/kimi-lite)](https://goreportcard.com/report/github.com/ekhodzitsky/kimi-lite)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

> **Native AI coding CLI in Go.** Single binary, zero runtime dependencies, <50 ms cold start. The fast alternative to 200 MB TypeScript bundles.

---

## Why kimi-lite?

| | **kimi-lite** | Kimi Code | Claude Code | aider |
|---|---|---|---|---|
| **Binary size** | ~15 MB | ~200 MB | ~200 MB | ~50 MB |
| **Cold start** | <50 ms | ~500 ms | ~500 ms | ~1 s |
| **Runtime deps** | None | Node/Bun | Node | Python + Git |
| **Native TUI** | ✅ Bubble Tea | ❌ Webview | ❌ Webview | ❌ Terminal |
| **Scrollback** | ✅ Native terminal | ❌ | ❌ | ✅ |
| **musl / Alpine** | ✅ Static binary | ❌ | ❌ | ❌ |
| **Cancellation** | ✅ Instant (`context.Context`) | Hangs | Hangs | Slow |
| **Memory** | ~20 MB | ~300 MB | ~300 MB | ~100 MB |

**kimi-lite** is built for engineers who want AI assistance *without* the bloat:

- **Go + native terminal** — no Electron, no Node, no 200 MB downloads. One static binary that runs on Alpine, WSL, macOS, and ARM64 out of the box.
- **Reliable cancellation** — every I/O path respects `context.Context`. Hit `Esc` and the operation stops *immediately*.
- **Streaming SSE with per-chunk timeouts** — hung LLM connections are detected and recovered automatically.
- **Session persistence** — SQLite with WAL mode. Resume any session with `--continue` or `--session`.
- **Sandboxed tools** — built-in `read`, `write`, `grep`, `shell`, `fetch_url` with configurable auto-approval and file-size guards.
- **MCP via proxy** — integrates with `mcp-guard` for stable, sandboxed Model Context Protocol tools.

---

## Installation

### Homebrew (macOS / Linux)

```bash
brew tap ekhodzitsky/tap
brew install kimi-lite
```

### Go Install

```bash
go install github.com/ekhodzitsky/kimi-lite/cmd/kimi-lite@latest
```

### Binary Download

Grab a pre-built release for your platform from the [Releases](https://github.com/ekhodzitsky/kimi-lite/releases) page.

### Build from Source

```bash
git clone https://github.com/ekhodzitsky/kimi-lite.git
cd kimi-lite
make build
```

---

## Quick Start

1. **Configure your API key:**

```bash
mkdir -p ~/.config/kimi-lite
cat > ~/.config/kimi-lite/config.toml << 'EOF'
[llm]
provider = "moonshot"
api_key = "YOUR_API_KEY"
model = "kimi-k2.5"
base_url = "https://api.moonshot.cn/v1"
timeout = "60s"
EOF
```

2. **Start chatting:**

```bash
kimi-lite
```

3. **Resume a session:**

```bash
kimi-lite --continue          # last session in this directory
kimi-lite --session <id>      # specific session
```

4. **Health check:**

```bash
kimi-lite doctor              # verify config, DB, LLM and MCP connectivity
```

---

## Commands

| Command | Description |
|---------|-------------|
| `/compact` | Summarize conversation history to free context |
| `/clear` | Clear current conversation (keeps session) |
| `/sessions` | List available sessions |
| `/goal <objective>` | Autonomous multi-turn execution |
| `/btw <question>` | Fork a side-channel subagent |
| `/checkpoint` | Git commit current changes |

### CLI Subcommands

```bash
kimi-lite export --session <id> --output session.json
kimi-lite import --input session.json
kimi-lite doctor
```

---

## Keybindings

| Key | Action |
|-----|--------|
| `Enter` | Send message |
| `Alt+Enter` | New line in input |
| `↑ / ↓` | Navigate input history |
| `Esc` | Cancel current operation |
| `Ctrl+C` | Quit |
| `Ctrl+B` | Toggle sidebar |
| `Ctrl+Y` | Toggle yolo mode |
| `Shift+Tab` | Toggle plan mode |

---

## Configuration

Full reference in `~/.config/kimi-lite/config.toml`:

```toml
[llm]
provider = "moonshot"
api_key = "$MOONSHOT_API_KEY"
model = "kimi-k2.5"
base_url = "https://api.moonshot.cn/v1"
timeout = "60s"

[llm.fallback]
provider = "openai"
api_key = "$OPENAI_API_KEY"
model = "gpt-4o-mini"

[behavior]
auto_approve = ["read_file", "list_directory", "grep", "glob", "fetch_url"]
shell_timeout = "30s"
max_turns = 50

[session]
db_path = "~/.local/share/kimi-lite/sessions.db"
max_history = 100

[mcp]
guard_command = "mcp-guard"
guard_config = "~/.config/mcp-guard/mcp-guard.toml"

[ui]
theme = "dark"
show_token_count = true
editor = "vim"
```

---

## Architecture

```
cmd/kimi-lite/          # Cobra CLI entry point
internal/
  app/                  # DI container & lifecycle
  config/               # TOML configuration
  core/                 # Business logic
    session.go          # Session management
    turn.go             # Turn lifecycle (input → LLM → tools → output)
    tools.go            # Sandboxed built-in executor
    approval.go         # Approval gate
    compressor.go       # Context compression (/compact)
  llm/                  # OpenAI-compatible client
    client.go           # HTTP client with retries & fallback
    streaming.go        # SSE parser with per-chunk timeouts
    models.go           # Model registry
  mcp/                  # MCP client (mcp-guard proxy)
    client.go           # JSON-RPC client
    transport.go        # stdio transport
  store/                # SQLite persistence (WAL, migrations)
    sqlite.go           # Store implementation
  tui/                  # Bubble Tea terminal UI
    model.go            # Root model
    messages/           # Message components
    input/              # Textarea input
    viewport/           # Scrollable output
    sidebar/            # File browser
    styles/             # Lipgloss themes
  git/                  # Git integration
pkg/api/                # Public types & interfaces
```

---

## Development

```bash
make test          # Run all tests with race detection
make lint          # Run golangci-lint
make build         # Build binary
make cross-compile # Cross-compile for all platforms
make fmt           # Format code
```

---

## Changelog

See [CHANGELOG.md](CHANGELOG.md).

---

## License

MIT License — see [LICENSE](LICENSE) for details.
