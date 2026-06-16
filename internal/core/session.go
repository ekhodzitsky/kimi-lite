package core

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"github.com/ekhodzitsky/kimi-lite/internal/idgen"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

var (
	cachedHome string
	homeErr    error
	homeOnce   sync.Once
)

// userHomeDir returns the user's home directory, caching the result after the
// first call. An error is returned if the home directory cannot be determined.
func userHomeDir() (string, error) {
	homeOnce.Do(func() {
		cachedHome, homeErr = os.UserHomeDir()
		if homeErr != nil {
			homeErr = fmt.Errorf("user home dir: %w", homeErr)
		}
	})
	return cachedHome, homeErr
}

// makePortablePath converts an absolute path to a portable relative path.
// It replaces the user's home directory with "~" for portability across machines.
func makePortablePath(absPath string) string {
	home, err := userHomeDir()
	if err != nil || home == "" {
		return absPath
	}
	rel, err := filepath.Rel(home, absPath)
	if err != nil {
		return absPath
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return absPath
	}
	if rel == "." {
		return "~"
	}
	return filepath.Join("~", rel)
}

// resolvePortablePath converts a portable path back to an absolute path.
// It rejects "~foo" style paths and only accepts "~" or "~/" prefixes.
func resolvePortablePath(portable string) string {
	if portable == "~" || strings.HasPrefix(portable, "~/") {
		home, err := userHomeDir()
		if err != nil || home == "" {
			return portable
		}
		if portable == "~" {
			return home
		}
		return filepath.Join(home, strings.TrimPrefix(portable, "~/"))
	}
	return portable
}

// isNilInterface reports whether v is nil or a typed-nil pointer/channel/map/
// slice/function/interface value. It is used to guard setters against values
// that compare non-nil as interfaces but hold a nil concrete pointer.
func isNilInterface(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	}
	return false
}

// SessionManager manages conversation sessions using an api.Store.
type SessionManager struct {
	store      api.Store
	mu         sync.RWMutex
	currentID  string
	hookRunner api.HookRunner
	metrics    api.MetricsCollector
}

// NewSessionManager creates a new SessionManager.
func NewSessionManager(store api.Store) *SessionManager {
	return &SessionManager{
		store:   store,
		metrics: api.NoopMetricsCollector{},
	}
}

// SetHookRunner sets the lifecycle hook runner.
// Typed-nil interface values are treated as unset.
func (sm *SessionManager) SetHookRunner(r api.HookRunner) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if isNilInterface(r) {
		sm.hookRunner = nil
		return
	}
	sm.hookRunner = r
}

// SetMetricsCollector sets the metrics collector.
// A nil or typed-nil value falls back to a no-op collector.
func (sm *SessionManager) SetMetricsCollector(m api.MetricsCollector) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if isNilInterface(m) {
		sm.metrics = api.NoopMetricsCollector{}
		return
	}
	sm.metrics = m
}

func (sm *SessionManager) getHookRunner() api.HookRunner {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.hookRunner
}

func (sm *SessionManager) getMetrics() api.MetricsCollector {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.metrics
}

// Start creates a new session for the given path and sets it as current.
func (sm *SessionManager) Start(ctx context.Context, path string) (*api.Session, error) {
	sess, err := sm.store.CreateSession(ctx, makePortablePath(path))
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	sm.setCurrent(sess.ID)
	sm.getMetrics().IncCounter("session.created")
	sm.runHooks(ctx, api.HookSessionStart, sess.ID)
	return sess, nil
}

// Resume retrieves an existing session by ID, loads its messages, and sets it as current.
func (sm *SessionManager) Resume(ctx context.Context, id string) (*api.Session, error) {
	sess, err := sm.store.GetSession(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	sess.Path = resolvePortablePath(sess.Path)
	msgs, err := sm.store.GetMessages(ctx, id, 0)
	if err != nil {
		return nil, fmt.Errorf("get messages: %w", err)
	}
	msgs, err = sm.recoverMessages(ctx, id, msgs)
	if err != nil {
		return nil, fmt.Errorf("recover messages: %w", err)
	}
	sess.Messages = msgs
	sm.setCurrent(sess.ID)
	sm.getMetrics().IncCounter("session.resumed")
	sm.runHooks(ctx, api.HookSessionStart, sess.ID)
	return sess, nil
}

// ContinueLast resumes the most recently updated session for the path.
func (sm *SessionManager) ContinueLast(ctx context.Context, path string) (*api.Session, error) {
	sess, err := sm.store.GetLastSession(ctx, makePortablePath(path))
	if err != nil {
		return nil, fmt.Errorf("get last session: %w", err)
	}
	sess.Path = resolvePortablePath(sess.Path)
	msgs, err := sm.store.GetMessages(ctx, sess.ID, 0)
	if err != nil {
		return nil, fmt.Errorf("get messages: %w", err)
	}
	msgs, err = sm.recoverMessages(ctx, sess.ID, msgs)
	if err != nil {
		return nil, fmt.Errorf("recover messages: %w", err)
	}
	sess.Messages = msgs
	sm.setCurrent(sess.ID)
	sm.getMetrics().IncCounter("session.resumed")
	sm.runHooks(ctx, api.HookSessionStart, sess.ID)
	return sess, nil
}

// List returns all sessions for the given path ordered by updated_at desc.
func (sm *SessionManager) List(ctx context.Context, path string) ([]api.Session, error) {
	sessions, err := sm.store.ListSessions(ctx, makePortablePath(path), 0)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	for i := range sessions {
		sessions[i].Path = resolvePortablePath(sessions[i].Path)
	}
	return sessions, nil
}

// ListAll returns sessions across all paths ordered by updated_at desc.
func (sm *SessionManager) ListAll(ctx context.Context, limit int) ([]api.Session, error) {
	if limit <= 0 {
		limit = 10000
	}
	sessions, err := sm.store.ListAllSessions(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("list all sessions: %w", err)
	}
	for i := range sessions {
		sessions[i].Path = resolvePortablePath(sessions[i].Path)
	}
	return sessions, nil
}

// Get retrieves a session by ID including its messages.
func (sm *SessionManager) Get(ctx context.Context, id string) (*api.Session, error) {
	sess, err := sm.store.GetSession(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	sess.Path = resolvePortablePath(sess.Path)
	msgs, err := sm.store.GetMessages(ctx, id, 0)
	if err != nil {
		return nil, fmt.Errorf("get messages: %w", err)
	}
	msgs, err = sm.recoverMessages(ctx, id, msgs)
	if err != nil {
		return nil, fmt.Errorf("recover messages: %w", err)
	}
	sess.Messages = msgs
	return sess, nil
}

// ClearMessages removes all messages from a session.
func (sm *SessionManager) ClearMessages(ctx context.Context, id string) error {
	if err := sm.store.ClearMessages(ctx, id); err != nil {
		return fmt.Errorf("clear messages: %w", err)
	}
	return nil
}

// Rename updates the name of the session with the given ID.
func (sm *SessionManager) Rename(ctx context.Context, id string, name string) error {
	sess, err := sm.store.GetSession(ctx, id)
	if err != nil {
		return fmt.Errorf("get session: %w", err)
	}
	sess.Name = name
	if err := sm.store.UpdateSession(ctx, sess); err != nil {
		return fmt.Errorf("update session: %w", err)
	}
	return nil
}

// Fork creates a new session copied from the source session, including all
// messages. If name is empty, a default name is derived from the source.
// The forked session becomes the current session.
func (sm *SessionManager) Fork(ctx context.Context, sourceID string, name string) (*api.Session, error) {
	source, err := sm.store.GetSession(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("get source session: %w", err)
	}

	msgs, err := sm.store.GetMessages(ctx, sourceID, 0)
	if err != nil {
		return nil, fmt.Errorf("get messages: %w", err)
	}

	if name == "" {
		if source.Name != "" {
			name = fmt.Sprintf("Fork of %s", source.Name)
		} else {
			name = fmt.Sprintf("Fork of %s", source.ID)
		}
	}

	forked, err := sm.store.CreateSession(ctx, source.Path)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	forked.Name = name
	if err := sm.store.UpdateSession(ctx, forked); err != nil {
		return nil, fmt.Errorf("update forked session: %w", err)
	}

	forkedMsgs := make([]api.Message, len(msgs))
	for i, msg := range msgs {
		msg.ID = idgen.GenerateID()
		forkedMsgs[i] = msg
	}
	if err := sm.store.ReplaceMessages(ctx, forked.ID, forkedMsgs); err != nil {
		return nil, fmt.Errorf("replace messages: %w", err)
	}

	sm.setCurrent(forked.ID)
	forked.Path = resolvePortablePath(forked.Path)
	forked.Messages = forkedMsgs
	sm.getMetrics().IncCounter("session.created")
	sm.runHooks(ctx, api.HookSessionStart, forked.ID)
	return forked, nil
}

// CurrentSessionID returns the ID of the current session.
func (sm *SessionManager) CurrentSessionID() string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.currentID
}

func (sm *SessionManager) setCurrent(id string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.currentID = id
}

func (sm *SessionManager) runHooks(ctx context.Context, event api.HookEvent, sessionID string) {
	runner := sm.getHookRunner()
	if runner == nil {
		return
	}
	if err := runner.Run(ctx, api.HookData{
		Event:     event,
		SessionID: sessionID,
	}); err != nil {
		slog.Warn("session hook failed", "event", event, "error", err)
	}
}

// recoverMessages detects assistant messages with tool calls that are missing
// matching tool-result messages (e.g. because the process was interrupted) and
// appends synthetic tool-result messages so the conversation remains valid for
// the LLM. The recovered message list is persisted via ReplaceMessages and
// returned.
func (sm *SessionManager) recoverMessages(ctx context.Context, sessionID string, msgs []api.Message) ([]api.Message, error) {
	var out []api.Message
	used := make(map[int]bool)
	added := false

	for i, msg := range msgs {
		if used[i] {
			continue
		}
		out = append(out, msg)

		if msg.Role != api.RoleAssistant || len(msg.ToolCalls) == 0 {
			continue
		}

		callIDs := make(map[string]struct{}, len(msg.ToolCalls))
		for _, c := range msg.ToolCalls {
			callIDs[c.ID] = struct{}{}
		}

		// Consume tool-result messages that immediately follow this assistant
		// message and belong to one of its tool calls.
		results := make(map[string]api.Message)
		for j := i + 1; j < len(msgs) && msgs[j].Role == api.RoleTool; j++ {
			tcid := msgs[j].ToolCallID
			if _, wanted := callIDs[tcid]; !wanted {
				continue
			}
			if _, seen := results[tcid]; seen {
				continue
			}
			results[tcid] = msgs[j]
			used[j] = true
			out = append(out, msgs[j])
		}

		// Synthesize results for any tool calls that were not recovered.
		for _, c := range msg.ToolCalls {
			if _, ok := results[c.ID]; ok {
				continue
			}
			added = true
			out = append(out, api.Message{
				ID:         idgen.GenerateID(),
				Role:       api.RoleTool,
				Content:    "Tool call was interrupted before a result was recorded.",
				ToolCallID: c.ID,
				CreatedAt:  msg.CreatedAt,
			})
		}
	}

	if !added {
		return msgs, nil
	}
	if err := sm.store.ReplaceMessages(ctx, sessionID, out); err != nil {
		return nil, fmt.Errorf("replace recovered messages: %w", err)
	}
	return out, nil
}
