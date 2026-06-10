# ADR 002: SQLite for Session Persistence

## Status
Accepted

## Context
kimi-lite needs to persist conversation sessions, messages, and turn history across restarts. Users should be able to:
- Resume the last session with `--continue`
- Resume a specific session with `--session <id>`
- List all sessions with `/sessions`

## Decision
Use SQLite for session persistence.

## Rationale

### Why SQLite?
- **Zero external dependencies** — embedded in the binary via `mattn/go-sqlite3`
- **Single file** — easy to backup, move, inspect
- **ACID** — reliable transactions for message history
- **Schema migrations** — simple evolution via embedded SQL files
- **Queryable** — SQL for listing, filtering, searching sessions
- **Proven** — used by countless applications, well-tested

### Alternatives Considered
- **JSON files**: Simple but no ACID, concurrency issues, no querying
- **BoltDB/bbolt**: Good for key-value but schema evolution is harder
- **PostgreSQL**: Overkill for a local CLI tool, requires server

## Schema

```sql
CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    name TEXT,
    path TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE messages (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    tool_calls TEXT, -- JSON array
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE turns (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    state INTEGER NOT NULL,
    input TEXT NOT NULL,
    response TEXT,
    tool_calls TEXT, -- JSON array
    results TEXT, -- JSON array
    error TEXT,
    started_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    ended_at DATETIME
);
```

## Consequences

### Positive
- Reliable persistence with transactions
- Easy session querying and management
- Single `.db` file that users can inspect with `sqlite3` CLI
- Foreign keys with cascade deletes

### Negative
- CGO dependency via `mattn/go-sqlite3` (complicates static cross-compilation slightly)
- Database file can grow large with long conversations
- No built-in encryption for session data
