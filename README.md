# kimi-lite

[![CI](https://github.com/ekhodzitsky/kimi-lite/actions/workflows/ci.yml/badge.svg)](https://github.com/ekhodzitsky/kimi-lite/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Go port of the [original TypeScript AI coding client](https://github.com/MoonshotAI/kimi-code).

A single-terminal AI assistant with no Node, Python runtime, or Electron dependencies.

## Features

- **Streaming TUI chat** — native Bubble Tea interface with multi-line input, history, `@`-mention file completion, slash commands, message queueing during streaming, and a two-line footer showing model, git, context, and clock.
- **Transcript search** — `Ctrl+F` to search the conversation history with inline match highlighting and navigation.
- **Quick shell overlay** — `Ctrl+X` to run ad-hoc shell commands without leaving the chat.
- **Built-in tools** — `read_file`, `write_file`, `str_replace_file`, `edit`, `glob`, `grep`, `shell`, `fetch_url`, `list_directory`, `web_search`, `read_video`, `dispatch_subagent`, and `TodoList` with sandboxed file access.
- **Subagents** — delegate focused work to `coder`, `explore`, and `plan` subagents via `dispatch_subagent`.
- **Lifecycle hooks** — run local commands at `session_start`, `turn_start`, `turn_end`, `tool_call`, `tool_result`, `approval_request`, and `approval_decision`.
- **ACP server** — `kimi-lite acp` speaks JSON-RPC 2.0 over stdio for external agent integration.
- **Risk-aware approvals** — every tool call is scored `low`/`medium`/`high`; destructive or sandbox-escaping operations require approval.
- **Context compression** — `/compact` summarizes older history using a language-aware token estimator.
- **Observability** — `--pprof` runtime profiling and internal metrics collection.
- **MCP support** — connect to Model Context Protocol servers via stdio, HTTP, or legacy SSE.
- **All-sessions picker** — `/sessions` lists and resumes sessions across all directories, with search and pagination.
- **Multi-modal messages** — image content parts are persisted, forwarded to vision-capable models, and rendered inline in terminals that support iTerm2, Kitty, or Sixel graphics protocols.
- **Language-aware UI** — transient status messages before long-running tools match the user's language; reasoning is instructed to stay in the same language.
- **Workspace prompt** — the system prompt includes a compact workspace tree with hidden directories collapsed.

## Installation

```bash
go install github.com/ekhodzitsky/kimi-lite/cmd/kimi-lite@latest
```

Or build from source:

```bash
git clone https://github.com/ekhodzitsky/kimi-lite.git
cd kimi-lite
make build
```

## Configuration

Create `~/.config/kimi-lite/config.toml`:

```toml
[llm]
provider = "moonshot"
api_key = "YOUR_API_KEY"
model = "kimi-k2.5"
base_url = "https://api.moonshot.cn/v1"
timeout = "60s"

[permission]
risk_threshold = "medium"

[[permission.risk_rules]]
tool = "shell"
level = "high"
message = "shell commands always require approval"

[behavior]
# Load specific skills by name, or leave empty to load every .md file in ~/.config/kimi-lite/skills/.
skills = ["go", "python"]

[ui]
# Built-in "dark" or "light", or a custom JSON theme file name from ~/.config/kimi-lite/themes/.
theme = "dark"
```

### Kimi K2.7 Code or Kimi Code subscription

For the latest **Kimi K2.7 Code** via the Moonshot API, use the open-platform endpoint:

```toml
[llm]
provider = "moonshot"
api_key = "YOUR_MOONSHOT_API_KEY"
model = "kimi-k2.7-code"
base_url = "https://api.moonshot.ai/v1"
```

If you have a **Kimi Code subscription**, point at the membership endpoint with the stable `kimi-for-coding` model ID (the backend routes it to the latest coding model):

```toml
[llm]
provider = "moonshot"
api_key = "YOUR_KIMI_CODE_API_KEY"
model = "kimi-for-coding"
base_url = "https://api.kimi.com/coding/v1"
```

Drop skill files into `~/.config/kimi-lite/skills/` (e.g. `go.md`, `python.md`); their contents are appended to the system prompt.

Custom themes are JSON files placed in `~/.config/kimi-lite/themes/`; set `ui.theme` to the file name (without `.json`) to use them. A theme defines colors such as `background`, `foreground`, `primary`, `success`, `error`, and `highlight`.

## Usage

```bash
kimi-lite                        # start a new session
kimi-lite --continue             # resume the last session
kimi-lite --session <id>         # resume a specific session
kimi-lite --pprof localhost:6060 # expose runtime profiling endpoints
kimi-lite acp                    # start an ACP server over stdio
```

Inside the chat:

| Command | Description |
|---------|-------------|
| `/compact` | Summarize conversation history |
| `/clear` | Clear current conversation |
| `/sessions` or `/resume` | List and resume sessions |
| `/title <name>` | Rename the current session |
| `/fork [name]` | Fork the current session |
| `/checkpoint` | Commit the current workspace changes |
| `/diff [args]` | Show git diff |
| `/mcp` | List connected MCP tools |
| `/export [path]` | Export the current session to JSON |
| `/import <path>` | Import a session JSON snapshot |
| `/model <name>` | Switch the active model |
| `/goal <text>` | Set a short-term goal for the session |
| `/btw <text>` | Queue a note for the next message |
| `/version` | Show the build version |
| `/help` | Show keybindings and commands |

Keyboard shortcuts:

| Key | Action |
|-----|--------|
| `Ctrl+F` | Search transcript |
| `Ctrl+X` | Quick shell overlay |
| `Ctrl+S` | Steer a streaming response |
| `Shift+Tab` | Toggle plan mode |
| `Tab` | Switch focus between input and viewport |
| `?` | Toggle help overlay |

The agent can also dispatch focused subagents (`coder`, `explore`, `plan`) via the `dispatch_subagent` tool for parallel, isolated work, and run local lifecycle hooks at key events such as tool calls and approvals. Context compression (`/compact`) uses a language-aware token estimator to keep long conversations within the model's context window.

## Quality

- Tests run with `-race` on Ubuntu and macOS.
- Fuzz tests cover hot paths such as token estimation, path validation, and HTTP chunk parsing.
- `goleak` verifies no goroutine leaks after long-running smoke tests.
- `make coverage-gate` enforces 70% statement coverage.
- `make lint` runs `golangci-lint` with a strict v2 configuration.
- Reproducible static binaries via `make build` and cross-compilation via `make cross-compile`.

## Development

```bash
make test    # run tests with race detector
make lint    # run golangci-lint
make build   # build binary
make fuzz    # run fuzz smoke tests
make bench   # run benchmarks
```

## Status

This is a Go reimplementation of the [original TypeScript client](https://github.com/MoonshotAI/kimi-code), rewritten with dependency injection, context cancellation, and zero global state. The core feature set is implemented and the API is stabilizing; see [CHANGELOG.md](CHANGELOG.md) for recent additions.

## License

MIT — see [LICENSE](LICENSE) for details.
