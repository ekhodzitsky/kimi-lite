# ADR 003: MCP via mcp-guard Proxy

## Status
Accepted

## Context
MCP (Model Context Protocol) allows AI tools to discover and use external tools. However, connecting directly to MCP servers from a CLI tool has risks:
- Untrusted MCP servers can execute arbitrary code
- No audit trail of tool invocations
- Difficult to implement per-tool policies

## Decision
Connect to MCP servers through `mcp-guard` proxy rather than directly.

## Rationale

### Why mcp-guard?
- **Security** — mcp-guard validates and filters tool calls
- **Policy enforcement** — per-tool allow/deny lists
- **Audit logging** — all tool invocations are logged
- **Stability** — isolates kimi-lite from MCP server crashes
- **Configuration** — centralized TOML config for all MCP servers

### Architecture
```
kimi-lite ←→ mcp-guard ←→ MCP Server A
                    ←→ MCP Server B
                    ←→ MCP Server C
```

### Communication
- JSON-RPC 2.0 over stdio (stdin/stdout)
- mcp-guard is spawned as a subprocess
- kimi-lite sends `initialize`, `tools/list`, `tools/call` requests
- Graceful degradation if mcp-guard is not installed

## Configuration

```toml
[mcp]
guard_command = "mcp-guard"
guard_config = "~/.config/mcp-guard/mcp-guard.toml"
```

## Consequences

### Positive
- Enhanced security via proxy validation
- Centralized MCP server configuration
- Audit trail for compliance
- kimi-lite works without MCP if mcp-guard is unavailable

### Negative
- Additional dependency (mcp-guard must be installed separately)
- Slight latency increase due to proxy hop
- Requires mcp-guard to be in PATH
