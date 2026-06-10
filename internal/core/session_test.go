package core

import (
	"context"
	"fmt"
	"testing"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func TestSessionManager_Start(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sm := NewSessionManager(store)

	sess, err := sm.Start(ctx, "/tmp/proj")
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	if sess.ID == "" {
		t.Error("expected session ID to be set")
	}
	if sess.Path != "/tmp/proj" {
		t.Errorf("path = %q, want %q", sess.Path, "/tmp/proj")
	}
	if sm.CurrentSessionID() != sess.ID {
		t.Errorf("current session = %q, want %q", sm.CurrentSessionID(), sess.ID)
	}
}

func TestSessionManager_Resume(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sm := NewSessionManager(store)

	sess, _ := sm.Start(ctx, "/tmp/proj")
	_ = store.AppendMessage(ctx, sess.ID, api.Message{ID: "m1", Role: api.RoleUser, Content: "hello", CreatedAt: sess.CreatedAt})

	resumed, err := sm.Resume(ctx, sess.ID)
	if err != nil {
		t.Fatalf("resume session: %v", err)
	}
	if resumed.ID != sess.ID {
		t.Errorf("id = %q, want %q", resumed.ID, sess.ID)
	}
	if len(resumed.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(resumed.Messages))
	}
	if sm.CurrentSessionID() != sess.ID {
		t.Errorf("current session = %q, want %q", sm.CurrentSessionID(), sess.ID)
	}
}

func TestSessionManager_Resume_NotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sm := NewSessionManager(store)

	_, err := sm.Resume(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
}

func TestSessionManager_ContinueLast(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sm := NewSessionManager(store)

	_, _ = sm.Start(ctx, "/tmp/proj")
	s2, _ := sm.Start(ctx, "/tmp/proj")

	last, err := sm.ContinueLast(ctx, "/tmp/proj")
	if err != nil {
		t.Fatalf("continue last: %v", err)
	}
	if last.ID != s2.ID {
		t.Errorf("last session = %q, want %q", last.ID, s2.ID)
	}
	if sm.CurrentSessionID() != s2.ID {
		t.Errorf("current session = %q, want %q", sm.CurrentSessionID(), s2.ID)
	}

	// Different path should fail.
	_, err = sm.ContinueLast(ctx, "/other")
	if err == nil {
		t.Fatal("expected error for different path")
	}
}

func TestSessionManager_List(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sm := NewSessionManager(store)

	s1, _ := sm.Start(ctx, "/tmp/proj")
	s2, _ := sm.Start(ctx, "/tmp/proj")

	list, err := sm.List(ctx, "/tmp/proj")
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(list))
	}
	// List is ordered by updated_at desc, so s2 should be first.
	if list[0].ID != s2.ID {
		t.Errorf("first = %q, want %q", list[0].ID, s2.ID)
	}
	if list[1].ID != s1.ID {
		t.Errorf("second = %q, want %q", list[1].ID, s1.ID)
	}
}

func TestSessionManager_Get(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sm := NewSessionManager(store)

	sess, _ := sm.Start(ctx, "/tmp/proj")
	_ = store.AppendMessage(ctx, sess.ID, api.Message{ID: "m1", Role: api.RoleUser, Content: "hello", CreatedAt: sess.CreatedAt})

	got, err := sm.Get(ctx, sess.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if got.ID != sess.ID {
		t.Errorf("id = %q, want %q", got.ID, sess.ID)
	}
	if len(got.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(got.Messages))
	}
}

func TestSessionManager_ClearMessages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sm := NewSessionManager(store)

	sess, _ := sm.Start(ctx, "/tmp/proj")
	_ = store.AppendMessage(ctx, sess.ID, api.Message{ID: "m1", Role: api.RoleUser, Content: "hello", CreatedAt: sess.CreatedAt})

	if err := sm.ClearMessages(ctx, sess.ID); err != nil {
		t.Fatalf("clear messages: %v", err)
	}

	msgs, _ := store.GetMessages(ctx, sess.ID, 0)
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}
}

func TestSessionManager_ConcurrentCurrentID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sm := NewSessionManager(store)

	var ids []string
	for i := 0; i < 10; i++ {
		sess, _ := sm.Start(ctx, fmt.Sprintf("/tmp/proj%d", i))
		ids = append(ids, sess.ID)
	}

	// Verify the last one won.
	if sm.CurrentSessionID() != ids[len(ids)-1] {
		t.Errorf("current = %q, want %q", sm.CurrentSessionID(), ids[len(ids)-1])
	}
}
