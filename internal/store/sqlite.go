// Package store provides SQLite persistence for kimi-lite sessions.
package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/ekhodzitsky/kimi-lite/internal/idgen"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// ErrSessionNotFound is returned when a session does not exist.
var ErrSessionNotFound = errors.New("session not found")

// defaultTimeout is the default context timeout for individual store queries.
const defaultTimeout = 5 * time.Second

//go:embed migrations/*.sql
var migrationFiles embed.FS

type migration struct {
	version int
	name    string
	content string
}

// parseMigrationVersion extracts the numeric prefix from a migration filename
// such as "001_initial.sql".
func parseMigrationVersion(name string) (int, bool) {
	parts := strings.SplitN(name, "_", 2)
	if len(parts) < 2 {
		return 0, false
	}
	n, err := strconv.Atoi(parts[0])
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// initialCapacity returns a sensible slice capacity for queries that accept a
// user-provided LIMIT. When limit is zero (unlimited) we use a small heuristic
// to avoid repeated reallocations for common result sizes.
func initialCapacity(limit int) int {
	if limit > 0 {
		return limit
	}
	return 16
}

// runMigrations reads numbered *.sql files from fsys/dir, sorts them by their
// numeric prefix, and applies any migration whose version is greater than the
// current PRAGMA user_version. Each migration runs inside a transaction and
// updates user_version in the same connection.
//
// NOTE: This helper does not accept a context because its only caller,
// NewSQLite, has no caller-provided context. Future refactors of the
// constructor should propagate context here.
func runMigrations(db *sql.DB, fsys fs.FS, dir string) error {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	var migrations []migration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version, ok := parseMigrationVersion(entry.Name())
		if !ok {
			continue
		}
		content, err := fs.ReadFile(fsys, path.Join(dir, entry.Name()))
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		migrations = append(migrations, migration{
			version: version,
			name:    entry.Name(),
			content: string(content),
		})
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].version < migrations[j].version
	})

	// Detect duplicate or gap-ridden migration sequences. Migrations must form a
	// contiguous sequence starting at 1 so that missing files are caught early.
	for i, m := range migrations {
		expected := i + 1
		if m.version != expected {
			return fmt.Errorf("migration version gap or duplicate: expected %d, found %d (%s)", expected, m.version, m.name)
		}
	}

	var currentVersion int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&currentVersion); err != nil {
		return fmt.Errorf("read user_version: %w", err)
	}

	for _, m := range migrations {
		if m.version <= currentVersion {
			continue
		}
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin tx for migration %d: %w", m.version, err)
		}
		if _, err := tx.Exec(m.content); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("run migration %d (%s): %w", m.version, m.name, err)
		}
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, m.version)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("set user_version %d: %w", m.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", m.version, err)
		}
	}

	return nil
}

// SQLite implements api.Store using SQLite.
type SQLite struct {
	db *sql.DB
}

// sqliteDSN builds a properly escaped SQLite connection string.
//
// Connection-scoped PRAGMAs are passed through the driver-specific _pragma DSN
// key so they are applied to every new connection, instead of being set
// post-open on whatever pooled connection happens to be in use.
func sqliteDSN(dbPath string) string {
	if dbPath == ":memory:" {
		// Use a unique shared-cache name for each in-memory instance so that
		// separate NewSQLite(":memory:") calls do not share state, while
		// connections within the same pool still share one database.
		// WAL mode is unsupported for :memory:, so it is omitted here.
		q := url.Values{}
		q.Add("_pragma", "busy_timeout(5000)")
		q.Add("_pragma", "foreign_keys(1)")
		return "file:kimi-mem-" + idgen.GenerateID() + "?mode=memory&cache=shared&" + q.Encode()
	}
	// Normalize Windows path separators so backslashes are not percent-encoded
	// by url.URL.EscapedPath.
	dbPath = filepath.ToSlash(dbPath)
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "journal_mode(WAL)")
	// synchronous(NORMAL) is a safe default companion to WAL and keeps write
	// durability reasonable without the full WAL2 penalty.
	q.Add("_pragma", "synchronous(NORMAL)")
	u := &url.URL{Scheme: "file", Path: dbPath, RawQuery: q.Encode()}
	dsn := u.String()
	// url.URL adds "//" for relative paths, which SQLite interprets as an
	// authority component. Strip it to produce the valid file:path form.
	if !filepath.IsAbs(dbPath) {
		dsn = "file:" + u.EscapedPath() + "?" + u.RawQuery
	}
	return dsn
}

// migrateTurnsStateColumn recreates the turns table with state as TEXT if it
// currently has INTEGER type. SQLite is dynamically typed, so existing string
// data is preserved during the copy.
func migrateTurnsStateColumn(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(turns)`)
	if err != nil {
		return fmt.Errorf("inspect turns table: %w", err)
	}
	defer func() { _ = rows.Close() }()

	needsMigrate := false
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var dfltValue sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dfltValue, &pk); err != nil {
			return fmt.Errorf("scan table_info: %w", err)
		}
		// Use SQLite type affinity: any declared type containing "INT" (case-
		// insensitive) has INTEGER affinity.
		if name == "state" && strings.Contains(strings.ToUpper(typ), "INT") {
			needsMigrate = true
			break
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate table_info: %w", err)
	}
	if !needsMigrate {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin turns migration tx: %w", err)
	}

	stmts := []string{
		`ALTER TABLE turns RENAME TO turns_old;`,
		`CREATE TABLE turns (
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
		);`,
		`INSERT INTO turns (id, session_id, state, input, response, tool_calls, results, error, started_at, ended_at)
		 SELECT id, session_id, state, input, response, tool_calls, results, error, started_at, ended_at
		 FROM turns_old;`,
		`DROP TABLE turns_old;`,
		`CREATE INDEX idx_turns_session_started ON turns(session_id, started_at);`,
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("recreate turns table: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit turns migration: %w", err)
	}
	return nil
}

// migrateToolCallIDColumn adds the tool_call_id column to messages if it is
// missing. It inspects the current schema so that real errors are returned.
func migrateToolCallIDColumn(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(messages)`)
	if err != nil {
		return fmt.Errorf("inspect messages table: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var dfltValue sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dfltValue, &pk); err != nil {
			return fmt.Errorf("scan table_info: %w", err)
		}
		if name == "tool_call_id" {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate table_info: %w", err)
	}

	if _, err := db.Exec(`ALTER TABLE messages ADD COLUMN tool_call_id TEXT`); err != nil {
		return fmt.Errorf("add tool_call_id column: %w", err)
	}
	return nil
}

// newSQLiteOptions holds testable configuration for NewSQLite.
type newSQLiteOptions struct {
	chmodFn func(string, os.FileMode) error
}

// newSQLiteOption configures the internal NewSQLite constructor.
type newSQLiteOption func(*newSQLiteOptions)

// withChmod overrides os.Chmod for the database and WAL companion files.
func withChmod(fn func(string, os.FileMode) error) newSQLiteOption {
	return func(o *newSQLiteOptions) { o.chmodFn = fn }
}

// NewSQLite opens (or creates) a SQLite database at dbPath and runs migrations.
func NewSQLite(dbPath string) (*SQLite, error) {
	return newSQLiteWithOptions(dbPath)
}

func newSQLiteWithOptions(dbPath string, opts ...newSQLiteOption) (*SQLite, error) {
	var cfg newSQLiteOptions
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.chmodFn == nil {
		cfg.chmodFn = os.Chmod
	}

	// Create the database file atomically with restrictive permissions before
	// SQLite opens it, eliminating the window where a newly-created file has
	// default (looser) permissions.
	if dbPath != ":memory:" {
		f, err := os.OpenFile(dbPath, os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			return nil, fmt.Errorf("create db file: %w", err)
		}
		_ = f.Close()
	}

	db, err := sql.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if dbPath != ":memory:" {
		if err := cfg.chmodFn(dbPath, 0600); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("set db permissions: %w", err)
		}
	}
	// Keep the pool small: one connection for the WAL writer and one for
	// concurrent readers; the PRAGMAs above apply to every connection.
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(2)

	if err := runMigrations(db, migrationFiles, "migrations"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	// Migrate: add tool_call_id column if missing (schema v1 -> v2).
	if err := migrateToolCallIDColumn(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate messages tool_call_id column: %w", err)
	}

	// Migrate: change turns.state from INTEGER to TEXT if needed (schema v2 -> v3).
	if err := migrateTurnsStateColumn(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate turns.state column: %w", err)
	}

	// Mark any orphaned TurnStreaming records as TurnError from previous crashes.
	if _, err := db.Exec(`UPDATE turns SET state = ?, error = 'process crashed during streaming' WHERE state = ?`, api.TurnError.String(), api.TurnStreaming.String()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("cleanup orphaned turns: %w", err)
	}

	// WAL mode may have created companion files during migrations/cleanup.
	// Restrict their permissions to match the main database file.
	if dbPath != ":memory:" {
		for _, suffix := range []string{"-wal", "-shm"} {
			walPath := dbPath + suffix
			if _, err := os.Stat(walPath); err == nil {
				if err := cfg.chmodFn(walPath, 0600); err != nil {
					_ = db.Close()
					return nil, fmt.Errorf("set %s permissions: %w", walPath, err)
				}
			}
		}
	}

	return &SQLite{db: db}, nil
}

// CreateSession creates a new session for the given path.
func (s *SQLite) CreateSession(ctx context.Context, path string) (*api.Session, error) {
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}
	now := time.Now().UTC()
	sess := &api.Session{
		ID:        idgen.GenerateID(),
		Name:      "",
		Path:      path,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, name, path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		sess.ID, sess.Name, sess.Path, sess.CreatedAt, sess.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("insert session: %w", err)
	}
	return sess, nil
}

// GetSession retrieves a session by ID.
func (s *SQLite) GetSession(ctx context.Context, id string) (*api.Session, error) {
	if id == "" {
		return nil, fmt.Errorf("session ID is required")
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, path, created_at, updated_at FROM sessions WHERE id = ?`, id,
	)

	var sess api.Session
	if err := row.Scan(&sess.ID, &sess.Name, &sess.Path, &sess.CreatedAt, &sess.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("%w: %v", ErrSessionNotFound, err)
		}
		return nil, fmt.Errorf("select session: %w", err)
	}
	sess.Messages = []api.Message{}
	return &sess, nil
}

// GetLastSession returns the most recently updated session for the given path.
func (s *SQLite) GetLastSession(ctx context.Context, path string) (*api.Session, error) {
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, path, created_at, updated_at FROM sessions WHERE path = ? ORDER BY updated_at DESC, id DESC LIMIT 1`, path,
	)

	var sess api.Session
	if err := row.Scan(&sess.ID, &sess.Name, &sess.Path, &sess.CreatedAt, &sess.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("%w: %v", ErrSessionNotFound, err)
		}
		return nil, fmt.Errorf("select last session: %w", err)
	}
	sess.Messages = []api.Message{}
	return &sess, nil
}

// ListSessions returns all sessions for the given path ordered by updated_at desc.
func (s *SQLite) ListSessions(ctx context.Context, path string, limit int) ([]api.Session, error) {
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}
	if limit <= 0 {
		limit = 10000
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, path, created_at, updated_at FROM sessions WHERE path = ? ORDER BY updated_at DESC, id DESC LIMIT ?`, path, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	sessions := make([]api.Session, 0, initialCapacity(limit))
	for rows.Next() {
		var sess api.Session
		if err := rows.Scan(&sess.ID, &sess.Name, &sess.Path, &sess.CreatedAt, &sess.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		sess.Messages = []api.Message{}
		sess.LastPrompt, _ = s.lastUserPrompt(ctx, sess.ID)
		sessions = append(sessions, sess)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sessions: %w", err)
	}
	return sessions, nil
}

// ListAllSessions returns sessions across all paths ordered by updated_at desc.
func (s *SQLite) ListAllSessions(ctx context.Context, limit int) ([]api.Session, error) {
	if limit <= 0 {
		limit = 10000
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, path, created_at, updated_at FROM sessions ORDER BY updated_at DESC, id DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list all sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	sessions := make([]api.Session, 0, initialCapacity(limit))
	for rows.Next() {
		var sess api.Session
		if err := rows.Scan(&sess.ID, &sess.Name, &sess.Path, &sess.CreatedAt, &sess.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		sess.Messages = []api.Message{}
		sess.LastPrompt, _ = s.lastUserPrompt(ctx, sess.ID)
		sessions = append(sessions, sess)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sessions: %w", err)
	}
	return sessions, nil
}

// lastUserPrompt returns the most recent user message content for a session,
// truncated for display. An empty string is returned when there is no user
// message or on lookup error so that listing remains best-effort.
func (s *SQLite) lastUserPrompt(ctx context.Context, sessionID string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	row := s.db.QueryRowContext(ctx, `
		SELECT content FROM messages
		WHERE session_id = ? AND role = ?
		ORDER BY created_at DESC
		LIMIT 1
	`, sessionID, api.RoleUser)
	var content string
	if err := row.Scan(&content); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("last user prompt: %w", err)
	}
	return truncatePrompt(content), nil
}

// truncatePrompt returns the first line of s trimmed to at most 120 characters.
func truncatePrompt(s string) string {
	const max = 120
	s = strings.TrimSpace(s)
	lines := strings.Split(s, "\n")
	first := lines[0]
	if len(first) > max {
		return first[:max-1] + "…"
	}
	return first
}

// UpdateSession updates session metadata.
func (s *SQLite) UpdateSession(ctx context.Context, session *api.Session) error {
	if session.ID == "" {
		return fmt.Errorf("session ID is required")
	}
	session.UpdatedAt = time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET name = ?, path = ?, updated_at = ? WHERE id = ?`,
		session.Name, session.Path, session.UpdatedAt, session.ID,
	)
	if err != nil {
		return fmt.Errorf("update session: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%w: %s", ErrSessionNotFound, session.ID)
	}
	return nil
}

// DeleteSession removes a session and its messages/turns.
func (s *SQLite) DeleteSession(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("session ID is required")
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%w: %s", ErrSessionNotFound, id)
	}
	return nil
}

// AppendMessage adds a message to a session.
func (s *SQLite) AppendMessage(ctx context.Context, sessionID string, msg api.Message) error {
	if sessionID == "" {
		return fmt.Errorf("session ID is required")
	}
	toolCallsJSON, err := json.Marshal(msg.ToolCalls)
	if err != nil {
		return fmt.Errorf("marshal tool calls: %w", err)
	}
	contentPartsJSON, err := json.Marshal(msg.ContentParts)
	if err != nil {
		return fmt.Errorf("marshal content parts: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO messages (id, session_id, role, content, content_parts, tool_call_id, tool_calls, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		msg.ID, sessionID, string(msg.Role), msg.Content, string(contentPartsJSON), msg.ToolCallID, string(toolCallsJSON), msg.CreatedAt.UTC(),
	); err != nil {
		return fmt.Errorf("insert message: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE sessions SET updated_at = ? WHERE id = ?`,
		time.Now().UTC(), sessionID,
	); err != nil {
		return fmt.Errorf("update session timestamp: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// GetMessages returns all messages for a session ordered by created_at.
func (s *SQLite) GetMessages(ctx context.Context, sessionID string, limit int) ([]api.Message, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("session ID is required")
	}
	if limit <= 0 {
		limit = 1000
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, role, content, content_parts, tool_call_id, tool_calls, created_at FROM messages WHERE session_id = ? ORDER BY created_at ASC, id ASC LIMIT ?`, sessionID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("get messages: %w", err)
	}
	defer func() { _ = rows.Close() }()

	msgs := make([]api.Message, 0, initialCapacity(limit))
	for rows.Next() {
		var msg api.Message
		var roleStr string
		var contentPartsJSON string
		var toolCallsJSON string
		if err := rows.Scan(&msg.ID, &roleStr, &msg.Content, &contentPartsJSON, &msg.ToolCallID, &toolCallsJSON, &msg.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		msg.Role = api.Role(roleStr)
		if contentPartsJSON != "" && contentPartsJSON != "null" {
			if err := json.Unmarshal([]byte(contentPartsJSON), &msg.ContentParts); err != nil {
				return nil, fmt.Errorf("unmarshal content parts: %w", err)
			}
		}
		if toolCallsJSON != "" && toolCallsJSON != "null" {
			if err := json.Unmarshal([]byte(toolCallsJSON), &msg.ToolCalls); err != nil {
				return nil, fmt.Errorf("unmarshal tool calls: %w", err)
			}
		}
		msgs = append(msgs, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate messages: %w", err)
	}
	return msgs, nil
}

// ClearMessages removes all messages for a session.
func (s *SQLite) ClearMessages(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return fmt.Errorf("session ID is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE session_id = ?`, sessionID); err != nil {
		return fmt.Errorf("clear messages: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE sessions SET updated_at = ? WHERE id = ?`,
		time.Now().UTC(), sessionID,
	); err != nil {
		return fmt.Errorf("update session timestamp: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// ReplaceMessages atomically replaces all messages for a session.
func (s *SQLite) ReplaceMessages(ctx context.Context, sessionID string, msgs []api.Message) error {
	if sessionID == "" {
		return fmt.Errorf("session ID is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE session_id = ?`, sessionID); err != nil {
		return fmt.Errorf("clear messages: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO messages (id, session_id, role, content, content_parts, tool_call_id, tool_calls, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, msg := range msgs {
		toolCallsJSON, err := json.Marshal(msg.ToolCalls)
		if err != nil {
			return fmt.Errorf("marshal tool calls: %w", err)
		}
		contentPartsJSON, err := json.Marshal(msg.ContentParts)
		if err != nil {
			return fmt.Errorf("marshal content parts: %w", err)
		}
		if _, err := stmt.ExecContext(ctx,
			msg.ID, sessionID, string(msg.Role), msg.Content, string(contentPartsJSON), msg.ToolCallID, string(toolCallsJSON), msg.CreatedAt.UTC(),
		); err != nil {
			return fmt.Errorf("insert message: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE sessions SET updated_at = ? WHERE id = ?`,
		time.Now().UTC(), sessionID,
	); err != nil {
		return fmt.Errorf("update session timestamp: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// SaveTurn persists a turn for a session.
func (s *SQLite) SaveTurn(ctx context.Context, sessionID string, turn api.Turn) error {
	if sessionID == "" {
		return fmt.Errorf("session ID is required")
	}
	toolCallsJSON, err := json.Marshal(turn.ToolCalls)
	if err != nil {
		return fmt.Errorf("marshal tool calls: %w", err)
	}
	resultsJSON, err := json.Marshal(turn.Results)
	if err != nil {
		return fmt.Errorf("marshal results: %w", err)
	}

	var endedAt any
	if turn.EndedAt != nil {
		endedAt = turn.EndedAt.UTC()
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO turns (id, session_id, state, input, response, tool_calls, results, error, started_at, ended_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id, session_id) DO UPDATE SET
		   state = excluded.state,
		   input = excluded.input,
		   response = excluded.response,
		   tool_calls = excluded.tool_calls,
		   results = excluded.results,
		   error = excluded.error,
		   started_at = excluded.started_at,
		   ended_at = excluded.ended_at`,
		turn.ID, sessionID, turn.State.String(), turn.Input, turn.Response,
		string(toolCallsJSON), string(resultsJSON), turn.Error,
		turn.StartedAt.UTC(), endedAt,
	); err != nil {
		return fmt.Errorf("upsert turn: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE sessions SET updated_at = ? WHERE id = ?`,
		time.Now().UTC(), sessionID,
	); err != nil {
		return fmt.Errorf("update session timestamp: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// GetTurns returns all turns for a session ordered by started_at.
func (s *SQLite) GetTurns(ctx context.Context, sessionID string, limit int) ([]api.Turn, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("session ID is required")
	}
	if limit <= 0 {
		limit = 1000
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, state, input, response, tool_calls, results, error, started_at, ended_at FROM turns WHERE session_id = ? ORDER BY started_at ASC, id ASC LIMIT ?`, sessionID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("get turns: %w", err)
	}
	defer func() { _ = rows.Close() }()

	turns := make([]api.Turn, 0, initialCapacity(limit))
	for rows.Next() {
		var turn api.Turn
		var stateStr string
		var toolCallsJSON string
		var resultsJSON string
		var endedAt sql.NullTime
		if err := rows.Scan(&turn.ID, &stateStr, &turn.Input, &turn.Response, &toolCallsJSON, &resultsJSON, &turn.Error, &turn.StartedAt, &endedAt); err != nil {
			return nil, fmt.Errorf("scan turn: %w", err)
		}
		turn.State, err = api.ParseTurnState(stateStr)
		if err != nil {
			return nil, fmt.Errorf("parse turn state: %w", err)
		}
		if toolCallsJSON != "" && toolCallsJSON != "null" {
			if err := json.Unmarshal([]byte(toolCallsJSON), &turn.ToolCalls); err != nil {
				return nil, fmt.Errorf("unmarshal tool calls: %w", err)
			}
		}
		if resultsJSON != "" && resultsJSON != "null" {
			if err := json.Unmarshal([]byte(resultsJSON), &turn.Results); err != nil {
				return nil, fmt.Errorf("unmarshal results: %w", err)
			}
		}
		if endedAt.Valid {
			t := endedAt.Time
			turn.EndedAt = &t
		}
		turns = append(turns, turn)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate turns: %w", err)
	}
	return turns, nil
}

// CountTurns returns the number of turns for a session with the given state.
func (s *SQLite) CountTurns(ctx context.Context, sessionID string, state api.TurnState) (int, error) {
	if sessionID == "" {
		return 0, fmt.Errorf("session ID is required")
	}
	var count int
	row := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM turns WHERE session_id = ? AND state = ?`, sessionID, state.String(),
	)
	if err := row.Scan(&count); err != nil {
		return 0, fmt.Errorf("count turns: %w", err)
	}
	return count, nil
}

// Close closes the underlying database connection.
func (s *SQLite) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("close sqlite: %w", err)
	}
	return nil
}
