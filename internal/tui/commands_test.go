package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ekhodzitsky/kimi-lite/internal/config"
	"github.com/ekhodzitsky/kimi-lite/internal/tui/input"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// fakeSessionManager is a test double for the session manager that records
// command invocations and returns configured results.
type fakeSessionManager struct {
	currentID    string
	renameCalled bool
	renameID     string
	renameName   string
	renameErr    error
	forkCalled   bool
	forkSourceID string
	forkName     string
	forkResult   *api.Session
	forkErr      error
}

func (f *fakeSessionManager) CurrentSessionID() string { return f.currentID }
func (f *fakeSessionManager) ClearMessages(ctx context.Context, id string) error {
	return nil
}
func (f *fakeSessionManager) Rename(ctx context.Context, id string, name string) error {
	f.renameCalled = true
	f.renameID = id
	f.renameName = name
	return f.renameErr
}
func (f *fakeSessionManager) Fork(ctx context.Context, sourceID string, name string) (*api.Session, error) {
	f.forkCalled = true
	f.forkSourceID = sourceID
	f.forkName = name
	return f.forkResult, f.forkErr
}

func TestCommandTitle(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "s1", Path: "/tmp", Name: "Old"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	sm := &fakeSessionManager{currentID: session.ID}
	m.SetSessionManager(sm)

	updated, cmd := m.Update(input.SendMsg{Content: "/title New Name"})
	model := updated.(*Model)

	if cmd == nil {
		t.Fatal("expected async command for /title")
	}

	msg := cmd()
	updated2, _ := model.Update(msg)
	model2 := updated2.(*Model)

	if !sm.renameCalled {
		t.Error("expected Rename to be called")
	}
	if sm.renameID != "s1" {
		t.Errorf("rename id = %q, want %q", sm.renameID, "s1")
	}
	if sm.renameName != "New Name" {
		t.Errorf("rename name = %q, want %q", sm.renameName, "New Name")
	}
	if model2.session.Name != "New Name" {
		t.Errorf("session name = %q, want %q", model2.session.Name, "New Name")
	}

	found := false
	for _, msg := range model2.messages {
		if strings.Contains(msg.Content, "renamed to \"New Name\"") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected confirmation message, got: %v", model2.messages)
	}
	if model2.state != api.TurnIdle {
		t.Errorf("state = %d, want TurnIdle", model2.state)
	}
}

func TestCommandTitle_MissingName(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "s1", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	updated, cmd := m.Update(input.SendMsg{Content: "/title"})
	model := updated.(*Model)

	if cmd != nil {
		t.Error("expected no command for /title without name")
	}

	found := false
	for _, msg := range model.messages {
		if strings.Contains(msg.Content, "usage: /title") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected usage error, got: %v", model.messages)
	}
}

func TestCommandTitle_Error(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "s1", Path: "/tmp", Name: "Old"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	sm := &fakeSessionManager{renameErr: errors.New("db failed")}
	m.SetSessionManager(sm)

	updated, cmd := m.Update(input.SendMsg{Content: "/title New Name"})
	model := updated.(*Model)

	msg := cmd()
	updated2, _ := model.Update(msg)
	model2 := updated2.(*Model)

	view := model2.vp.View().Content
	if !strings.Contains(view, "db failed") {
		t.Errorf("viewport should show error, got %q", view)
	}
}

func TestCommandFork(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "s1", Path: "/tmp", Name: "Original"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	forked := &api.Session{ID: "s2", Path: "/tmp", Name: "Forked"}
	sm := &fakeSessionManager{currentID: session.ID, forkResult: forked}
	m.SetSessionManager(sm)

	updated, cmd := m.Update(input.SendMsg{Content: "/fork Forked"})
	model := updated.(*Model)

	if cmd == nil {
		t.Fatal("expected async command for /fork")
	}

	msg := cmd()
	updated2, _ := model.Update(msg)
	model2 := updated2.(*Model)

	if !sm.forkCalled {
		t.Error("expected Fork to be called")
	}
	if sm.forkSourceID != "s1" {
		t.Errorf("fork source = %q, want %q", sm.forkSourceID, "s1")
	}
	if sm.forkName != "Forked" {
		t.Errorf("fork name = %q, want %q", sm.forkName, "Forked")
	}
	if model2.session.ID != "s2" {
		t.Errorf("session id = %q, want %q", model2.session.ID, "s2")
	}

	found := false
	for _, msg := range model2.messages {
		if strings.Contains(msg.Content, "Forked to session") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected confirmation message, got: %v", model2.messages)
	}
	if model2.state != api.TurnIdle {
		t.Errorf("state = %d, want TurnIdle", model2.state)
	}
}

func TestCommandFork_DefaultName(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "s1", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	forked := &api.Session{ID: "s2", Path: "/tmp", Name: "Fork of s1"}
	sm := &fakeSessionManager{currentID: session.ID, forkResult: forked}
	m.SetSessionManager(sm)

	updated, cmd := m.Update(input.SendMsg{Content: "/fork"})
	model := updated.(*Model)

	if cmd == nil {
		t.Fatal("expected async command for /fork")
	}

	msg := cmd()
	updated2, _ := model.Update(msg)
	model2 := updated2.(*Model)

	if sm.forkName != "" {
		t.Errorf("fork name = %q, want empty", sm.forkName)
	}
	if model2.session.ID != "s2" {
		t.Errorf("session id = %q, want %q", model2.session.ID, "s2")
	}
}

func TestCommandFork_Error(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "s1", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background())
	m.width = 120
	m.height = 40
	m.updateLayout()

	sm := &fakeSessionManager{forkErr: errors.New("fork failed")}
	m.SetSessionManager(sm)

	updated, cmd := m.Update(input.SendMsg{Content: "/fork"})
	model := updated.(*Model)

	msg := cmd()
	updated2, _ := model.Update(msg)
	model2 := updated2.(*Model)

	view := model2.vp.View().Content
	if !strings.Contains(view, "fork failed") {
		t.Errorf("viewport should show error, got %q", view)
	}
}

// Verify fakeSessionManager satisfies the interface used by the TUI.
var _ sessionManager = (*fakeSessionManager)(nil)

// Ensure command messages are recognized as tea.Msg.
var _ tea.Msg = (*SetTitleMsg)(nil)
var _ tea.Msg = (*ForkResultMsg)(nil)
