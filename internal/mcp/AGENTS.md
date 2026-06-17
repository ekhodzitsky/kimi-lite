# MCP Integration Guide

> Scoped rules for `internal/mcp`. See root `AGENTS.md` for build/test commands
> and general Go conventions.
>
> **Version:** 2.0
> **Last updated:** 2026-06-16

## Supported Modes

kimi-lite supports three MCP integration modes.

### 1. Legacy mcp-guard path

Used when `cfg.MCPServers` is empty.

- Attempts to find `mcp-guard` in `PATH`.
- If found, starts the subprocess and connects via stdio JSON-RPC.
- If not found, runs with built-in tools only.
- Uses the same TOML config format as mcp-guard.

### 2. Direct MCP server configuration

Used when `cfg.MCPServers` is populated.

- Each server is configured via `[mcp_servers.<name>]` tables in `config.toml`.
- Supported transports:
  - `stdio`: `command`, `args`, `env`, `cwd`.
  - `http`: `url`, `headers`, `bearer_token_env_var`.
  - `sse`: `url`, `headers`, `bearer_token_env_var`.
- Per-server options: `enabled`, `startup_timeout_ms`, `tool_timeout_ms`,
  `enabled_tools`, `disabled_tools`.
- HTTP and SSE transports use `netutil.SecureHTTPClient()` for SSRF-hardened
  outbound requests.
- Multiple servers are aggregated by `mcp.MultiClient`.
- Duplicate tool names are prefixed with the server key.
- Unavailable servers are logged and skipped; the app continues with the
  remaining servers.

## Key Types and Functions

- `NewClient(transport)` — creates a client from any `Transport`.
- `NewClientFromConfig(cfg)` — legacy stdio client connected to `mcp-guard`.
- `NewClientFromServerConfig(cfg, httpClient)` — direct stdio, http, or sse
  client from `api.MCPServerConfig`.
- `NewHTTPTransport(url, headers, bearerEnv, httpClient)` — JSON-RPC over HTTP
  POST.
- `NewSSETransport(url, headers, bearerEnv, httpClient)` — JSON-RPC over
  Server-Sent Events.
- `NewMultiClient(clients, configs)` — aggregates multiple MCP clients.
- `Connect()` — performs the MCP initialize handshake.
- `ListTools()` / `CallTool()` — tool operations.

## Schema Normalization

- MCP tool parameter schemas are normalized to the stricter Moonshot JSON
  Schema subset.
- Fills missing types, collapses type arrays, fixes `anyOf`/`oneOf` parent
  types.

## Safety

- MCP stdio servers spawn subprocesses. Validate `command` paths and do not
  execute arbitrary user-provided strings.
- HTTP/SSE transports must use `netutil.SecureHTTPClient()`.
- Respect per-server `tool_timeout_ms` and `enabled_tools`/`disabled_tools`.

## Adding a New Transport

1. Add a new `Transport` implementation in `internal/mcp/`.
2. Wire it into `NewClientFromServerConfig()`.
3. Ensure SSRF protection for network transports.
4. Add unit tests that mock the underlying connection or server.
5. Update this file and root `AGENTS.md` if user-facing config changes.
