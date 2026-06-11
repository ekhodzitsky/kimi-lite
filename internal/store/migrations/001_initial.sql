-- Initial schema for kimi-lite session store.

CREATE TABLE IF NOT EXISTS sessions (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL DEFAULT '',
    path       TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sessions_path ON sessions(path);
CREATE INDEX IF NOT EXISTS idx_sessions_updated_at ON sessions(updated_at);

CREATE TABLE IF NOT EXISTS messages (
    id           TEXT NOT NULL,
    session_id   TEXT NOT NULL,
    role         TEXT NOT NULL,
    content      TEXT NOT NULL,
    tool_call_id TEXT,
    tool_calls   TEXT,
    created_at   DATETIME NOT NULL,
    PRIMARY KEY (id, session_id),
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_messages_session_created ON messages(session_id, created_at);

CREATE TABLE IF NOT EXISTS turns (
    id         TEXT NOT NULL,
    session_id TEXT NOT NULL,
    state      TEXT NOT NULL,
    input      TEXT NOT NULL DEFAULT '',
    response   TEXT NOT NULL DEFAULT '',
    tool_calls TEXT,
    results    TEXT,
    error      TEXT,
    started_at DATETIME NOT NULL,
    ended_at   DATETIME,
    PRIMARY KEY (id, session_id),
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_turns_session_started ON turns(session_id, started_at);
