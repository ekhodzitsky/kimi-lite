# kimi-lite

[![CI](https://github.com/ekhodzitsky/kimi-lite/actions/workflows/ci.yml/badge.svg)](https://github.com/ekhodzitsky/kimi-lite/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Go port of the [original Python AI coding client](https://github.com/MoonshotAI/kimi-code).

A single-terminal AI assistant with no Node, Python runtime, or Electron dependencies.

## Features

- **Streaming TUI chat** — native Bubble Tea interface with multi-line input, history, and sidebar file browser.
- **Built-in tools** — `read_file`, `write_file`, `str_replace_file`, `edit`, `glob`, `grep`, `shell`, `fetch_url`, `list_directory`, `web_search`, and `read_video` (ffmpeg/ffprobe) with sandboxed file access.
- **Subagents** — delegate focused work to `coder`, `explore`, and `plan` subagents via `dispatch_subagent`.
- **Lifecycle hooks** — run local commands at `session_start`, `turn_start`, `turn_end`, `tool_call`, `tool_result`, `approval_request`, and `approval_decision`.
- **ACP server** — `kimi-lite acp` speaks JSON-RPC 2.0 over stdio for external agent integration.
- **Risk-aware approvals** — every tool call is scored `low`/`medium`/`high`; destructive or sandbox-escaping operations require approval.
- **Context compression** — `/compact` summarizes older history using a language-aware token estimator.
- **Observability** — `--pprof` runtime profiling and internal metrics collection.
- **MCP support** — connect to Model Context Protocol servers via stdio or HTTP.

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
```

Drop skill files into `~/.config/kimi-lite/skills/` (e.g. `go.md`, `python.md`); their contents are appended to the system prompt.

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
| `/sessions` | List available sessions |

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

This is a Go reimplementation of the [original Python client](https://github.com/MoonshotAI/kimi-code), rewritten with dependency injection, context cancellation, and zero global state. The core feature set is implemented and the API is stabilizing; see [CHANGELOG.md](CHANGELOG.md) for recent additions.

## License

MIT — see [LICENSE](LICENSE) for details.
