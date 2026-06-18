# Graphify for kimi-lite development

This project uses [Graphify](https://github.com/safishamsi/graphify) as an
optional dev-time tool for understanding the codebase.

## Install

```bash
make graphify-install
```

This creates `.venv-graphify/` and installs the `graphifyy[mcp]` package.

## Build the knowledge graph

```bash
make graphify-build
```

Artifacts are written to `./graphify-out/` (git-ignored).

## Query the graph

```bash
make graphify-query QUESTION="how does turn sequencing work?"
make graphify-explain ENTITY="TurnManager"
make graphify-path FROM="TurnManager" TO="SQLite"
```

## Watch mode

```bash
make graphify-watch
```

Rebuilds the graph incrementally as files change.

## Use as an MCP server

Start the Graphify MCP server:

```bash
make graphify-serve
```

Then add it to your local `~/.config/kimi-lite/config.toml` (replace the
absolute paths with your own):

```toml
[mcp_servers.graphify]
enabled = true
transport = "stdio"
command = "/abs/path/to/kimi-lite/.venv-graphify/bin/python"
args = ["-m", "graphify.serve", "/abs/path/to/kimi-lite/graphify-out/graph.json"]
startup_timeout_ms = 30000
tool_timeout_ms = 60000
```

Restart `kimi-lite` to pick up the server.

## Notes

- Graphify is optional. If it is not installed, use the usual `grep`,
  `read_file`, and `list_directory` tools.
- The `kimi-lite` codebase is Go-only, so code-only extraction works without
  an API key.
