package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func TestPortablePath_RoundTrip(t *testing.T) {
	t.Parallel()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home directory:", err)
	}

	cases := []string{
		filepath.Join(home, "proj"),
		filepath.Join(home, "proj", "src", "main.go"),
		"/tmp/proj",
		"/tmp",
	}

	for _, abs := range cases {
		portable := makePortablePath(abs)
		resolved := resolvePortablePath(portable)
		if resolved != abs {
			t.Errorf("round-trip failed for %q: portable=%q resolved=%q", abs, portable, resolved)
		}
	}

	// Sibling directory that shares a prefix with home must NOT be treated as inside home.
	sibling := home + "data"
	if portable := makePortablePath(sibling); portable != sibling {
		t.Errorf("sibling path %q incorrectly portable-ized as %q, want %q", sibling, portable, sibling)
	}
}

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

func TestSessionManager_Get_ResolvesPath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sm := NewSessionManager(store)

	// Start stores a portable path; Get must resolve it back to absolute.
	sess, _ := sm.Start(ctx, "/tmp/proj")

	got, err := sm.Get(ctx, sess.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if got.Path != "/tmp/proj" {
		t.Errorf("path = %q, want %q", got.Path, "/tmp/proj")
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

func TestSessionManager_Rename(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sm := NewSessionManager(store)

	sess, err := sm.Start(ctx, "/tmp/proj")
	if err != nil {
		t.Fatalf("start session: %v", err)
	}

	if err := sm.Rename(ctx, sess.ID, "My Session"); err != nil {
		t.Fatalf("rename session: %v", err)
	}

	updated, err := store.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if updated.Name != "My Session" {
		t.Errorf("name = %q, want %q", updated.Name, "My Session")
	}
}

func TestSessionManager_Fork(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sm := NewSessionManager(store)

	sess, err := sm.Start(ctx, "/tmp/proj")
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	_ = store.AppendMessage(ctx, sess.ID, api.Message{ID: "m1", Role: api.RoleUser, Content: "hello", CreatedAt: sess.CreatedAt})
	_ = store.AppendMessage(ctx, sess.ID, api.Message{ID: "m2", Role: api.RoleAssistant, Content: "hi", CreatedAt: sess.CreatedAt})

	forked, err := sm.Fork(ctx, sess.ID, "Forked Session")
	if err != nil {
		t.Fatalf("fork session: %v", err)
	}

	if forked.ID == sess.ID {
		t.Error("forked session should have a new ID")
	}
	if forked.Name != "Forked Session" {
		t.Errorf("name = %q, want %q", forked.Name, "Forked Session")
	}
	if forked.Path != "/tmp/proj" {
		t.Errorf("path = %q, want %q", forked.Path, "/tmp/proj")
	}
	if len(forked.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(forked.Messages))
	}
	if sm.CurrentSessionID() != forked.ID {
		t.Errorf("current session = %q, want %q", sm.CurrentSessionID(), forked.ID)
	}

	// Verify messages are persisted under the new session.
	storedMsgs, err := store.GetMessages(ctx, forked.ID, 0)
	if err != nil {
		t.Fatalf("get forked messages: %v", err)
	}
	if len(storedMsgs) != 2 {
		t.Errorf("expected 2 stored messages, got %d", len(storedMsgs))
	}
}

func TestSessionManager_Fork_RegeneratesMessageIDs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sm := NewSessionManager(store)

	sess, err := sm.Start(ctx, "/tmp/proj")
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	_ = store.AppendMessage(ctx, sess.ID, api.Message{ID: "m1", Role: api.RoleUser, Content: "hello", CreatedAt: sess.CreatedAt})
	_ = store.AppendMessage(ctx, sess.ID, api.Message{ID: "m2", Role: api.RoleAssistant, Content: "hi", CreatedAt: sess.CreatedAt})

	forked, err := sm.Fork(ctx, sess.ID, "Forked")
	if err != nil {
		t.Fatalf("fork session: %v", err)
	}

	storedMsgs, err := store.GetMessages(ctx, forked.ID, 0)
	if err != nil {
		t.Fatalf("get forked messages: %v", err)
	}
	if len(storedMsgs) != 2 {
		t.Fatalf("expected 2 stored messages, got %d", len(storedMsgs))
	}
	for _, m := range storedMsgs {
		if m.ID == "m1" || m.ID == "m2" {
			t.Errorf("forked message reused source ID %q", m.ID)
		}
		if m.ID == "" {
			t.Error("forked message has empty ID")
		}
	}
}

func TestSessionManager_Fork_DefaultName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sm := NewSessionManager(store)

	sess, err := sm.Start(ctx, "/tmp/proj")
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	_ = sm.Rename(ctx, sess.ID, "Original")

	forked, err := sm.Fork(ctx, sess.ID, "")
	if err != nil {
		t.Fatalf("fork session: %v", err)
	}

	want := "Fork of Original"
	if forked.Name != want {
		t.Errorf("name = %q, want %q", forked.Name, want)
	}
}

func TestSessionManager_Metrics(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sm := NewSessionManager(store)

	metrics := &recordingMetrics{}
	sm.SetMetricsCollector(metrics)

	if _, err := sm.Start(ctx, "/tmp/proj"); err != nil {
		t.Fatalf("start session: %v", err)
	}
	if metrics.counterName != "session.created" {
		t.Errorf("expected session.created counter, got %q", metrics.counterName)
	}
}

func TestSessionManager_Setters_TypedNil(t *testing.T) {
	t.Parallel()
	sm := NewSessionManager(newMockStore())

	var nilMetrics *recordingMetrics
	sm.SetMetricsCollector(nilMetrics)
	if sm.metrics == nil {
		t.Error("typed-nil metrics should fall back to noop")
	}

	var nilHook *recordingHookRunner
	sm.SetHookRunner(nilHook)
	if sm.hookRunner != nil {
		t.Error("typed-nil hook runner should be stored as nil")
	}
}

func TestResolvePortablePath_RejectsTildeFoo(t *testing.T) {
	t.Parallel()
	if got := resolvePortablePath("~foo"); got != "~foo" {
		t.Errorf("resolvePortablePath(\"~foo\") = %q, want ~foo", got)
	}
}

func TestSessionManager_Setters_Synchronized(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sm := NewSessionManager(store)

	metrics := &recordingMetrics{}
	hook := &recordingHookRunner{}
	sm.SetMetricsCollector(metrics)
	sm.SetHookRunner(hook)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sm.SetMetricsCollector(metrics)
			sm.SetHookRunner(hook)
		}()
	}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = sm.Start(ctx, "/tmp/proj")
		}()
	}
	wg.Wait()
}

type recordingHookRunner struct{}

func (r *recordingHookRunner) Run(_ context.Context, _ api.HookData) error { return nil }

type recordingMetrics struct {
	mu          sync.Mutex
	counterName string
}

func (r *recordingMetrics) IncCounter(name string, _ ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counterName = name
}

func (r *recordingMetrics) RecordLatency(_ string, _ time.Duration, _ ...string) {}

func (r *recordingMetrics) RecordError(_ string) {}
