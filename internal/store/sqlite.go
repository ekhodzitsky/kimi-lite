// Package store provides SQLite persistence for kimi-lite sessions.
package store

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"github.com/ekhodzitsky/kimi-lite/internal/idgen"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
	_ "modernc.org/sqlite"
)

//go:embed migrations/001_initial.sql
var initialSchema string

// SQLite implements api.Store using SQLite.
type SQLite struct {
	db *sql.DB
}

// NewSQLite opens (or creates) a SQLite database at dbPath and runs migrations.
func NewSQLite(dbPath string) (*SQLite, error) {
	q := url.Values{}
	q.Set("_fk", "1")
	if dbPath == ":memory:" {
		q.Set("cache", "shared")
	}
	u := url.URL{Scheme: "file", Opaque: dbPath, RawQuery: q.Encode()}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(2)

	if _, err := db.Exec(`PRAGMA foreign_keys = ON;`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL;`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable wal mode: %w", err)
	}
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000;`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("set busy timeout: %w", err)
	}

	if _, err := db.Exec(initialSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("run initial schema: %w", err)
	}

	// Mark any orphaned TurnStreaming records as TurnError from previous crashes.
	if _, err := db.Exec(`UPDATE turns SET state = ?, error = 'process crashed during streaming' WHERE state = ?`, api.TurnError.String(), api.TurnStreaming.String()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("cleanup orphaned turns: %w", err)
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
			return nil, fmt.Errorf("session not found: %w", err)
		}
		return nil, fmt.Errorf("select session: %w", err)
	}
	sess.Messages = []api.Message{}
	return &sess, nil
}

// GetLastSession returns the most recently updated session for the given path.
func (s *SQLite) GetLastSession(ctx context.Context, path string) (*api.Session, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, path, created_at, updated_at FROM sessions WHERE path = ? ORDER BY updated_at DESC LIMIT 1`, path,
	)

	var sess api.Session
	if err := row.Scan(&sess.ID, &sess.Name, &sess.Path, &sess.CreatedAt, &sess.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("session not found: %w", err)
		}
		return nil, fmt.Errorf("select last session: %w", err)
	}
	sess.Messages = []api.Message{}
	return &sess, nil
}

// ListSessions returns all sessions for the given path ordered by updated_at desc.
func (s *SQLite) ListSessions(ctx context.Context, path string, limit int) ([]api.Session, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, path, created_at, updated_at FROM sessions WHERE path = ? ORDER BY updated_at DESC LIMIT ?`, path, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []api.Session
	for rows.Next() {
		var sess api.Session
		if err := rows.Scan(&sess.ID, &sess.Name, &sess.Path, &sess.CreatedAt, &sess.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		sess.Messages = []api.Message{}
		sessions = append(sessions, sess)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sessions: %w", err)
	}
	return sessions, nil
}

// UpdateSession updates session metadata.
func (s *SQLite) UpdateSession(ctx context.Context, session *api.Session) error {
	session.UpdatedAt = time.Now().UTC()
	if _, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET name = ?, path = ?, updated_at = ? WHERE id = ?`,
		session.Name, session.Path, session.UpdatedAt, session.ID,
	); err != nil {
		return fmt.Errorf("update session: %w", err)
	}
	return nil
}

// DeleteSession removes a session and its messages/turns.
func (s *SQLite) DeleteSession(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("session ID is required")
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete session: %w", err)
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

	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO messages (id, session_id, role, content, tool_calls, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		msg.ID, sessionID, string(msg.Role), msg.Content, string(toolCallsJSON), msg.CreatedAt.UTC(),
	); err != nil {
		return fmt.Errorf("insert message: %w", err)
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
		`SELECT id, role, content, tool_calls, created_at FROM messages WHERE session_id = ? ORDER BY created_at ASC LIMIT ?`, sessionID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("get messages: %w", err)
	}
	defer rows.Close()

	var msgs []api.Message
	for rows.Next() {
		var msg api.Message
		var roleStr string
		var toolCallsJSON string
		if err := rows.Scan(&msg.ID, &roleStr, &msg.Content, &toolCallsJSON, &msg.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		msg.Role = api.Role(roleStr)
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
	if _, err := s.db.ExecContext(ctx, `DELETE FROM messages WHERE session_id = ?`, sessionID); err != nil {
		return fmt.Errorf("clear messages: %w", err)
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

	for _, msg := range msgs {
		toolCallsJSON, err := json.Marshal(msg.ToolCalls)
		if err != nil {
			return fmt.Errorf("marshal tool calls: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO messages (id, session_id, role, content, tool_calls, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			msg.ID, sessionID, string(msg.Role), msg.Content, string(toolCallsJSON), msg.CreatedAt.UTC(),
		); err != nil {
			return fmt.Errorf("insert message: %w", err)
		}
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

	var endedAt interface{}
	if turn.EndedAt != nil {
		endedAt = turn.EndedAt.UTC()
	}

	if _, err := s.db.ExecContext(ctx,
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
	return nil
}

// GetTurns returns all turns for a session ordered by started_at.
func (s *SQLite) GetTurns(ctx context.Context, sessionID string, limit int) ([]api.Turn, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("session ID is required")
	}
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, state, input, response, tool_calls, results, error, started_at, ended_at FROM turns WHERE session_id = ? ORDER BY started_at ASC LIMIT ?`, sessionID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("get turns: %w", err)
	}
	defer rows.Close()

	var turns []api.Turn
	for rows.Next() {
		var turn api.Turn
		var stateStr string
		var toolCallsJSON string
		var resultsJSON string
		var endedAt sql.NullTime
		if err := rows.Scan(&turn.ID, &stateStr, &turn.Input, &turn.Response, &toolCallsJSON, &resultsJSON, &turn.Error, &turn.StartedAt, &endedAt); err != nil {
			return nil, fmt.Errorf("scan turn: %w", err)
		}
		turn.State = api.ParseTurnState(stateStr)
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

// Close closes the underlying database connection.
func (s *SQLite) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("close sqlite: %w", err)
	}
	return nil
}
