# kimi-lite

[![CI](https://github.com/ekhodzitsky/kimi-lite/actions/workflows/ci.yml/badge.svg)](https://github.com/ekhodzitsky/kimi-lite/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Go port of the [original Python AI coding client](https://github.com/MoonshotAI/kimi-code).

A single-terminal AI assistant with no Node, Python runtime, or Electron dependencies.

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
```

## Usage

```bash
kimi-lite                        # start a new session
kimi-lite --continue             # resume the last session
kimi-lite --session <id>         # resume a specific session
```

Inside the chat:

| Command | Description |
|---------|-------------|
| `/compact` | Summarize conversation history |
| `/clear` | Clear current conversation |
| `/sessions` | List available sessions |

## Development

```bash
make test    # run tests with race detector
make lint    # run golangci-lint
make build   # build binary
```

## Status

This is a Go reimplementation of the [original Python client](https://github.com/MoonshotAI/kimi-code). Not all features from the original are ported yet, and the API may change.

## License

MIT — see [LICENSE](LICENSE) for details.
