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
- **Zero external dependencies** — pure-Go driver (`modernc.org/sqlite`), no CGO required
- **Single file** — easy to backup, move, inspect
- **ACID** — reliable transactions for message history
- **Schema migrations** — simple evolution via embedded SQL files
- **Queryable** — SQL for listing, filtering, searching sessions
- **Proven** — used by countless applications, well-tested

### Alternatives Considered
- **JSON files**: Simple but no ACID, concurrency issues, no querying
- **BoltDB/bbolt**: Good for key-value but schema evolution is harder
- **PostgreSQL**: Overkill for a local CLI tool, requires server
- **mattn/go-sqlite3**: Rejected because it requires CGO, which complicates static cross-compilation and breaks `CGO_ENABLED=0` builds

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
- `modernc.org/sqlite` is a larger/slightly slower dependency than the C driver, but pure Go so `CGO_ENABLED=0` static cross-compilation works out of the box
- Database file can grow large with long conversations
- No built-in encryption for session data
