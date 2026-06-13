package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// makePortablePath converts an absolute path to a portable relative path.
// It replaces the user's home directory with "~" for portability across machines.
func makePortablePath(absPath string) string {
	home, err := os.UserHomeDir()
	if err != nil {
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
func resolvePortablePath(portable string) string {
	if !strings.HasPrefix(portable, "~") {
		return portable
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return portable
	}
	if portable == "~" {
		return home
	}
	return filepath.Join(home, strings.TrimPrefix(portable, "~/"))
}

// SessionManager manages conversation sessions using an api.Store.
type SessionManager struct {
	store     api.Store
	mu        sync.RWMutex
	currentID string
}

// NewSessionManager creates a new SessionManager.
func NewSessionManager(store api.Store) *SessionManager {
	return &SessionManager{store: store}
}

// Start creates a new session for the given path and sets it as current.
func (sm *SessionManager) Start(ctx context.Context, path string) (*api.Session, error) {
	sess, err := sm.store.CreateSession(ctx, makePortablePath(path))
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	sm.setCurrent(sess.ID)
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
	sess.Messages = msgs
	sm.setCurrent(sess.ID)
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
	sess.Messages = msgs
	sm.setCurrent(sess.ID)
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

	for _, msg := range msgs {
		if err := sm.store.AppendMessage(ctx, forked.ID, msg); err != nil {
			return nil, fmt.Errorf("append message: %w", err)
		}
	}

	sm.setCurrent(forked.ID)
	forked.Path = resolvePortablePath(forked.Path)
	forked.Messages = msgs
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
