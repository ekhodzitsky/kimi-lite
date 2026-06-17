package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
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

func TestSQLite_ListSessions_LastPrompt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	sess, err := s.CreateSession(ctx, "/tmp/proj")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := s.AppendMessage(ctx, sess.ID, api.Message{ID: "m1", Role: api.RoleAssistant, Content: "hi", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("append assistant message: %v", err)
	}
	if err := s.AppendMessage(ctx, sess.ID, api.Message{ID: "m2", Role: api.RoleUser, Content: "last question", CreatedAt: time.Now().UTC().Add(time.Second)}); err != nil {
		t.Fatalf("append user message: %v", err)
	}

	list, err := s.ListSessions(ctx, "/tmp/proj", 0)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 session, got %d", len(list))
	}
	if list[0].LastPrompt != "last question" {
		t.Errorf("last prompt = %q, want %q", list[0].LastPrompt, "last question")
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

func TestSQLite_UpdateSession_EmptyID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	sess := &api.Session{ID: "", Name: "name", Path: "/tmp/proj"}
	if err := s.UpdateSession(ctx, sess); err == nil {
		t.Fatal("expected error for empty session ID")
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

func TestSQLite_AppendMessage_WithImageContentParts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	sess, _ := s.CreateSession(ctx, "/tmp/proj")
	msg := api.Message{
		ID:      "msg-1",
		Role:    api.RoleTool,
		Content: "[image output]",
		ContentParts: []api.ContentPart{
			{Type: api.ContentPartImageURL, ImageURL: &api.ImageURL{URL: "data:image/png;base64,abcd", Detail: "low"}},
		},
		ToolCallID: "tc1",
		CreatedAt:  time.Now().UTC(),
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
	if len(msgs[0].ContentParts) != 1 {
		t.Fatalf("expected 1 content part, got %d", len(msgs[0].ContentParts))
	}
	if msgs[0].ContentParts[0].Type != api.ContentPartImageURL {
		t.Errorf("content part type = %q, want image_url", msgs[0].ContentParts[0].Type)
	}
	if msgs[0].ContentParts[0].ImageURL == nil || msgs[0].ContentParts[0].ImageURL.URL != "data:image/png;base64,abcd" {
		t.Errorf("image url = %v, want data:image/png;base64,abcd", msgs[0].ContentParts[0].ImageURL)
	}
}

func TestSQLite_ClearMessages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	sess, _ := s.CreateSession(ctx, "/tmp/proj")
	_ = s.AppendMessage(ctx, sess.ID, api.Message{ID: "m1", Role: api.RoleUser, Content: "hi", CreatedAt: time.Now().UTC()})
	_ = s.AppendMessage(ctx, sess.ID, api.Message{ID: "m2", Role: api.RoleAssistant, Content: "hello", CreatedAt: time.Now().UTC()})

	time.Sleep(10 * time.Millisecond)
	if err := s.ClearMessages(ctx, sess.ID); err != nil {
		t.Fatalf("clear messages: %v", err)
	}

	msgs, _ := s.GetMessages(ctx, sess.ID, 0)
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}

	got, err := s.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if !got.UpdatedAt.After(sess.UpdatedAt) {
		t.Errorf("updated_at did not advance: %v -> %v", sess.UpdatedAt, got.UpdatedAt)
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

	t.Run("preserves all message fields", func(t *testing.T) {
		createdAt := time.Date(2024, 6, 15, 12, 30, 0, 0, time.UTC)
		replaced := []api.Message{
			{
				ID:        "full-1",
				Role:      api.RoleAssistant,
				Content:   "calling tool",
				CreatedAt: createdAt,
				ToolCalls: []api.ToolCall{
					{ID: "tc1", Name: "read_file", Arguments: `{"path":"foo.txt"}`},
					{ID: "tc2", Name: "grep", Arguments: `{"pattern":"bar"}`},
				},
			},
			{
				ID:         "full-2",
				Role:       api.RoleTool,
				Content:    "tool result",
				ToolCallID: "tc1",
				CreatedAt:  createdAt.Add(time.Second),
			},
		}
		if err := s.ReplaceMessages(ctx, sess.ID, replaced); err != nil {
			t.Fatalf("replace messages: %v", err)
		}

		msgs, err := s.GetMessages(ctx, sess.ID, 0)
		if err != nil {
			t.Fatalf("get messages: %v", err)
		}
		if len(msgs) != len(replaced) {
			t.Fatalf("expected %d messages, got %d", len(replaced), len(msgs))
		}

		got := msgs[0]
		if got.ID != "full-1" || got.Role != api.RoleAssistant || got.Content != "calling tool" {
			t.Errorf("assistant message mismatch: %+v", got)
		}
		if !got.CreatedAt.Equal(createdAt) {
			t.Errorf("assistant created_at = %v, want %v", got.CreatedAt, createdAt)
		}
		if len(got.ToolCalls) != 2 {
			t.Fatalf("expected 2 tool calls, got %d", len(got.ToolCalls))
		}
		if got.ToolCalls[0].ID != "tc1" || got.ToolCalls[0].Name != "read_file" || got.ToolCalls[0].Arguments != `{"path":"foo.txt"}` {
			t.Errorf("tool call 0 mismatch: %+v", got.ToolCalls[0])
		}
		if got.ToolCalls[1].ID != "tc2" || got.ToolCalls[1].Name != "grep" || got.ToolCalls[1].Arguments != `{"pattern":"bar"}` {
			t.Errorf("tool call 1 mismatch: %+v", got.ToolCalls[1])
		}

		gotTool := msgs[1]
		if gotTool.ID != "full-2" || gotTool.Role != api.RoleTool || gotTool.Content != "tool result" {
			t.Errorf("tool message mismatch: %+v", gotTool)
		}
		if gotTool.ToolCallID != "tc1" {
			t.Errorf("tool_call_id = %q, want %q", gotTool.ToolCallID, "tc1")
		}
		if !gotTool.CreatedAt.Equal(createdAt.Add(time.Second)) {
			t.Errorf("tool created_at = %v, want %v", gotTool.CreatedAt, createdAt.Add(time.Second))
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
			want:   "file:kimi-mem-",
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
		{
			name:   "windows path separators normalized",
			dbPath: `C:\Users\test\data.db`,
			want:   "file:///C:/Users/test/data.db?_pragma=busy_timeout%285000%29&_pragma=foreign_keys%281%29&_pragma=journal_mode%28WAL%29&_pragma=synchronous%28NORMAL%29",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := sqliteDSN(tt.dbPath)
			switch tt.name {
			case "memory":
				if !strings.HasPrefix(got, tt.want) {
					t.Errorf("sqliteDSN(%q) = %q, want prefix %q", tt.dbPath, got, tt.want)
				}
				if !strings.Contains(got, "mode=memory") || !strings.Contains(got, "cache=shared") {
					t.Errorf("sqliteDSN(%q) = %q, want mode=memory and cache=shared", tt.dbPath, got)
				}
			case "windows path separators normalized":
				if runtime.GOOS != "windows" {
					// filepath.ToSlash only converts OS separators; skip exact
					// assertion on platforms where backslash is a regular filename
					// character.
					return
				}
				if got != tt.want {
					t.Errorf("sqliteDSN(%q) = %q, want %q", tt.dbPath, got, tt.want)
				}
			default:
				if got != tt.want {
					t.Errorf("sqliteDSN(%q) = %q, want %q", tt.dbPath, got, tt.want)
				}
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

func TestSQLite_DeterministicOrderingByID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	sess, _ := s.CreateSession(ctx, "/tmp/proj")
	now := time.Now().UTC()

	// Messages with identical created_at must order by id.
	m2 := api.Message{ID: "b", Role: api.RoleUser, Content: "b", CreatedAt: now}
	m1 := api.Message{ID: "a", Role: api.RoleUser, Content: "a", CreatedAt: now}
	_ = s.AppendMessage(ctx, sess.ID, m2)
	_ = s.AppendMessage(ctx, sess.ID, m1)
	msgs, err := s.GetMessages(ctx, sess.ID, 0)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].ID != "a" || msgs[1].ID != "b" {
		t.Errorf("message order = %v, want [a b]", []string{msgs[0].ID, msgs[1].ID})
	}

	// Turns with identical started_at must order by id.
	t2 := api.Turn{ID: "z", State: api.TurnIdle, StartedAt: now}
	t1 := api.Turn{ID: "m", State: api.TurnIdle, StartedAt: now}
	_ = s.SaveTurn(ctx, sess.ID, t2)
	_ = s.SaveTurn(ctx, sess.ID, t1)
	turns, err := s.GetTurns(ctx, sess.ID, 0)
	if err != nil {
		t.Fatalf("get turns: %v", err)
	}
	if len(turns) != 2 {
		t.Fatalf("expected 2 turns, got %d", len(turns))
	}
	if turns[0].ID != "m" || turns[1].ID != "z" {
		t.Errorf("turn order = %v, want [m z]", []string{turns[0].ID, turns[1].ID})
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

	// Insert more than the safe default cap of 1000.
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
	if len(msgs) != 1000 {
		t.Fatalf("expected 1000 messages (default limit), got %d", len(msgs))
	}
	// Verify chronological order.
	for i := 1; i < len(msgs); i++ {
		if msgs[i].CreatedAt.Before(msgs[i-1].CreatedAt) {
			t.Errorf("messages out of order at index %d", i)
		}
	}

	// Explicit large limit should return all messages.
	msgs, err = s.GetMessages(ctx, sess.ID, 10000)
	if err != nil {
		t.Fatalf("get messages with large limit: %v", err)
	}
	if len(msgs) != 1005 {
		t.Fatalf("expected 1005 messages with explicit limit, got %d", len(msgs))
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
		if version != 2 {
			t.Errorf("user_version = %d, want 2", version)
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

	t.Run("query user_version error", func(t *testing.T) {
		t.Parallel()
		db := newPartialErrDB(t, partialErrDriver{
			queryErr: func(query string) error {
				if strings.Contains(query, "user_version") {
					return errors.New("user_version error")
				}
				return nil
			},
		})
		defer db.Close()

		if err := runMigrations(db, migrationFiles, "migrations"); err == nil {
			t.Fatal("expected error when user_version query fails")
		}
	})

	t.Run("begin error", func(t *testing.T) {
		t.Parallel()
		db := newPartialErrDB(t, partialErrDriver{
			beginErr: errors.New("begin error"),
		})
		defer db.Close()

		if err := runMigrations(db, migrationFiles, "migrations"); err == nil {
			t.Fatal("expected error when begin fails")
		}
	})

	t.Run("set user_version error", func(t *testing.T) {
		t.Parallel()
		db := newPartialErrDB(t, partialErrDriver{
			execErr: func(query string) error {
				if strings.Contains(query, "PRAGMA user_version") {
					return errors.New("set user_version error")
				}
				return nil
			},
		})
		defer db.Close()

		if err := runMigrations(db, migrationFiles, "migrations"); err == nil {
			t.Fatal("expected error when setting user_version fails")
		}
	})

	t.Run("commit error", func(t *testing.T) {
		t.Parallel()
		db := newPartialErrDB(t, partialErrDriver{
			commitErr: errors.New("commit error"),
		})
		defer db.Close()

		if err := runMigrations(db, migrationFiles, "migrations"); err == nil {
			t.Fatal("expected error when commit fails")
		}
	})
}

func TestSQLite_WALSHMPermissions(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	s, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	if _, err := s.CreateSession(ctx, "/tmp/proj"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	for _, suffix := range []string{"", "-wal", "-shm"} {
		p := dbPath + suffix
		info, err := os.Stat(p)
		if err != nil {
			if suffix == "" {
				t.Fatalf("stat db file: %v", err)
			}
			continue
		}
		if info.Mode().Perm() != 0600 {
			t.Errorf("%s permissions = %o, want %o", p, info.Mode().Perm(), 0600)
		}
	}
}

func TestSQLite_ReplaceMessages_BumpsUpdatedAt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	sess, err := s.CreateSession(ctx, "/tmp/proj")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Wait a hair so the replacement timestamp is strictly later.
	time.Sleep(10 * time.Millisecond)

	newMsgs := []api.Message{
		{ID: "n1", Role: api.RoleSystem, Content: "system", CreatedAt: time.Now().UTC()},
		{ID: "n2", Role: api.RoleUser, Content: "user", CreatedAt: time.Now().UTC().Add(time.Millisecond)},
	}
	if err := s.ReplaceMessages(ctx, sess.ID, newMsgs); err != nil {
		t.Fatalf("replace messages: %v", err)
	}

	got, err := s.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if !got.UpdatedAt.After(sess.UpdatedAt) {
		t.Errorf("updated_at did not advance: %v -> %v", sess.UpdatedAt, got.UpdatedAt)
	}
}

func TestSQLite_MigrateTurnsStateColumn(t *testing.T) {
	t.Parallel()

	run := func(t *testing.T, stateType string) {
		t.Helper()
		dbPath := filepath.Join(t.TempDir(), "migrate.db")

		db, err := sql.Open("sqlite", sqliteDSN(dbPath))
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		if err := db.Ping(); err != nil {
			t.Fatalf("ping sqlite: %v", err)
		}
		defer db.Close()

		stmts := []string{
			`CREATE TABLE sessions (id TEXT PRIMARY KEY);`,
			fmt.Sprintf(`CREATE TABLE turns (
				id TEXT NOT NULL,
				session_id TEXT NOT NULL,
				state %s NOT NULL,
				input TEXT NOT NULL DEFAULT '',
				response TEXT NOT NULL DEFAULT '',
				tool_calls TEXT,
				results TEXT,
				error TEXT,
				started_at DATETIME NOT NULL,
				ended_at DATETIME,
				PRIMARY KEY (id, session_id),
				FOREIGN KEY (session_id) REFERENCES sessions(id)
			);`, stateType),
			`INSERT INTO sessions (id) VALUES ('s1');`,
			`INSERT INTO turns (id, session_id, state, input, started_at)
			 VALUES ('t1', 's1', 1, 'hello', '2024-01-01 00:00:00');`,
		}
		for i, stmt := range stmts {
			if _, err := db.Exec(stmt); err != nil {
				t.Fatalf("exec stmt %d: %v", i, err)
			}
		}

		if err := migrateTurnsStateColumn(db); err != nil {
			t.Fatalf("migrate turns state column: %v", err)
		}

		var typ string
		if err := db.QueryRow(`SELECT type FROM pragma_table_info('turns') WHERE name = 'state'`).Scan(&typ); err != nil {
			t.Fatalf("read state column type: %v", err)
		}
		if typ != "TEXT" {
			t.Errorf("state column type = %q, want TEXT", typ)
		}

		var stateStr string
		if err := db.QueryRow(`SELECT state FROM turns WHERE id = 't1'`).Scan(&stateStr); err != nil {
			t.Fatalf("read migrated state: %v", err)
		}
		if stateStr != "1" {
			t.Errorf("state value = %q, want %q", stateStr, "1")
		}
	}

	t.Run("INTEGER", func(t *testing.T) { t.Parallel(); run(t, "INTEGER") })
	t.Run("INT", func(t *testing.T) { t.Parallel(); run(t, "INT") })
}

func TestSQLite_Close_NilSafe(t *testing.T) {
	t.Parallel()

	var s *SQLite
	if err := s.Close(); err != nil {
		t.Errorf("nil receiver Close returned error: %v", err)
	}

	s2 := &SQLite{}
	if err := s2.Close(); err != nil {
		t.Errorf("nil db Close returned error: %v", err)
	}
}

func TestSQLite_CountTurns(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	sess, err := s.CreateSession(ctx, "/tmp/proj")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	if _, err := s.CountTurns(ctx, "", api.TurnIdle); err == nil {
		t.Error("expected error for empty session ID")
	}

	count, err := s.CountTurns(ctx, sess.ID, api.TurnIdle)
	if err != nil {
		t.Fatalf("count turns: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}

	for i, state := range []api.TurnState{api.TurnIdle, api.TurnIdle, api.TurnThinking} {
		turn := api.Turn{
			ID:        fmt.Sprintf("turn-%d", i),
			State:     state,
			StartedAt: time.Now().UTC(),
		}
		if err := s.SaveTurn(ctx, sess.ID, turn); err != nil {
			t.Fatalf("save turn %d: %v", i, err)
		}
	}

	count, err = s.CountTurns(ctx, sess.ID, api.TurnIdle)
	if err != nil {
		t.Fatalf("count idle turns: %v", err)
	}
	if count != 2 {
		t.Errorf("idle count = %d, want 2", count)
	}

	count, err = s.CountTurns(ctx, sess.ID, api.TurnThinking)
	if err != nil {
		t.Fatalf("count thinking turns: %v", err)
	}
	if count != 1 {
		t.Errorf("thinking count = %d, want 1", count)
	}
}

func TestParseMigrationVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		want   int
		wantOK bool
	}{
		{"valid", "001_initial.sql", 1, true},
		{"large prefix", "123_migration.sql", 123, true},
		{"no underscore", "initial.sql", 0, false},
		{"non numeric prefix", "abc_initial.sql", 0, false},
		{"zero prefix", "000_initial.sql", 0, false},
		{"negative prefix", "-001_initial.sql", 0, false},
		{"empty", "", 0, false},
		{"only number", "001.sql", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := parseMigrationVersion(tt.input)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("parseMigrationVersion(%q) = (%d, %v), want (%d, %v)", tt.input, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestInitialCapacity(t *testing.T) {
	t.Parallel()

	if got := initialCapacity(100); got != 100 {
		t.Errorf("initialCapacity(100) = %d, want 100", got)
	}
	if got := initialCapacity(0); got != 16 {
		t.Errorf("initialCapacity(0) = %d, want 16", got)
	}
	if got := initialCapacity(-5); got != 16 {
		t.Errorf("initialCapacity(-5) = %d, want 16", got)
	}
}

func TestRunMigrations_Errors(t *testing.T) {
	t.Parallel()

	t.Run("read dir error", func(t *testing.T) {
		t.Parallel()
		db, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		defer db.Close()

		badFS := &failFS{readDirErr: errors.New("boom")}
		if err := runMigrations(db, badFS, "."); err == nil {
			t.Fatal("expected error for read dir failure")
		}
	})

	t.Run("read file error", func(t *testing.T) {
		t.Parallel()
		db, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		defer db.Close()

		tmpDir := t.TempDir()
		migrationsDir := filepath.Join(tmpDir, "migrations")
		if err := os.MkdirAll(migrationsDir, 0755); err != nil {
			t.Fatalf("create migrations dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(migrationsDir, "001_initial.sql"), []byte("SELECT 1;"), 0644); err != nil {
			t.Fatalf("write migration: %v", err)
		}

		badFS := &failFS{
			delegate:    os.DirFS(migrationsDir),
			readFileErr: errors.New("read boom"),
		}
		if err := runMigrations(db, badFS, "."); err == nil {
			t.Fatal("expected error for read file failure")
		}
	})

	t.Run("invalid migration sql", func(t *testing.T) {
		t.Parallel()
		db, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		defer db.Close()

		tmpDir := t.TempDir()
		migrationsDir := filepath.Join(tmpDir, "migrations")
		if err := os.MkdirAll(migrationsDir, 0755); err != nil {
			t.Fatalf("create migrations dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(migrationsDir, "001_bad.sql"), []byte("THIS IS NOT SQL"), 0644); err != nil {
			t.Fatalf("write migration: %v", err)
		}

		if err := runMigrations(db, os.DirFS(migrationsDir), "."); err == nil {
			t.Fatal("expected error for invalid migration SQL")
		}
	})

	t.Run("non sql files ignored", func(t *testing.T) {
		t.Parallel()
		db, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		defer db.Close()

		tmpDir := t.TempDir()
		migrationsDir := filepath.Join(tmpDir, "migrations")
		if err := os.MkdirAll(migrationsDir, 0755); err != nil {
			t.Fatalf("create migrations dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(migrationsDir, "001_initial.sql"), []byte("SELECT 1;"), 0644); err != nil {
			t.Fatalf("write migration: %v", err)
		}
		if err := os.WriteFile(filepath.Join(migrationsDir, "README.md"), []byte("docs"), 0644); err != nil {
			t.Fatalf("write readme: %v", err)
		}
		if err := os.WriteFile(filepath.Join(migrationsDir, "002invalid.sql"), []byte("SELECT 2;"), 0644); err != nil {
			t.Fatalf("write invalid migration: %v", err)
		}

		if err := runMigrations(db, os.DirFS(migrationsDir), "."); err != nil {
			t.Fatalf("run migrations: %v", err)
		}

		var version int
		if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
			t.Fatalf("read user_version: %v", err)
		}
		if version != 1 {
			t.Errorf("user_version = %d, want 1", version)
		}
	})

	t.Run("migrations sorted", func(t *testing.T) {
		t.Parallel()
		db, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		defer db.Close()

		tmpDir := t.TempDir()
		migrationsDir := filepath.Join(tmpDir, "migrations")
		if err := os.MkdirAll(migrationsDir, 0755); err != nil {
			t.Fatalf("create migrations dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(migrationsDir, "002_second.sql"), []byte("CREATE TABLE second (id TEXT PRIMARY KEY);"), 0644); err != nil {
			t.Fatalf("write migration 2: %v", err)
		}
		if err := os.WriteFile(filepath.Join(migrationsDir, "001_first.sql"), []byte("CREATE TABLE first (id TEXT PRIMARY KEY);"), 0644); err != nil {
			t.Fatalf("write migration 1: %v", err)
		}

		if err := runMigrations(db, os.DirFS(migrationsDir), "."); err != nil {
			t.Fatalf("run migrations: %v", err)
		}

		for _, table := range []string{"first", "second"} {
			var count int
			if err := db.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s`, table)).Scan(&count); err != nil {
				t.Errorf("table %s missing: %v", table, err)
			}
		}
	})
}

func TestRunMigrations_VersionValidation(t *testing.T) {
	t.Parallel()

	writeMigration := func(t *testing.T, dir, name, sql string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(sql), 0644); err != nil {
			t.Fatalf("write migration %s: %v", name, err)
		}
	}

	t.Run("gap detected", func(t *testing.T) {
		t.Parallel()
		db, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		defer db.Close()

		tmpDir := t.TempDir()
		migrationsDir := filepath.Join(tmpDir, "migrations")
		if err := os.MkdirAll(migrationsDir, 0755); err != nil {
			t.Fatalf("create migrations dir: %v", err)
		}
		writeMigration(t, migrationsDir, "001_first.sql", "SELECT 1;")
		writeMigration(t, migrationsDir, "003_third.sql", "SELECT 3;")

		if err := runMigrations(db, os.DirFS(migrationsDir), "."); err == nil {
			t.Fatal("expected error for migration gap")
		}
	})

	t.Run("duplicate detected", func(t *testing.T) {
		t.Parallel()
		db, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		defer db.Close()

		tmpDir := t.TempDir()
		migrationsDir := filepath.Join(tmpDir, "migrations")
		if err := os.MkdirAll(migrationsDir, 0755); err != nil {
			t.Fatalf("create migrations dir: %v", err)
		}
		writeMigration(t, migrationsDir, "001_first.sql", "SELECT 1;")
		writeMigration(t, migrationsDir, "001_second.sql", "SELECT 2;")

		if err := runMigrations(db, os.DirFS(migrationsDir), "."); err == nil {
			t.Fatal("expected error for duplicate migration version")
		}
	})
}

func TestMigrateToolCallIDColumn_Errors(t *testing.T) {
	t.Parallel()

	t.Run("messages table missing", func(t *testing.T) {
		t.Parallel()
		db, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		defer db.Close()

		if err := migrateToolCallIDColumn(db); err == nil {
			t.Fatal("expected error when messages table missing")
		}
	})

	t.Run("column already present", func(t *testing.T) {
		t.Parallel()
		db, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		defer db.Close()

		if _, err := db.Exec(`CREATE TABLE messages (id TEXT, tool_call_id TEXT)`); err != nil {
			t.Fatalf("create messages table: %v", err)
		}

		if err := migrateToolCallIDColumn(db); err != nil {
			t.Fatalf("migrate: %v", err)
		}
	})

	t.Run("rows iteration error", func(t *testing.T) {
		t.Parallel()
		db := newErrRowsDB(t, errors.New("rows error"))
		defer db.Close()

		if err := migrateToolCallIDColumn(db); err == nil {
			t.Fatal("expected error when rows iteration fails")
		}
	})

	t.Run("alter table error", func(t *testing.T) {
		t.Parallel()
		db, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		defer db.Close()

		if _, err := db.Exec(`CREATE TABLE messages (id TEXT)`); err != nil {
			t.Fatalf("create messages table: %v", err)
		}
		if _, err := db.Exec(`PRAGMA query_only = 1`); err != nil {
			t.Fatalf("set query_only: %v", err)
		}

		if err := migrateToolCallIDColumn(db); err == nil {
			t.Fatal("expected error when ALTER TABLE fails")
		}
	})
}

func TestMigrateTurnsStateColumn_Errors(t *testing.T) {
	t.Parallel()

	t.Run("query error", func(t *testing.T) {
		t.Parallel()
		db, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		db.Close()

		if err := migrateTurnsStateColumn(db); err == nil {
			t.Fatal("expected error when db is closed")
		}
	})

	t.Run("no migration needed", func(t *testing.T) {
		t.Parallel()
		db, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		defer db.Close()

		if _, err := db.Exec(`CREATE TABLE turns (id TEXT, state TEXT)`); err != nil {
			t.Fatalf("create turns table: %v", err)
		}

		if err := migrateTurnsStateColumn(db); err != nil {
			t.Fatalf("migrate: %v", err)
		}
	})

	t.Run("rows iteration error", func(t *testing.T) {
		t.Parallel()
		db := newErrRowsDB(t, errors.New("rows error"))
		defer db.Close()

		if err := migrateTurnsStateColumn(db); err == nil {
			t.Fatal("expected error when rows iteration fails")
		}
	})

	t.Run("migration sql error", func(t *testing.T) {
		t.Parallel()
		db, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		defer db.Close()

		if _, err := db.Exec(`CREATE TABLE turns (id TEXT, state INTEGER)`); err != nil {
			t.Fatalf("create turns table: %v", err)
		}
		if _, err := db.Exec(`CREATE TABLE turns_old (id TEXT)`); err != nil {
			t.Fatalf("create turns_old table: %v", err)
		}

		if err := migrateTurnsStateColumn(db); err == nil {
			t.Fatal("expected error when migration SQL fails")
		}
	})
}

func TestNewSQLite_Errors(t *testing.T) {
	t.Parallel()

	t.Run("directory as db path", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		_, err := NewSQLite(dir)
		if err == nil {
			t.Fatal("expected error when db path is a directory")
		}
	})

	t.Run("nonexistent parent directory", func(t *testing.T) {
		t.Parallel()
		_, err := NewSQLite(filepath.Join(t.TempDir(), "missing", "test.db"))
		if err == nil {
			t.Fatal("expected error when parent directory missing")
		}
	})

	t.Run("corrupted db file", func(t *testing.T) {
		t.Parallel()
		dbPath := filepath.Join(t.TempDir(), "corrupt.db")
		if err := os.WriteFile(dbPath, []byte("not a sqlite database"), 0644); err != nil {
			t.Fatalf("write corrupt file: %v", err)
		}
		_, err := NewSQLite(dbPath)
		if err == nil {
			t.Fatal("expected error for corrupted db file")
		}
	})

	t.Run("read only parent directory", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if err := os.Chmod(dir, 0555); err != nil {
			t.Fatalf("chmod dir: %v", err)
		}
		defer os.Chmod(dir, 0755)

		dbPath := filepath.Join(dir, "test.db")
		_, err := NewSQLite(dbPath)
		if err == nil {
			t.Fatal("expected error when parent directory is read only")
		}
	})

	t.Run("main db chmod error", func(t *testing.T) {
		t.Parallel()
		dbPath := filepath.Join(t.TempDir(), "test.db")
		_, err := newSQLiteWithOptions(dbPath, withChmod(func(string, os.FileMode) error {
			return errors.New("chmod denied")
		}))
		if err == nil {
			t.Fatal("expected error when main db chmod fails")
		}
	})

	t.Run("wal chmod error", func(t *testing.T) {
		t.Parallel()
		dbPath := filepath.Join(t.TempDir(), "test.db")
		_, err := newSQLiteWithOptions(dbPath, withChmod(func(path string, mode os.FileMode) error {
			if strings.HasSuffix(path, "-wal") || strings.HasSuffix(path, "-shm") {
				return errors.New("wal chmod denied")
			}
			return os.Chmod(path, mode)
		}))
		if err == nil {
			t.Fatal("expected error when wal chmod fails")
		}
	})

	t.Run("migrate tool_call_id fails on virtual messages table", func(t *testing.T) {
		t.Parallel()
		dbPath := filepath.Join(t.TempDir(), "virtual.db")
		db, err := sql.Open("sqlite", sqliteDSN(dbPath))
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		// sessions and turns tables satisfy runMigrations; messages as virtual
		// table cannot be ALTERed, causing migrateToolCallIDColumn to fail.
		stmts := []string{
			`CREATE TABLE sessions (id TEXT PRIMARY KEY, name TEXT, path TEXT, created_at DATETIME, updated_at DATETIME);`,
			`CREATE VIRTUAL TABLE messages USING fts5(id, content);`,
			`CREATE TABLE turns (id TEXT, session_id TEXT, state TEXT, input TEXT, response TEXT, tool_calls TEXT, results TEXT, error TEXT, started_at DATETIME, ended_at DATETIME, PRIMARY KEY (id, session_id));`,
		}
		for _, stmt := range stmts {
			if _, err := db.Exec(stmt); err != nil {
				t.Fatalf("exec stmt: %v", err)
			}
		}
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}

		_, err = NewSQLite(dbPath)
		if err == nil {
			t.Fatal("expected error when migrateToolCallIDColumn fails")
		}
	})

	t.Run("migrate turns state fails when turns_old exists", func(t *testing.T) {
		t.Parallel()
		dbPath := filepath.Join(t.TempDir(), "turnsold.db")
		db, err := sql.Open("sqlite", sqliteDSN(dbPath))
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		stmts := []string{
			`CREATE TABLE sessions (id TEXT PRIMARY KEY, name TEXT, path TEXT, created_at DATETIME, updated_at DATETIME);`,
			`CREATE TABLE messages (id TEXT, session_id TEXT, role TEXT, content TEXT, tool_call_id TEXT, tool_calls TEXT, created_at DATETIME, PRIMARY KEY (id, session_id));`,
			`CREATE TABLE turns_old (id TEXT);`,
			`CREATE TABLE turns (id TEXT, session_id TEXT, state INTEGER, input TEXT, response TEXT, tool_calls TEXT, results TEXT, error TEXT, started_at DATETIME, ended_at DATETIME, PRIMARY KEY (id, session_id));`,
		}
		for _, stmt := range stmts {
			if _, err := db.Exec(stmt); err != nil {
				t.Fatalf("exec stmt: %v", err)
			}
		}
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}

		_, err = NewSQLite(dbPath)
		if err == nil {
			t.Fatal("expected error when migrateTurnsStateColumn fails")
		}
	})

	t.Run("cleanup orphaned turns fails when state column missing", func(t *testing.T) {
		t.Parallel()
		dbPath := filepath.Join(t.TempDir(), "cleanup.db")
		db, err := sql.Open("sqlite", sqliteDSN(dbPath))
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		stmts := []string{
			`CREATE TABLE sessions (id TEXT PRIMARY KEY, name TEXT, path TEXT, created_at DATETIME, updated_at DATETIME);`,
			`CREATE TABLE messages (id TEXT, session_id TEXT, role TEXT, content TEXT, tool_call_id TEXT, tool_calls TEXT, created_at DATETIME, PRIMARY KEY (id, session_id));`,
			`CREATE TABLE turns (id TEXT, session_id TEXT, input TEXT, response TEXT, tool_calls TEXT, results TEXT, error TEXT, started_at DATETIME, ended_at DATETIME, PRIMARY KEY (id, session_id));`,
		}
		for _, stmt := range stmts {
			if _, err := db.Exec(stmt); err != nil {
				t.Fatalf("exec stmt: %v", err)
			}
		}
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}

		_, err = NewSQLite(dbPath)
		if err == nil {
			t.Fatal("expected error when cleanup orphaned turns fails")
		}
	})
}

func TestSQLite_CreateSession_Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	if _, err := s.CreateSession(ctx, ""); err == nil {
		t.Error("expected error for empty path")
	}
}

func TestSQLite_GetSession_Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	if _, err := s.GetSession(ctx, ""); err == nil {
		t.Error("expected error for empty session ID")
	}
}

func TestSQLite_GetLastSession_EmptyPath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	if _, err := s.GetLastSession(ctx, ""); err == nil {
		t.Error("expected error for empty path")
	}
}

func TestSQLite_ListSessions_Limit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	s1 := createSessionAt(t, s, "/tmp/proj", base, base)
	s2 := createSessionAt(t, s, "/tmp/proj", base.Add(time.Second), base.Add(time.Second))

	list, err := s.ListSessions(ctx, "/tmp/proj", 2)
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

func TestSQLite_GetMessages_Limit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	sess, _ := s.CreateSession(ctx, "/tmp/proj")
	for i := 0; i < 5; i++ {
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

	msgs, err := s.GetMessages(ctx, sess.ID, 2)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].ID != "msg-0" {
		t.Errorf("first message = %q, want msg-0", msgs[0].ID)
	}
	if msgs[1].ID != "msg-1" {
		t.Errorf("second message = %q, want msg-1", msgs[1].ID)
	}
}

func TestSQLite_GetTurns_Limit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	sess, _ := s.CreateSession(ctx, "/tmp/proj")
	for i := 0; i < 5; i++ {
		turn := api.Turn{
			ID:        fmt.Sprintf("turn-%d", i),
			State:     api.TurnIdle,
			StartedAt: time.Now().UTC().Add(time.Duration(i) * time.Millisecond),
		}
		if err := s.SaveTurn(ctx, sess.ID, turn); err != nil {
			t.Fatalf("save turn %d: %v", i, err)
		}
	}

	turns, err := s.GetTurns(ctx, sess.ID, 2)
	if err != nil {
		t.Fatalf("get turns: %v", err)
	}
	if len(turns) != 2 {
		t.Fatalf("expected 2 turns, got %d", len(turns))
	}
	if turns[0].ID != "turn-0" {
		t.Errorf("first turn = %q, want turn-0", turns[0].ID)
	}
	if turns[1].ID != "turn-1" {
		t.Errorf("second turn = %q, want turn-1", turns[1].ID)
	}
}

func TestSQLite_DefaultLimits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	sess, _ := s.CreateSession(ctx, "/tmp/proj")

	// GetTurns with limit <= 0 defaults to 1000.
	for i := 0; i < 1005; i++ {
		turn := api.Turn{
			ID:        fmt.Sprintf("turn-%d", i),
			State:     api.TurnIdle,
			StartedAt: time.Now().UTC().Add(time.Duration(i) * time.Millisecond),
		}
		if err := s.SaveTurn(ctx, sess.ID, turn); err != nil {
			t.Fatalf("save turn %d: %v", i, err)
		}
	}
	turns, err := s.GetTurns(ctx, sess.ID, 0)
	if err != nil {
		t.Fatalf("get turns: %v", err)
	}
	if len(turns) != 1000 {
		t.Fatalf("expected 1000 turns (default limit), got %d", len(turns))
	}

	// ListSessions with limit <= 0 defaults to 10000.
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 10005; i++ {
		createSessionAt(t, s, "/many", base, base)
	}
	sessions, err := s.ListSessions(ctx, "/many", 0)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 10000 {
		t.Fatalf("expected 10000 sessions (default limit), got %d", len(sessions))
	}
}

func TestSQLite_GetMessages_CorruptToolCalls(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	sess, _ := s.CreateSession(ctx, "/tmp/proj")
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO messages (id, session_id, role, content, tool_call_id, tool_calls, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"bad-msg", sess.ID, string(api.RoleAssistant), "content", "", "not-json", time.Now().UTC()); err != nil {
		t.Fatalf("insert corrupt message: %v", err)
	}

	if _, err := s.GetMessages(ctx, sess.ID, 0); err == nil {
		t.Fatal("expected error for corrupt tool_calls JSON")
	}
}

func TestSQLite_GetTurns_CorruptJSON(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	sess, _ := s.CreateSession(ctx, "/tmp/proj")

	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO turns (id, session_id, state, input, response, tool_calls, results, error, started_at, ended_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"bad-turn", sess.ID, api.TurnIdle.String(), "input", "response", "not-json", "null", "", time.Now().UTC(), nil); err != nil {
		t.Fatalf("insert corrupt turn: %v", err)
	}

	if _, err := s.GetTurns(ctx, sess.ID, 0); err == nil {
		t.Fatal("expected error for corrupt tool_calls JSON")
	}

	if _, err := s.db.ExecContext(ctx, `DELETE FROM turns WHERE id = ?`, "bad-turn"); err != nil {
		t.Fatalf("delete corrupt turn: %v", err)
	}

	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO turns (id, session_id, state, input, response, tool_calls, results, error, started_at, ended_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"bad-turn2", sess.ID, api.TurnIdle.String(), "input", "response", "null", "not-json", "", time.Now().UTC(), nil); err != nil {
		t.Fatalf("insert corrupt results: %v", err)
	}

	if _, err := s.GetTurns(ctx, sess.ID, 0); err == nil {
		t.Fatal("expected error for corrupt results JSON")
	}
}

func TestSQLite_GetTurns_InvalidState(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	sess, _ := s.CreateSession(ctx, "/tmp/proj")
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO turns (id, session_id, state, input, response, tool_calls, results, error, started_at, ended_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"bad-state", sess.ID, "not-a-state", "input", "response", "null", "null", "", time.Now().UTC(), nil); err != nil {
		t.Fatalf("insert invalid state turn: %v", err)
	}

	if _, err := s.GetTurns(ctx, sess.ID, 0); err == nil {
		t.Fatal("expected error for invalid turn state")
	}
}

func TestSQLite_AppendMessage_Errors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	msg := api.Message{ID: "m1", Role: api.RoleUser, Content: "hi", CreatedAt: time.Now().UTC()}

	if err := s.AppendMessage(ctx, "", msg); err == nil {
		t.Error("expected error for empty session ID")
	}

	if err := s.AppendMessage(ctx, "nonexistent", msg); err == nil {
		t.Error("expected error appending to nonexistent session")
	}
}

func TestSQLite_ClearMessages_Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	if err := s.ClearMessages(ctx, ""); err == nil {
		t.Error("expected error for empty session ID")
	}
}

func TestSQLite_ReplaceMessages_Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	if err := s.ReplaceMessages(ctx, "", []api.Message{}); err == nil {
		t.Error("expected error for empty session ID")
	}
}

func TestSQLite_SaveTurn_Errors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	turn := api.Turn{ID: "t1", State: api.TurnIdle, StartedAt: time.Now().UTC()}

	if err := s.SaveTurn(ctx, "", turn); err == nil {
		t.Error("expected error for empty session ID")
	}

	if err := s.SaveTurn(ctx, "nonexistent", turn); err == nil {
		t.Error("expected error saving turn to nonexistent session")
	}
}

func TestSQLite_DeleteSession_Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	if err := s.DeleteSession(ctx, ""); err == nil {
		t.Error("expected error for empty session ID")
	}
}

func TestSQLite_DeleteSession_Nonexistent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	err := s.DeleteSession(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error deleting nonexistent session")
	}
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got %T: %v", err, err)
	}
}

func TestSQLite_DBClosedErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	// Create a session so there is data to query.
	sess, err := s.CreateSession(ctx, "/tmp/proj")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	msg := api.Message{ID: "m1", Role: api.RoleUser, Content: "hi", CreatedAt: time.Now().UTC()}
	if err := s.AppendMessage(ctx, sess.ID, msg); err != nil {
		t.Fatalf("append message: %v", err)
	}
	turn := api.Turn{ID: "t1", State: api.TurnIdle, StartedAt: time.Now().UTC()}
	if err := s.SaveTurn(ctx, sess.ID, turn); err != nil {
		t.Fatalf("save turn: %v", err)
	}

	// Close the underlying database directly; subsequent calls must error.
	if err := s.db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	if _, err := s.GetSession(ctx, sess.ID); err == nil {
		t.Error("expected error from GetSession after db closed")
	}
	if _, err := s.GetLastSession(ctx, "/tmp/proj"); err == nil {
		t.Error("expected error from GetLastSession after db closed")
	}
	if _, err := s.ListSessions(ctx, "/tmp/proj", 0); err == nil {
		t.Error("expected error from ListSessions after db closed")
	}
	if _, err := s.GetMessages(ctx, sess.ID, 0); err == nil {
		t.Error("expected error from GetMessages after db closed")
	}
	if _, err := s.GetTurns(ctx, sess.ID, 0); err == nil {
		t.Error("expected error from GetTurns after db closed")
	}
	if _, err := s.CountTurns(ctx, sess.ID, api.TurnIdle); err == nil {
		t.Error("expected error from CountTurns after db closed")
	}
	if err := s.AppendMessage(ctx, sess.ID, msg); err == nil {
		t.Error("expected error from AppendMessage after db closed")
	}
	if err := s.ClearMessages(ctx, sess.ID); err == nil {
		t.Error("expected error from ClearMessages after db closed")
	}
	if err := s.ReplaceMessages(ctx, sess.ID, []api.Message{msg}); err == nil {
		t.Error("expected error from ReplaceMessages after db closed")
	}
	if err := s.SaveTurn(ctx, sess.ID, turn); err == nil {
		t.Error("expected error from SaveTurn after db closed")
	}
	if err := s.UpdateSession(ctx, sess); err == nil {
		t.Error("expected error from UpdateSession after db closed")
	}
	if err := s.DeleteSession(ctx, sess.ID); err == nil {
		t.Error("expected error from DeleteSession after db closed")
	}
	if _, err := s.CreateSession(ctx, "/tmp/proj"); err == nil {
		t.Error("expected error from CreateSession after db closed")
	}
}

func TestSQLite_ContextCancelled(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	sess, err := s.CreateSession(ctx, "/tmp/proj")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	msg := api.Message{ID: "m1", Role: api.RoleUser, Content: "hi", CreatedAt: time.Now().UTC()}
	turn := api.Turn{ID: "t1", State: api.TurnIdle, StartedAt: time.Now().UTC()}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()

	if _, err := s.GetSession(cancelled, sess.ID); err == nil {
		t.Error("expected error from GetSession with cancelled context")
	}
	if _, err := s.GetLastSession(cancelled, "/tmp/proj"); err == nil {
		t.Error("expected error from GetLastSession with cancelled context")
	}
	if _, err := s.ListSessions(cancelled, "/tmp/proj", 0); err == nil {
		t.Error("expected error from ListSessions with cancelled context")
	}
	if _, err := s.GetMessages(cancelled, sess.ID, 0); err == nil {
		t.Error("expected error from GetMessages with cancelled context")
	}
	if _, err := s.GetTurns(cancelled, sess.ID, 0); err == nil {
		t.Error("expected error from GetTurns with cancelled context")
	}
	if _, err := s.CountTurns(cancelled, sess.ID, api.TurnIdle); err == nil {
		t.Error("expected error from CountTurns with cancelled context")
	}
	if err := s.AppendMessage(cancelled, sess.ID, msg); err == nil {
		t.Error("expected error from AppendMessage with cancelled context")
	}
	if err := s.ClearMessages(cancelled, sess.ID); err == nil {
		t.Error("expected error from ClearMessages with cancelled context")
	}
	if err := s.ReplaceMessages(cancelled, sess.ID, []api.Message{msg}); err == nil {
		t.Error("expected error from ReplaceMessages with cancelled context")
	}
	if err := s.SaveTurn(cancelled, sess.ID, turn); err == nil {
		t.Error("expected error from SaveTurn with cancelled context")
	}
	if err := s.UpdateSession(cancelled, sess); err == nil {
		t.Error("expected error from UpdateSession with cancelled context")
	}
	if err := s.DeleteSession(cancelled, sess.ID); err == nil {
		t.Error("expected error from DeleteSession with cancelled context")
	}
}

func TestSQLite_UpdateSession_Nonexistent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	sess := &api.Session{
		ID:   "nonexistent",
		Name: "name",
		Path: "/tmp/proj",
	}
	err := s.UpdateSession(ctx, sess)
	if err == nil {
		t.Fatal("expected error updating nonexistent session")
	}
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got %T: %v", err, err)
	}
}

func TestSQLite_ListSessions_EmptyPathError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	if _, err := s.ListSessions(ctx, "", 0); err == nil {
		t.Error("expected error for empty path")
	}
}

func TestSQLite_GetMessages_EmptySessionID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	if _, err := s.GetMessages(ctx, "", 0); err == nil {
		t.Error("expected error for empty session ID")
	}
}

func TestSQLite_GetTurns_EmptySessionID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	if _, err := s.GetTurns(ctx, "", 0); err == nil {
		t.Error("expected error for empty session ID")
	}
}

func TestSQLite_ReplaceMessages_PartialErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	msg := api.Message{ID: "m1", Role: api.RoleUser, Content: "hi", CreatedAt: time.Now().UTC()}
	fakeErr := errors.New("partial error")

	t.Run("delete error", func(t *testing.T) {
		t.Parallel()
		s := &SQLite{db: newPartialErrDB(t, partialErrDriver{
			execErr: func(query string) error {
				if strings.HasPrefix(query, "DELETE") {
					return fakeErr
				}
				return nil
			},
		})}
		defer s.Close()
		if err := s.ReplaceMessages(ctx, "s1", []api.Message{msg}); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("prepare error", func(t *testing.T) {
		t.Parallel()
		s := &SQLite{db: newPartialErrDB(t, partialErrDriver{
			prepareErr: fakeErr,
		})}
		defer s.Close()
		if err := s.ReplaceMessages(ctx, "s1", []api.Message{msg}); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("stmt exec error", func(t *testing.T) {
		t.Parallel()
		s := &SQLite{db: newPartialErrDB(t, partialErrDriver{
			stmtExecErr: fakeErr,
		})}
		defer s.Close()
		if err := s.ReplaceMessages(ctx, "s1", []api.Message{msg}); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("update timestamp error", func(t *testing.T) {
		t.Parallel()
		s := &SQLite{db: newPartialErrDB(t, partialErrDriver{
			execErr: func(query string) error {
				if strings.HasPrefix(query, "UPDATE sessions") {
					return fakeErr
				}
				return nil
			},
		})}
		defer s.Close()
		if err := s.ReplaceMessages(ctx, "s1", []api.Message{msg}); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("commit error", func(t *testing.T) {
		t.Parallel()
		s := &SQLite{db: newPartialErrDB(t, partialErrDriver{
			commitErr: fakeErr,
		})}
		defer s.Close()
		if err := s.ReplaceMessages(ctx, "s1", []api.Message{msg}); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestSQLite_AppendMessage_PartialErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	msg := api.Message{ID: "m1", Role: api.RoleUser, Content: "hi", CreatedAt: time.Now().UTC()}
	fakeErr := errors.New("partial error")

	t.Run("insert error", func(t *testing.T) {
		t.Parallel()
		s := &SQLite{db: newPartialErrDB(t, partialErrDriver{
			execErr: func(query string) error {
				if strings.HasPrefix(query, "INSERT INTO messages") {
					return fakeErr
				}
				return nil
			},
		})}
		defer s.Close()
		if err := s.AppendMessage(ctx, "s1", msg); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("update timestamp error", func(t *testing.T) {
		t.Parallel()
		s := &SQLite{db: newPartialErrDB(t, partialErrDriver{
			execErr: func(query string) error {
				if strings.HasPrefix(query, "UPDATE sessions") {
					return fakeErr
				}
				return nil
			},
		})}
		defer s.Close()
		if err := s.AppendMessage(ctx, "s1", msg); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("commit error", func(t *testing.T) {
		t.Parallel()
		s := &SQLite{db: newPartialErrDB(t, partialErrDriver{
			commitErr: fakeErr,
		})}
		defer s.Close()
		if err := s.AppendMessage(ctx, "s1", msg); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestSQLite_SaveTurn_PartialErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	turn := api.Turn{ID: "t1", State: api.TurnIdle, StartedAt: time.Now().UTC()}
	fakeErr := errors.New("partial error")

	t.Run("upsert error", func(t *testing.T) {
		t.Parallel()
		s := &SQLite{db: newPartialErrDB(t, partialErrDriver{
			execErr: func(query string) error {
				if strings.HasPrefix(query, "INSERT INTO turns") {
					return fakeErr
				}
				return nil
			},
		})}
		defer s.Close()
		if err := s.SaveTurn(ctx, "s1", turn); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("update timestamp error", func(t *testing.T) {
		t.Parallel()
		s := &SQLite{db: newPartialErrDB(t, partialErrDriver{
			execErr: func(query string) error {
				if strings.HasPrefix(query, "UPDATE sessions") {
					return fakeErr
				}
				return nil
			},
		})}
		defer s.Close()
		if err := s.SaveTurn(ctx, "s1", turn); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("commit error", func(t *testing.T) {
		t.Parallel()
		s := &SQLite{db: newPartialErrDB(t, partialErrDriver{
			commitErr: fakeErr,
		})}
		defer s.Close()
		if err := s.SaveTurn(ctx, "s1", turn); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestSQLite_RowsIterationErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rowsErr := errors.New("rows iteration error")

	t.Run("ListSessions", func(t *testing.T) {
		t.Parallel()
		s := &SQLite{db: newErrRowsDB(t, rowsErr)}
		defer s.Close()
		if _, err := s.ListSessions(ctx, "/tmp/proj", 0); err == nil {
			t.Fatal("expected error from ListSessions")
		}
	})

	t.Run("GetMessages", func(t *testing.T) {
		t.Parallel()
		s := &SQLite{db: newErrRowsDB(t, rowsErr)}
		defer s.Close()
		if _, err := s.GetMessages(ctx, "s1", 0); err == nil {
			t.Fatal("expected error from GetMessages")
		}
	})

	t.Run("GetTurns", func(t *testing.T) {
		t.Parallel()
		s := &SQLite{db: newErrRowsDB(t, rowsErr)}
		defer s.Close()
		if _, err := s.GetTurns(ctx, "s1", 0); err == nil {
			t.Fatal("expected error from GetTurns")
		}
	})
}

func TestSQLite_FakeDBErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fakeErr := errors.New("fake db error")

	s := &SQLite{db: newErrDB(t, fakeErr)}
	defer s.Close()

	sess := &api.Session{ID: "s1", Name: "n", Path: "/tmp/proj"}
	msg := api.Message{ID: "m1", Role: api.RoleUser, Content: "hi", CreatedAt: time.Now().UTC()}
	turn := api.Turn{ID: "t1", State: api.TurnIdle, StartedAt: time.Now().UTC()}

	if _, err := s.CreateSession(ctx, "/tmp/proj"); err == nil {
		t.Error("expected error from CreateSession")
	}
	if _, err := s.GetSession(ctx, "s1"); err == nil {
		t.Error("expected error from GetSession")
	}
	if _, err := s.GetLastSession(ctx, "/tmp/proj"); err == nil {
		t.Error("expected error from GetLastSession")
	}
	if _, err := s.ListSessions(ctx, "/tmp/proj", 0); err == nil {
		t.Error("expected error from ListSessions")
	}
	if _, err := s.GetMessages(ctx, "s1", 0); err == nil {
		t.Error("expected error from GetMessages")
	}
	if _, err := s.GetTurns(ctx, "s1", 0); err == nil {
		t.Error("expected error from GetTurns")
	}
	if _, err := s.CountTurns(ctx, "s1", api.TurnIdle); err == nil {
		t.Error("expected error from CountTurns")
	}
	if err := s.AppendMessage(ctx, "s1", msg); err == nil {
		t.Error("expected error from AppendMessage")
	}
	if err := s.ClearMessages(ctx, "s1"); err == nil {
		t.Error("expected error from ClearMessages")
	}
	if err := s.ReplaceMessages(ctx, "s1", []api.Message{msg}); err == nil {
		t.Error("expected error from ReplaceMessages")
	}
	if err := s.SaveTurn(ctx, "s1", turn); err == nil {
		t.Error("expected error from SaveTurn")
	}
	if err := s.UpdateSession(ctx, sess); err == nil {
		t.Error("expected error from UpdateSession")
	}
	if err := s.DeleteSession(ctx, "s1"); err == nil {
		t.Error("expected error from DeleteSession")
	}
}

func TestSQLite_CountTurns_EmptySessionID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	if _, err := s.CountTurns(ctx, "", api.TurnIdle); err == nil {
		t.Error("expected error for empty session ID")
	}
}

// failFS is a test double that delegates to an optional fs.FS and can fail
// specific operations.
type failFS struct {
	delegate    fs.FS
	readDirErr  error
	readFileErr error
}

func (f *failFS) Open(name string) (fs.File, error) {
	if f.delegate != nil {
		return f.delegate.Open(name)
	}
	return nil, fs.ErrNotExist
}

func (f *failFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if f.readDirErr != nil {
		return nil, f.readDirErr
	}
	if f.delegate != nil {
		return fs.ReadDir(f.delegate, name)
	}
	return nil, fs.ErrNotExist
}

func (f *failFS) ReadFile(name string) ([]byte, error) {
	if f.readFileErr != nil {
		return nil, f.readFileErr
	}
	if f.delegate != nil {
		return fs.ReadFile(f.delegate, name)
	}
	return nil, fs.ErrNotExist
}

// errDriver is a minimal sql.Driver that always returns a configured error.
// It is used to exercise error branches in store methods without touching a
// real database.
type errDriver struct {
	err error
}

func (d *errDriver) Open(string) (driver.Conn, error) {
	return &errConn{err: d.err}, nil
}

type errConn struct {
	err error
}

func (c *errConn) Prepare(string) (driver.Stmt, error) { return nil, c.err }
func (c *errConn) Close() error                        { return c.err }
func (c *errConn) Begin() (driver.Tx, error)           { return nil, c.err }

func (c *errConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return nil, c.err
}

func (c *errConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return nil, c.err
}

func (c *errConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return nil, c.err
}

func newErrDB(t *testing.T, err error) *sql.DB {
	t.Helper()
	driverName := fmt.Sprintf("errdriver-%d", time.Now().UnixNano())
	sql.Register(driverName, &errDriver{err: err})
	db, err := sql.Open(driverName, "")
	if err != nil {
		t.Fatalf("open err db: %v", err)
	}
	return db
}

// fakeResult implements driver.Result for tests.
type fakeResult struct{}

func (fakeResult) LastInsertId() (int64, error) { return 0, nil }
func (fakeResult) RowsAffected() (int64, error) { return 1, nil }

// fakeRows implements driver.Rows for tests.
type fakeRows struct {
	cols    []string
	values  []driver.Value
	done    bool
	nextErr error
}

func (r *fakeRows) Columns() []string { return r.cols }
func (r *fakeRows) Close() error      { return nil }
func (r *fakeRows) Next(dest []driver.Value) error {
	if r.nextErr != nil {
		return r.nextErr
	}
	if r.done {
		return io.EOF
	}
	r.done = true
	copy(dest, r.values)
	return nil
}

// fakeStmt implements driver.Stmt for tests.
type fakeStmt struct {
	execErr  error
	queryErr error
	closeErr error
}

func (s *fakeStmt) Close() error  { return s.closeErr }
func (s *fakeStmt) NumInput() int { return -1 }
func (s *fakeStmt) Exec(args []driver.Value) (driver.Result, error) {
	return fakeResult{}, s.execErr
}
func (s *fakeStmt) Query(args []driver.Value) (driver.Rows, error) {
	return &fakeRows{}, s.queryErr
}

// fakeTx implements driver.Tx for tests.
type fakeTx struct {
	commitErr   error
	rollbackErr error
}

func (tx *fakeTx) Commit() error   { return tx.commitErr }
func (tx *fakeTx) Rollback() error { return tx.rollbackErr }

// partialErrDriver is a sql.Driver whose connection succeeds or fails per
// operation. It is used to exercise error branches after BeginTx.
type partialErrDriver struct {
	beginErr    error
	execErr     func(query string) error
	queryErr    func(query string) error
	prepareErr  error
	commitErr   error
	stmtExecErr error
}

func (d *partialErrDriver) Open(string) (driver.Conn, error) {
	return &partialErrConn{
		beginErr:    d.beginErr,
		execErr:     d.execErr,
		queryErr:    d.queryErr,
		prepareErr:  d.prepareErr,
		commitErr:   d.commitErr,
		stmtExecErr: d.stmtExecErr,
	}, nil
}

type partialErrConn struct {
	beginErr    error
	execErr     func(query string) error
	queryErr    func(query string) error
	prepareErr  error
	commitErr   error
	stmtExecErr error
}

func (c *partialErrConn) Prepare(string) (driver.Stmt, error) {
	return &fakeStmt{execErr: c.stmtExecErr, queryErr: c.stmtExecErr}, c.prepareErr
}
func (c *partialErrConn) Close() error { return nil }
func (c *partialErrConn) Begin() (driver.Tx, error) {
	return &fakeTx{commitErr: c.commitErr}, c.beginErr
}
func (c *partialErrConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	if c.execErr != nil {
		if err := c.execErr(query); err != nil {
			return nil, err
		}
	}
	return fakeResult{}, nil
}
func (c *partialErrConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if c.queryErr != nil {
		if err := c.queryErr(query); err != nil {
			return nil, err
		}
	}
	if strings.Contains(query, "user_version") {
		return &fakeRows{cols: []string{"user_version"}, values: []driver.Value{int64(0)}}, nil
	}
	return &fakeRows{}, nil
}
func (c *partialErrConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return &fakeTx{commitErr: c.commitErr}, c.beginErr
}

func newPartialErrDB(t *testing.T, cfg partialErrDriver) *sql.DB {
	t.Helper()
	driverName := fmt.Sprintf("partialdriver-%d", time.Now().UnixNano())
	sql.Register(driverName, &cfg)
	db, err := sql.Open(driverName, "")
	if err != nil {
		t.Fatalf("open partial err db: %v", err)
	}
	return db
}

// errRowsDriver returns a single row whose Next call returns the configured
// error. It is used to exercise rows.Err() branches.
type errRowsDriver struct{ err error }

func (d *errRowsDriver) Open(string) (driver.Conn, error) {
	return &errRowsConn{err: d.err}, nil
}

type errRowsConn struct{ err error }

func (c *errRowsConn) Prepare(string) (driver.Stmt, error) { return &fakeStmt{}, nil }
func (c *errRowsConn) Close() error                        { return nil }
func (c *errRowsConn) Begin() (driver.Tx, error)           { return &fakeTx{}, nil }
func (c *errRowsConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return fakeResult{}, nil
}
func (c *errRowsConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return &fakeRows{nextErr: c.err}, nil
}
func (c *errRowsConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return &fakeTx{}, nil
}

func newErrRowsDB(t *testing.T, err error) *sql.DB {
	t.Helper()
	driverName := fmt.Sprintf("errrowsdriver-%d", time.Now().UnixNano())
	sql.Register(driverName, &errRowsDriver{err: err})
	db, err := sql.Open(driverName, "")
	if err != nil {
		t.Fatalf("open err rows db: %v", err)
	}
	return db
}
