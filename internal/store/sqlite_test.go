package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ekhodzitsky/kimi-lite/internal/idgen"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func newTestStore(t *testing.T) *SQLite {
	t.Helper()
	s, err := NewSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func createSessionAt(t *testing.T, s *SQLite, path string, createdAt, updatedAt time.Time) *api.Session {
	t.Helper()
	sess := &api.Session{
		ID:        idgen.GenerateID(),
		Name:      "",
		Path:      path,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}
	_, err := s.db.ExecContext(context.Background(),
		`INSERT INTO sessions (id, name, path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		sess.ID, sess.Name, sess.Path, sess.CreatedAt, sess.UpdatedAt,
	)
	if err != nil {
		t.Fatalf("create session at: %v", err)
	}
	return sess
}

func TestSQLite_CreateSession(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	sess, err := s.CreateSession(ctx, "/tmp/proj")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if sess.ID == "" {
		t.Error("expected session ID to be set")
	}
	if sess.Path != "/tmp/proj" {
		t.Errorf("path = %q, want %q", sess.Path, "/tmp/proj")
	}
	if sess.CreatedAt.IsZero() {
		t.Error("expected created_at to be set")
	}
	if sess.UpdatedAt.IsZero() {
		t.Error("expected updated_at to be set")
	}
}

func TestSQLite_GetSession(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	sess, err := s.CreateSession(ctx, "/tmp/proj")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	got, err := s.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if got.ID != sess.ID {
		t.Errorf("id = %q, want %q", got.ID, sess.ID)
	}
	if got.Name != sess.Name {
		t.Errorf("name = %q, want %q", got.Name, sess.Name)
	}
	if got.Path != sess.Path {
		t.Errorf("path = %q, want %q", got.Path, sess.Path)
	}
}

func TestSQLite_GetSession_NotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	_, err := s.GetSession(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got %T: %v", err, err)
	}
}

func TestSQLite_GetLastSession(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	_ = createSessionAt(t, s, "/tmp/proj", base, base)
	s2 := createSessionAt(t, s, "/tmp/proj", base.Add(time.Second), base.Add(time.Second))

	got, err := s.GetLastSession(ctx, "/tmp/proj")
	if err != nil {
		t.Fatalf("get last session: %v", err)
	}
	if got.ID != s2.ID {
		t.Errorf("last session = %q, want %q", got.ID, s2.ID)
	}

	_, err = s.GetLastSession(ctx, "/other")
	if err == nil {
		t.Fatal("expected error for different path")
	}
}

func TestSQLite_ListSessions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	s1 := createSessionAt(t, s, "/tmp/proj", base, base)
	s2 := createSessionAt(t, s, "/tmp/proj", base.Add(time.Second), base.Add(time.Second))

	list, err := s.ListSessions(ctx, "/tmp/proj", 0)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(list))
	}
	if list[0].ID != s2.ID {
		t.Errorf("first session = %q, want %q", list[0].ID, s2.ID)
	}
	if list[1].ID != s1.ID {
		t.Errorf("second session = %q, want %q", list[1].ID, s1.ID)
	}
}

func TestSQLite_ListSessions_Empty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	list, err := s.ListSessions(ctx, "/nonexistent", 0)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 sessions, got %d", len(list))
	}
}

func TestSQLite_UpdateSession(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	sess, _ := s.CreateSession(ctx, "/tmp/proj")
	sess.Name = "updated"
	oldUpdated := sess.UpdatedAt

	if err := s.UpdateSession(ctx, sess); err != nil {
		t.Fatalf("update session: %v", err)
	}

	got, _ := s.GetSession(ctx, sess.ID)
	if got.Name != "updated" {
		t.Errorf("name = %q, want %q", got.Name, "updated")
	}
	if !got.UpdatedAt.After(oldUpdated) {
		t.Error("expected updated_at to change")
	}
}

func TestSQLite_DeleteSession(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	sess, _ := s.CreateSession(ctx, "/tmp/proj")
	if err := s.DeleteSession(ctx, sess.ID); err != nil {
		t.Fatalf("delete session: %v", err)
	}

	_, err := s.GetSession(ctx, sess.ID)
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestSQLite_DeleteSession_Cascade(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	sess, _ := s.CreateSession(ctx, "/tmp/proj")
	_ = s.AppendMessage(ctx, sess.ID, api.Message{ID: "m1", Role: api.RoleUser, Content: "hi", CreatedAt: time.Now().UTC()})
	_ = s.SaveTurn(ctx, sess.ID, api.Turn{ID: "t1", State: api.TurnIdle, StartedAt: time.Now().UTC()})

	if err := s.DeleteSession(ctx, sess.ID); err != nil {
		t.Fatalf("delete session: %v", err)
	}

	msgs, err := s.GetMessages(ctx, sess.ID, 0)
	if err != nil {
		t.Fatalf("get messages after delete: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages after cascade, got %d", len(msgs))
	}
	turns, err := s.GetTurns(ctx, sess.ID, 0)
	if err != nil {
		t.Fatalf("get turns after delete: %v", err)
	}
	if len(turns) != 0 {
		t.Errorf("expected 0 turns after cascade, got %d", len(turns))
	}
}

func TestSQLite_PragmaAppliedToAllConnections(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	// Reserve both pooled connections sequentially so the second one is forced
	// to be a distinct physical connection from the first.
	conns := make([]*sql.Conn, 2)
	for i := range conns {
		conn, err := s.db.Conn(ctx)
		if err != nil {
			t.Fatalf("acquire connection %d: %v", i, err)
		}
		defer conn.Close()
		conns[i] = conn
	}

	for i, conn := range conns {
		var fk int
		if err := conn.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&fk); err != nil {
			t.Fatalf("read foreign_keys on connection %d: %v", i, err)
		}
		if fk != 1 {
			t.Errorf("foreign_keys on connection %d = %d, want 1", i, fk)
		}

		var bt int
		if err := conn.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&bt); err != nil {
			t.Fatalf("read busy_timeout on connection %d: %v", i, err)
		}
		if bt != 5000 {
			t.Errorf("busy_timeout on connection %d = %d, want 5000", i, bt)
		}
	}
}

func TestSQLite_CrashRecovery(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "crash.db")

	s, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	sess, err := s.CreateSession(ctx, "/tmp/proj")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	turn := api.Turn{
		ID:        idgen.GenerateID(),
		State:     api.TurnStreaming,
		Input:     "hi",
		StartedAt: time.Now().UTC(),
	}
	if err := s.SaveTurn(ctx, sess.ID, turn); err != nil {
		t.Fatalf("save streaming turn: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	// Re-open the same on-disk database. NewSQLite should recover the orphaned
	// streaming turn and mark it as an error.
	s2, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer s2.Close()

	turns, err := s2.GetTurns(ctx, sess.ID, 0)
	if err != nil {
		t.Fatalf("get turns: %v", err)
	}
	if len(turns) != 1 {
		t.Fatalf("len(turns) = %d, want 1", len(turns))
	}
	got := turns[0]
	if got.State != api.TurnError {
		t.Errorf("state = %v, want TurnError", got.State)
	}
	if !strings.Contains(got.Error, "process crashed during streaming") {
		t.Errorf("error = %q, want containing 'process crashed during streaming'", got.Error)
	}

	// Sanity-check that the DSN PRAGMAs were applied on the file-backed DB.
	var journalMode string
	if err := s2.db.QueryRow(`PRAGMA journal_mode`).Scan(&journalMode); err != nil {
		t.Fatalf("read journal_mode: %v", err)
	}
	if !strings.EqualFold(journalMode, "wal") {
		t.Errorf("journal_mode = %q, want wal", journalMode)
	}

	var busyTimeout int
	if err := s2.db.QueryRow(`PRAGMA busy_timeout`).Scan(&busyTimeout); err != nil {
		t.Fatalf("read busy_timeout: %v", err)
	}
	if busyTimeout != 5000 {
		t.Errorf("busy_timeout = %d, want 5000", busyTimeout)
	}
}

func TestSQLite_DeleteSession_NoOrphans(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	sess, _ := s.CreateSession(ctx, "/tmp/proj")
	_ = s.AppendMessage(ctx, sess.ID, api.Message{ID: "m1", Role: api.RoleUser, Content: "hi", CreatedAt: time.Now().UTC()})
	_ = s.SaveTurn(ctx, sess.ID, api.Turn{ID: "t1", State: api.TurnIdle, StartedAt: time.Now().UTC()})

	if err := s.DeleteSession(ctx, sess.ID); err != nil {
		t.Fatalf("delete session: %v", err)
	}

	var msgCount, turnCount, sessCount int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM messages`).Scan(&msgCount); err != nil {
		t.Fatalf("count messages: %v", err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM turns`).Scan(&turnCount); err != nil {
		t.Fatalf("count turns: %v", err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE id = ?`, sess.ID).Scan(&sessCount); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if msgCount != 0 {
		t.Errorf("messages table has %d orphaned rows, want 0", msgCount)
	}
	if turnCount != 0 {
		t.Errorf("turns table has %d orphaned rows, want 0", turnCount)
	}
	if sessCount != 0 {
		t.Errorf("sessions table has %d rows for deleted session, want 0", sessCount)
	}
}

func TestSQLite_AppendAndGetMessages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	sess, _ := s.CreateSession(ctx, "/tmp/proj")
	msg := api.Message{
		ID:        "msg-1",
		Role:      api.RoleUser,
		Content:   "hello",
		CreatedAt: time.Now().UTC(),
	}
	if err := s.AppendMessage(ctx, sess.ID, msg); err != nil {
		t.Fatalf("append message: %v", err)
	}

	msgs, err := s.GetMessages(ctx, sess.ID, 0)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].ID != msg.ID {
		t.Errorf("msg id = %q, want %q", msgs[0].ID, msg.ID)
	}
	if msgs[0].Role != msg.Role {
		t.Errorf("msg role = %q, want %q", msgs[0].Role, msg.Role)
	}
	if msgs[0].Content != msg.Content {
		t.Errorf("msg content = %q, want %q", msgs[0].Content, msg.Content)
	}
}

func TestSQLite_AppendMessage_WithToolCalls(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	sess, _ := s.CreateSession(ctx, "/tmp/proj")
	msg := api.Message{
		ID:      "msg-1",
		Role:    api.RoleAssistant,
		Content: "using tools",
		ToolCalls: []api.ToolCall{
			{ID: "tc1", Name: "grep", Arguments: `{"pattern":"foo"}`},
		},
		CreatedAt: time.Now().UTC(),
	}
	if err := s.AppendMessage(ctx, sess.ID, msg); err != nil {
		t.Fatalf("append message: %v", err)
	}

	msgs, _ := s.GetMessages(ctx, sess.ID, 0)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if len(msgs[0].ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(msgs[0].ToolCalls))
	}
	if msgs[0].ToolCalls[0].Name != "grep" {
		t.Errorf("tool call name = %q, want %q", msgs[0].ToolCalls[0].Name, "grep")
	}
	if msgs[0].ToolCalls[0].Arguments != `{"pattern":"foo"}` {
		t.Errorf("tool call arguments = %q, want %q", msgs[0].ToolCalls[0].Arguments, `{"pattern":"foo"}`)
	}
}

func TestSQLite_ClearMessages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	sess, _ := s.CreateSession(ctx, "/tmp/proj")
	_ = s.AppendMessage(ctx, sess.ID, api.Message{ID: "m1", Role: api.RoleUser, Content: "hi", CreatedAt: time.Now().UTC()})
	_ = s.AppendMessage(ctx, sess.ID, api.Message{ID: "m2", Role: api.RoleAssistant, Content: "hello", CreatedAt: time.Now().UTC()})

	if err := s.ClearMessages(ctx, sess.ID); err != nil {
		t.Fatalf("clear messages: %v", err)
	}

	msgs, _ := s.GetMessages(ctx, sess.ID, 0)
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}
}

func TestSQLite_ReplaceMessages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	sess, _ := s.CreateSession(ctx, "/tmp/proj")
	orig1 := api.Message{ID: "m1", Role: api.RoleUser, Content: "hello", CreatedAt: time.Now().UTC()}
	orig2 := api.Message{ID: "m2", Role: api.RoleAssistant, Content: "hi", CreatedAt: time.Now().UTC().Add(time.Millisecond)}
	if err := s.AppendMessage(ctx, sess.ID, orig1); err != nil {
		t.Fatalf("append message 1: %v", err)
	}
	if err := s.AppendMessage(ctx, sess.ID, orig2); err != nil {
		t.Fatalf("append message 2: %v", err)
	}

	t.Run("happy path", func(t *testing.T) {
		new1 := api.Message{ID: "n1", Role: api.RoleSystem, Content: "system", CreatedAt: time.Now().UTC()}
		new2 := api.Message{ID: "n2", Role: api.RoleUser, Content: "user", CreatedAt: time.Now().UTC().Add(time.Millisecond)}
		if err := s.ReplaceMessages(ctx, sess.ID, []api.Message{new1, new2}); err != nil {
			t.Fatalf("replace messages: %v", err)
		}

		msgs, err := s.GetMessages(ctx, sess.ID, 0)
		if err != nil {
			t.Fatalf("get messages: %v", err)
		}
		if len(msgs) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(msgs))
		}
		if msgs[0].ID != new1.ID || msgs[0].Role != new1.Role || msgs[0].Content != new1.Content {
			t.Errorf("first message = %+v, want %+v", msgs[0], new1)
		}
		if msgs[1].ID != new2.ID || msgs[1].Role != new2.Role || msgs[1].Content != new2.Content {
			t.Errorf("second message = %+v, want %+v", msgs[1], new2)
		}
	})

	t.Run("empty slice clears messages", func(t *testing.T) {
		if err := s.ReplaceMessages(ctx, sess.ID, []api.Message{}); err != nil {
			t.Fatalf("replace messages with empty slice: %v", err)
		}

		msgs, err := s.GetMessages(ctx, sess.ID, 0)
		if err != nil {
			t.Fatalf("get messages: %v", err)
		}
		if len(msgs) != 0 {
			t.Fatalf("expected 0 messages, got %d", len(msgs))
		}
	})

	t.Run("rollback restores original messages", func(t *testing.T) {
		// Seed fresh original messages so the rollback assertion is meaningful.
		if err := s.ReplaceMessages(ctx, sess.ID, []api.Message{orig1, orig2}); err != nil {
			t.Fatalf("seed original messages: %v", err)
		}

		// Two new messages sharing the same ID trigger a PRIMARY KEY conflict
		// inside the INSERT loop, forcing a rollback.
		bad := []api.Message{
			{ID: "dup", Role: api.RoleUser, Content: "first", CreatedAt: time.Now().UTC()},
			{ID: "dup", Role: api.RoleAssistant, Content: "second", CreatedAt: time.Now().UTC().Add(time.Millisecond)},
		}
		if err := s.ReplaceMessages(ctx, sess.ID, bad); err == nil {
			t.Fatal("expected error for duplicate IDs, got nil")
		}

		msgs, err := s.GetMessages(ctx, sess.ID, 0)
		if err != nil {
			t.Fatalf("get messages: %v", err)
		}
		if len(msgs) != 2 {
			t.Fatalf("expected 2 messages after rollback, got %d", len(msgs))
		}
		if msgs[0].ID != orig1.ID || msgs[1].ID != orig2.ID {
			t.Errorf("messages after rollback = %+v, want original", msgs)
		}
	})
}

func TestSQLite_SaveAndGetTurns(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	sess, _ := s.CreateSession(ctx, "/tmp/proj")
	now := time.Now().UTC()
	turn := api.Turn{
		ID:        "turn-1",
		State:     api.TurnThinking,
		Input:     "input",
		Response:  "response",
		StartedAt: now,
	}
	if err := s.SaveTurn(ctx, sess.ID, turn); err != nil {
		t.Fatalf("save turn: %v", err)
	}

	turns, err := s.GetTurns(ctx, sess.ID, 0)
	if err != nil {
		t.Fatalf("get turns: %v", err)
	}
	if len(turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(turns))
	}
	if turns[0].ID != turn.ID {
		t.Errorf("turn id = %q, want %q", turns[0].ID, turn.ID)
	}
	if turns[0].State != turn.State {
		t.Errorf("turn state = %d, want %d", turns[0].State, turn.State)
	}
	if turns[0].Input != turn.Input {
		t.Errorf("turn input = %q, want %q", turns[0].Input, turn.Input)
	}
	if turns[0].Response != turn.Response {
		t.Errorf("turn response = %q, want %q", turns[0].Response, turn.Response)
	}
}

func TestSQLite_SaveTurn_Upsert(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	sess, _ := s.CreateSession(ctx, "/tmp/proj")
	turn := api.Turn{
		ID:        "turn-1",
		State:     api.TurnThinking,
		Input:     "input",
		Response:  "",
		StartedAt: time.Now().UTC(),
	}
	if err := s.SaveTurn(ctx, sess.ID, turn); err != nil {
		t.Fatalf("save turn: %v", err)
	}

	turn.State = api.TurnIdle
	turn.Response = "done"
	ended := time.Now().UTC()
	turn.EndedAt = &ended
	if err := s.SaveTurn(ctx, sess.ID, turn); err != nil {
		t.Fatalf("save turn update: %v", err)
	}

	turns, _ := s.GetTurns(ctx, sess.ID, 0)
	if len(turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(turns))
	}
	if turns[0].State != api.TurnIdle {
		t.Errorf("state = %d, want %d", turns[0].State, api.TurnIdle)
	}
	if turns[0].Response != "done" {
		t.Errorf("response = %q, want %q", turns[0].Response, "done")
	}
	if turns[0].EndedAt == nil {
		t.Fatal("expected ended_at to be set")
	}
}

func TestSQLite_SaveTurn_WithToolCallsAndResults(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	sess, _ := s.CreateSession(ctx, "/tmp/proj")
	turn := api.Turn{
		ID:    "turn-1",
		State: api.TurnToolCalls,
		ToolCalls: []api.ToolCall{
			{ID: "tc1", Name: "read_file", Arguments: `{"path":"a.go"}`},
		},
		Results: []api.ToolResult{
			{CallID: "tc1", Name: "read_file", Output: "package main"},
		},
		StartedAt: time.Now().UTC(),
	}
	if err := s.SaveTurn(ctx, sess.ID, turn); err != nil {
		t.Fatalf("save turn: %v", err)
	}

	turns, _ := s.GetTurns(ctx, sess.ID, 0)
	if len(turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(turns))
	}
	if len(turns[0].ToolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(turns[0].ToolCalls))
	}
	if len(turns[0].Results) != 1 {
		t.Errorf("expected 1 result, got %d", len(turns[0].Results))
	}
	if turns[0].Results[0].Output != "package main" {
		t.Errorf("result output = %q, want %q", turns[0].Results[0].Output, "package main")
	}
}

func TestSQLite_DBFilePermissions(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	s, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	defer s.Close()

	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("stat db file: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("db file permissions = %o, want %o", info.Mode().Perm(), 0600)
	}
}

func TestSQLite_Close(t *testing.T) {
	s, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestSQLite_PathWithSpaceAndQuestionMark(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	dbDir := filepath.Join(tmpDir, "a?b")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		t.Fatalf("create db dir: %v", err)
	}
	dbPath := filepath.Join(dbDir, "c d.db")

	s, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	defer s.Close()

	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("db file not found at %q: %v", dbPath, err)
	}

	ctx := context.Background()
	sess, err := s.CreateSession(ctx, "/tmp/proj")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if sess.ID == "" {
		t.Error("expected session ID to be set")
	}
}

func TestSQLiteDSN(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		dbPath string
		want   string
	}{
		{
			name:   "memory",
			dbPath: ":memory:",
			want:   "file:kimi-mem?mode=memory&cache=shared&_pragma=busy_timeout%285000%29&_pragma=foreign_keys%281%29",
		},
		{
			name:   "absolute path",
			dbPath: "/tmp/test.db",
			want:   "file:///tmp/test.db?_pragma=busy_timeout%285000%29&_pragma=foreign_keys%281%29&_pragma=journal_mode%28WAL%29&_pragma=synchronous%28NORMAL%29",
		},
		{
			name:   "relative path",
			dbPath: "relative.db",
			want:   "file:relative.db?_pragma=busy_timeout%285000%29&_pragma=foreign_keys%281%29&_pragma=journal_mode%28WAL%29&_pragma=synchronous%28NORMAL%29",
		},
		{
			name:   "path with spaces",
			dbPath: "/tmp/path with spaces.db",
			want:   "file:///tmp/path%20with%20spaces.db?_pragma=busy_timeout%285000%29&_pragma=foreign_keys%281%29&_pragma=journal_mode%28WAL%29&_pragma=synchronous%28NORMAL%29",
		},
		{
			name:   "path with question mark",
			dbPath: "/tmp/path?query.db",
			want:   "file:///tmp/path%3Fquery.db?_pragma=busy_timeout%285000%29&_pragma=foreign_keys%281%29&_pragma=journal_mode%28WAL%29&_pragma=synchronous%28NORMAL%29",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := sqliteDSN(tt.dbPath)
			if got != tt.want {
				t.Errorf("sqliteDSN(%q) = %q, want %q", tt.dbPath, got, tt.want)
			}
		})
	}
}

func TestSQLite_Concurrency(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	sess, err := s.CreateSession(ctx, "/tmp/proj")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	const nReplace = 8
	const nClear = 4
	const nAppend = 16
	const nTurns = 8

	// Phase 1: stress ReplaceMessages and ClearMessages concurrently.
	// The final effect of this phase is that the messages table ends empty,
	// but the operations contend for the single WAL writer slot.
	var wg sync.WaitGroup
	errCh := make(chan error, nReplace+nClear)

	for i := 0; i < nReplace; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			msg := api.Message{
				ID:        fmt.Sprintf("replace-%d", idx),
				Role:      api.RoleUser,
				Content:   fmt.Sprintf("replaced-%d", idx),
				CreatedAt: time.Now().UTC(),
			}
			if err := s.ReplaceMessages(ctx, sess.ID, []api.Message{msg}); err != nil {
				errCh <- fmt.Errorf("replace %d: %w", idx, err)
			}
		}(i)
	}
	for i := 0; i < nClear; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if err := s.ClearMessages(ctx, sess.ID); err != nil {
				errCh <- fmt.Errorf("clear %d: %w", idx, err)
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("phase 1 error: %v", err)
	}

	// Ensure the messages table is empty before phase 2 so the final count is
	// deterministic regardless of which phase-1 operation won the race.
	if err := s.ClearMessages(ctx, sess.ID); err != nil {
		t.Fatalf("final clear after phase 1: %v", err)
	}

	// Phase 2: concurrent AppendMessage and SaveTurn writers.
	// Because phase 1 is done, the final message count is deterministic.
	errCh = make(chan error, nAppend+nTurns)
	wg = sync.WaitGroup{}

	for i := 0; i < nAppend; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			msg := api.Message{
				ID:        fmt.Sprintf("append-%d", idx),
				Role:      api.RoleUser,
				Content:   fmt.Sprintf("content-%d", idx),
				CreatedAt: time.Now().UTC(),
			}
			if err := s.AppendMessage(ctx, sess.ID, msg); err != nil {
				errCh <- fmt.Errorf("append %d: %w", idx, err)
			}
		}(i)
	}
	for i := 0; i < nTurns; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			turn := api.Turn{
				ID:        fmt.Sprintf("turn-%d", idx),
				State:     api.TurnIdle,
				Input:     fmt.Sprintf("input-%d", idx),
				StartedAt: time.Now().UTC(),
			}
			if err := s.SaveTurn(ctx, sess.ID, turn); err != nil {
				errCh <- fmt.Errorf("save turn %d: %w", idx, err)
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("phase 2 error: %v", err)
	}

	msgs, err := s.GetMessages(ctx, sess.ID, 0)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(msgs) != nAppend {
		t.Fatalf("messages = %d, want %d", len(msgs), nAppend)
	}
	// Verify each append message survived deterministically.
	seen := make(map[string]bool, len(msgs))
	for _, m := range msgs {
		seen[m.ID] = true
	}
	for i := 0; i < nAppend; i++ {
		id := fmt.Sprintf("append-%d", i)
		if !seen[id] {
			t.Errorf("missing message %s", id)
		}
	}

	turns, err := s.GetTurns(ctx, sess.ID, 0)
	if err != nil {
		t.Fatalf("get turns: %v", err)
	}
	if len(turns) != nTurns {
		t.Fatalf("turns = %d, want %d", len(turns), nTurns)
	}
}

func TestSQLite_ErrorPropagation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	// AppendMessage to nonexistent session should fail (FK violation).
	msg := api.Message{
		ID:        "msg-1",
		Role:      api.RoleUser,
		Content:   "hello",
		CreatedAt: time.Now().UTC(),
	}
	err := s.AppendMessage(ctx, "nonexistent", msg)
	if err == nil {
		t.Fatal("expected error appending message to nonexistent session")
	}
}

func TestGenerateID_Unique(t *testing.T) {
	ids := make(map[string]struct{})
	for i := 0; i < 100; i++ {
		id := idgen.GenerateID()
		if _, ok := ids[id]; ok {
			t.Fatalf("duplicate id generated: %s", id)
		}
		ids[id] = struct{}{}
	}
}

func TestSQLite_NilEndedAt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	sess, _ := s.CreateSession(ctx, "/tmp/proj")
	turn := api.Turn{
		ID:        "turn-1",
		State:     api.TurnIdle,
		StartedAt: time.Now().UTC(),
	}
	if err := s.SaveTurn(ctx, sess.ID, turn); err != nil {
		t.Fatalf("save turn: %v", err)
	}

	turns, _ := s.GetTurns(ctx, sess.ID, 0)
	if len(turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(turns))
	}
	if turns[0].EndedAt != nil {
		t.Error("expected nil ended_at")
	}
}

func TestSQLite_GetSession_MessagesEmpty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	sess, _ := s.CreateSession(ctx, "/tmp/proj")
	got, _ := s.GetSession(ctx, sess.ID)
	if got.Messages == nil {
		t.Error("expected Messages to be non-nil empty slice")
	}
}

func TestSQLite_ListSessions_Ordering(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	s1 := createSessionAt(t, s, "/tmp/proj", base, base)
	s2 := createSessionAt(t, s, "/tmp/proj", base.Add(time.Second), base.Add(time.Second))
	s3 := createSessionAt(t, s, "/tmp/proj", base.Add(2*time.Second), base.Add(2*time.Second))

	// Update s1 to move it to the top.
	s1.Name = "bumped"
	_ = s.UpdateSession(ctx, s1)

	list, _ := s.ListSessions(ctx, "/tmp/proj", 0)
	if len(list) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(list))
	}
	if list[0].ID != s1.ID {
		t.Errorf("first = %q, want %q", list[0].ID, s1.ID)
	}
	if list[1].ID != s3.ID {
		t.Errorf("second = %q, want %q", list[1].ID, s3.ID)
	}
	if list[2].ID != s2.ID {
		t.Errorf("third = %q, want %q", list[2].ID, s2.ID)
	}
}

func TestSQLite_TurnErrorField(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	sess, _ := s.CreateSession(ctx, "/tmp/proj")
	turn := api.Turn{
		ID:        "turn-1",
		State:     api.TurnError,
		Error:     "something went wrong",
		StartedAt: time.Now().UTC(),
	}
	if err := s.SaveTurn(ctx, sess.ID, turn); err != nil {
		t.Fatalf("save turn: %v", err)
	}

	turns, _ := s.GetTurns(ctx, sess.ID, 0)
	if len(turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(turns))
	}
	if turns[0].Error != turn.Error {
		t.Errorf("error = %q, want %q", turns[0].Error, turn.Error)
	}
}

func TestSQLite_SessionPathIsolation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	s1, _ := s.CreateSession(ctx, "/tmp/a")
	s2, _ := s.CreateSession(ctx, "/tmp/b")

	listA, _ := s.ListSessions(ctx, "/tmp/a", 0)
	if len(listA) != 1 || listA[0].ID != s1.ID {
		t.Errorf("listA = %+v, want session %s", listA, s1.ID)
	}

	listB, _ := s.ListSessions(ctx, "/tmp/b", 0)
	if len(listB) != 1 || listB[0].ID != s2.ID {
		t.Errorf("listB = %+v, want session %s", listB, s2.ID)
	}
}

func TestSQLite_GetLastSession_Errors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	_, err := s.GetLastSession(ctx, "/empty")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got %T: %v", err, err)
	}
}

func TestSQLite_EmptyPathValidation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	_, err := s.GetLastSession(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty path in GetLastSession")
	}
	if !strings.Contains(err.Error(), "path is required") {
		t.Fatalf("expected 'path is required' error, got %q", err.Error())
	}

	_, err = s.ListSessions(ctx, "", 0)
	if err == nil {
		t.Fatal("expected error for empty path in ListSessions")
	}
	if !strings.Contains(err.Error(), "path is required") {
		t.Fatalf("expected 'path is required' error, got %q", err.Error())
	}
}

func TestSQLite_AppendMessage_BumpsUpdatedAt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	sessA := createSessionAt(t, s, "/tmp/proj", base, base)
	_ = createSessionAt(t, s, "/tmp/proj", base.Add(time.Second), base.Add(time.Second))

	msg := api.Message{ID: "m1", Role: api.RoleUser, Content: "hi", CreatedAt: time.Now().UTC()}
	if err := s.AppendMessage(ctx, sessA.ID, msg); err != nil {
		t.Fatalf("append message: %v", err)
	}

	got, err := s.GetLastSession(ctx, "/tmp/proj")
	if err != nil {
		t.Fatalf("get last session: %v", err)
	}
	if got.ID != sessA.ID {
		t.Errorf("last session = %q, want %q", got.ID, sessA.ID)
	}
}

func TestSQLite_SaveTurn_BumpsUpdatedAt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	sessA := createSessionAt(t, s, "/tmp/proj", base, base)
	_ = createSessionAt(t, s, "/tmp/proj", base.Add(time.Second), base.Add(time.Second))

	turn := api.Turn{ID: "t1", State: api.TurnIdle, StartedAt: time.Now().UTC()}
	if err := s.SaveTurn(ctx, sessA.ID, turn); err != nil {
		t.Fatalf("save turn: %v", err)
	}

	got, err := s.GetLastSession(ctx, "/tmp/proj")
	if err != nil {
		t.Fatalf("get last session: %v", err)
	}
	if got.ID != sessA.ID {
		t.Errorf("last session = %q, want %q", got.ID, sessA.ID)
	}
}

func TestSQLite_GetMessages_NoLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)
	sess, _ := s.CreateSession(ctx, "/tmp/proj")

	// Insert more than the old default cap of 1000.
	for i := 0; i < 1005; i++ {
		msg := api.Message{
			ID:        fmt.Sprintf("msg-%d", i),
			Role:      api.RoleUser,
			Content:   fmt.Sprintf("content %d", i),
			CreatedAt: time.Now().UTC().Add(time.Duration(i) * time.Millisecond),
		}
		if err := s.AppendMessage(ctx, sess.ID, msg); err != nil {
			t.Fatalf("append message %d: %v", i, err)
		}
	}

	msgs, err := s.GetMessages(ctx, sess.ID, 0)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(msgs) != 1005 {
		t.Fatalf("expected 1005 messages, got %d", len(msgs))
	}
	// Verify chronological order.
	for i := 1; i < len(msgs); i++ {
		if msgs[i].CreatedAt.Before(msgs[i-1].CreatedAt) {
			t.Errorf("messages out of order at index %d", i)
		}
	}
}

func TestSQLite_MigrationRunner(t *testing.T) {
	t.Parallel()

	t.Run("advances user_version on fresh db", func(t *testing.T) {
		t.Parallel()
		s, err := NewSQLite(filepath.Join(t.TempDir(), "test.db"))
		if err != nil {
			t.Fatalf("new sqlite: %v", err)
		}
		defer s.Close()

		var version int
		if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
			t.Fatalf("read user_version: %v", err)
		}
		if version != 1 {
			t.Errorf("user_version = %d, want 1", version)
		}
	})

	t.Run("fake migration applies once", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")
		migrationsDir := filepath.Join(tmpDir, "migrations")
		if err := os.MkdirAll(migrationsDir, 0755); err != nil {
			t.Fatalf("create migrations dir: %v", err)
		}

		// Seed a real 001 and a fake 002.
		initialSQL, err := os.ReadFile("migrations/001_initial.sql")
		if err != nil {
			t.Fatalf("read initial migration: %v", err)
		}
		if err := os.WriteFile(filepath.Join(migrationsDir, "001_initial.sql"), initialSQL, 0644); err != nil {
			t.Fatalf("write initial migration: %v", err)
		}
		fake002 := `CREATE TABLE migration_test (id TEXT PRIMARY KEY);`
		if err := os.WriteFile(filepath.Join(migrationsDir, "002_test.sql"), []byte(fake002), 0644); err != nil {
			t.Fatalf("write fake migration: %v", err)
		}

		// First open: migrations should run.
		db, err := sql.Open("sqlite", sqliteDSN(dbPath))
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		if err := runMigrations(db, os.DirFS(migrationsDir), "."); err != nil {
			t.Fatalf("run migrations: %v", err)
		}

		var version int
		if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
			t.Fatalf("read user_version: %v", err)
		}
		if version != 2 {
			t.Errorf("user_version after first run = %d, want 2", version)
		}

		var count int
		if err := db.QueryRow(`SELECT count(*) FROM migration_test`).Scan(&count); err != nil {
			t.Errorf("migration_test table missing: %v", err)
		}
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}

		// Replace 002 with a statement that would fail if re-run, proving it is skipped.
		failing002 := `CREATE TABLE migration_test (id TEXT PRIMARY KEY);`
		if err := os.WriteFile(filepath.Join(migrationsDir, "002_test.sql"), []byte(failing002), 0644); err != nil {
			t.Fatalf("write failing migration: %v", err)
		}

		db2, err := sql.Open("sqlite", sqliteDSN(dbPath))
		if err != nil {
			t.Fatalf("reopen sqlite: %v", err)
		}
		defer db2.Close()

		if err := runMigrations(db2, os.DirFS(migrationsDir), "."); err != nil {
			t.Fatalf("re-run migrations: %v", err)
		}

		var version2 int
		if err := db2.QueryRow(`PRAGMA user_version`).Scan(&version2); err != nil {
			t.Fatalf("read user_version: %v", err)
		}
		if version2 != 2 {
			t.Errorf("user_version after re-run = %d, want 2", version2)
		}
	})
}
