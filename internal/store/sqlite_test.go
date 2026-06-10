package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
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

func TestSQLite_Close(t *testing.T) {
	s, err := NewSQLite(":memory:")
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestSQLite_Concurrency(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)

	sess, _ := s.CreateSession(ctx, "/tmp/proj")

	// Concurrent appends.
	done := make(chan error, 2)
	go func() {
		done <- s.AppendMessage(ctx, sess.ID, api.Message{ID: "m1", Role: api.RoleUser, Content: "a", CreatedAt: time.Now().UTC()})
	}()
	go func() {
		done <- s.AppendMessage(ctx, sess.ID, api.Message{ID: "m2", Role: api.RoleAssistant, Content: "b", CreatedAt: time.Now().UTC()})
	}()

	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatalf("concurrent append: %v", err)
		}
	}

	msgs, _ := s.GetMessages(ctx, sess.ID, 0)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages after concurrent append, got %d", len(msgs))
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
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected sql.ErrNoRows wrapped, got %T: %v", err, err)
	}
}
