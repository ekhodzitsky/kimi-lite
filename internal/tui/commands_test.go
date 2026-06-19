package tui

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func (f *fakeSessionManager) CurrentSessionID() string                           { return f.currentID }
func (f *fakeSessionManager) ClearMessages(ctx context.Context, id string) error { return nil }
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
func (f *fakeSessionManager) Resume(ctx context.Context, id string) (*api.Session, error) {
	return &api.Session{ID: id, Path: "/tmp/proj"}, nil
}
func (f *fakeSessionManager) List(ctx context.Context, path string) ([]api.Session, error) {
	return nil, nil
}
func (f *fakeSessionManager) ListAll(ctx context.Context, limit int) ([]api.Session, error) {
	return nil, nil
}

func TestCommandTitle(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "s1", Path: "/tmp", Name: "Old"}
	m, _ := New(cfg, session, context.Background(), "")
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
	m, _ := New(cfg, session, context.Background(), "")
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
	m, _ := New(cfg, session, context.Background(), "")
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
	m, _ := New(cfg, session, context.Background(), "")
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
	m, _ := New(cfg, session, context.Background(), "")
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
	m, _ := New(cfg, session, context.Background(), "")
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

// fakeStore is a test double for api.Store that records export/import calls.
type fakeStore struct {
	createdSession *api.Session
	createErr      error
	appendErr      error
	saveTurnErr    error
	getSessionErr  error
	messages       []api.Message
	turns          []api.Turn
	deletedID      string
	clearedID      string
}

func (f *fakeStore) CreateSession(ctx context.Context, path string) (*api.Session, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	s := &api.Session{ID: "imported", Path: path, Name: "Imported"}
	f.createdSession = s
	return s, nil
}
func (f *fakeStore) GetSession(ctx context.Context, id string) (*api.Session, error) {
	if f.getSessionErr != nil {
		return nil, f.getSessionErr
	}
	return &api.Session{ID: id, Path: "/tmp", Name: "ExportMe"}, nil
}
func (f *fakeStore) GetLastSession(ctx context.Context, path string) (*api.Session, error) {
	return nil, nil
}
func (f *fakeStore) ListSessions(ctx context.Context, path string, limit int) ([]api.Session, error) {
	return nil, nil
}
func (f *fakeStore) ListAllSessions(ctx context.Context, limit int) ([]api.Session, error) {
	return nil, nil
}
func (f *fakeStore) UpdateSession(ctx context.Context, session *api.Session) error { return nil }
func (f *fakeStore) DeleteSession(ctx context.Context, id string) error {
	f.deletedID = id
	return nil
}
func (f *fakeStore) AppendMessage(ctx context.Context, sessionID string, msg api.Message) error {
	if f.appendErr != nil {
		return f.appendErr
	}
	f.messages = append(f.messages, msg)
	return nil
}
func (f *fakeStore) GetMessages(ctx context.Context, sessionID string, limit int) ([]api.Message, error) {
	return f.messages, nil
}
func (f *fakeStore) ClearMessages(ctx context.Context, id string) error {
	f.clearedID = id
	return nil
}
func (f *fakeStore) ReplaceMessages(ctx context.Context, sessionID string, msgs []api.Message) error {
	f.messages = msgs
	return nil
}
func (f *fakeStore) SaveTurn(ctx context.Context, sessionID string, turn api.Turn) error {
	if f.saveTurnErr != nil {
		return f.saveTurnErr
	}
	f.turns = append(f.turns, turn)
	return nil
}
func (f *fakeStore) GetTurns(ctx context.Context, sessionID string, limit int) ([]api.Turn, error) {
	return f.turns, nil
}
func (f *fakeStore) CountTurns(ctx context.Context, sessionID string, state api.TurnState) (int, error) {
	return 0, nil
}
func (f *fakeStore) NextTurnSeq(ctx context.Context, sessionID string) (int, error) {
	return 1, nil
}
func (f *fakeStore) Close() error { return nil }

// fakeLLMClient is a test double that records model changes.
type fakeLLMClient struct {
	model string
}

func (f *fakeLLMClient) Chat(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (*api.Message, error) {
	return nil, nil
}
func (f *fakeLLMClient) ChatStream(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
	return nil, nil
}
func (f *fakeLLMClient) Models() []api.ModelInfo { return nil }
func (f *fakeLLMClient) SetModel(model string)   { f.model = model }

func TestCommandExport(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "s1", Path: t.TempDir(), Name: "ExportMe"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()

	store := &fakeStore{
		messages: []api.Message{{ID: "m1", Role: api.RoleUser, Content: "hello", CreatedAt: time.Now().UTC()}},
		turns:    []api.Turn{{ID: "t1", Seq: 1, State: api.TurnIdle}},
	}
	m.SetStore(store)

	path := filepath.Join(t.TempDir(), "out.json")
	updated, cmd := m.Update(input.SendMsg{Content: "/export " + path})
	model := updated.(*Model)

	if cmd == nil {
		t.Fatal("expected async command for /export")
	}

	msg := cmd()
	updated2, _ := model.Update(msg)
	model2 := updated2.(*Model)

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected export file to exist: %v", err)
	}

	found := false
	for _, msg := range model2.messages {
		if strings.Contains(msg.Content, "exported to") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected export confirmation, got: %v", model2.messages)
	}
	if model2.state != api.TurnIdle {
		t.Errorf("state = %d, want TurnIdle", model2.state)
	}
}

func TestCommandExport_DefaultPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfg := config.DefaultConfig()
	session := &api.Session{ID: "s1", Path: dir, Name: "MySession"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()
	m.SetStore(&fakeStore{})

	// Change working directory to dir so default path lands there.
	cwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(cwd)

	updated, cmd := m.Update(input.SendMsg{Content: "/export"})
	model := updated.(*Model)
	msg := cmd()
	model.Update(msg)

	expected := filepath.Join(dir, "MySession.json")
	if _, err := os.Stat(expected); err != nil {
		t.Errorf("expected default export file %q to exist: %v", expected, err)
	}
}

func TestCommandExport_StoreError(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "s1", Path: t.TempDir()}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()
	m.SetStore(&fakeStore{getSessionErr: errors.New("store down")})

	updated, cmd := m.Update(input.SendMsg{Content: "/export out.json"})
	model := updated.(*Model)
	msg := cmd()
	updated2, _ := model.Update(msg)
	model2 := updated2.(*Model)

	view := model2.vp.View().Content
	if !strings.Contains(view, "store down") {
		t.Errorf("viewport should show error, got %q", view)
	}
	if model2.state != api.TurnError {
		t.Errorf("state = %d, want TurnError", model2.state)
	}
}

func TestCommandImport(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "s1", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()

	store := &fakeStore{}
	m.SetStore(store)

	export := api.SessionExport{
		Version:  api.SessionExportVersion,
		Session:  api.Session{ID: "orig", Path: "/tmp", Name: "Orig"},
		Messages: []api.Message{{ID: "m1", Role: api.RoleUser, Content: "hi", CreatedAt: time.Now().UTC()}},
		Turns:    []api.Turn{{ID: "t1", Seq: 1, State: api.TurnIdle}},
	}
	path := filepath.Join(t.TempDir(), "in.json")
	data, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		t.Fatalf("marshal export: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write export: %v", err)
	}

	updated, cmd := m.Update(input.SendMsg{Content: "/import " + path})
	model := updated.(*Model)
	if cmd == nil {
		t.Fatal("expected async command for /import")
	}

	msg := cmd()
	updated2, _ := model.Update(msg)
	model2 := updated2.(*Model)

	if model2.session == nil || model2.session.ID != "imported" {
		t.Errorf("session = %v, want imported session", model2.session)
	}
	if len(store.messages) != 1 || store.messages[0].Content != "hi" {
		t.Errorf("expected imported message, got %v", store.messages)
	}
	if len(store.turns) != 1 {
		t.Errorf("expected imported turn, got %v", store.turns)
	}

	found := false
	for _, msg := range model2.messages {
		if strings.Contains(msg.Content, "Imported session") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected import confirmation, got: %v", model2.messages)
	}
}

func TestCommandImport_MissingPath(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "s1", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()

	updated, cmd := m.Update(input.SendMsg{Content: "/import"})
	model := updated.(*Model)

	if cmd != nil {
		t.Error("expected no command for /import without path")
	}
	found := false
	for _, msg := range model.messages {
		if strings.Contains(msg.Content, "usage: /import") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected usage error, got: %v", model.messages)
	}
}

func TestCommandModel_KnownModel(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "s1", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()

	llmClient := &fakeLLMClient{}
	m.SetLLMClient(llmClient)

	updated, cmd := m.Update(input.SendMsg{Content: "/model gpt-4o"})
	model := updated.(*Model)
	if cmd == nil {
		t.Fatal("expected async command for /model")
	}

	msg := cmd()
	updated2, _ := model.Update(msg)
	model2 := updated2.(*Model)

	if model2.modelName != "gpt-4o" {
		t.Errorf("modelName = %q, want %q", model2.modelName, "gpt-4o")
	}
	if model2.config.LLM.Model != "gpt-4o" {
		t.Errorf("config.LLM.Model = %q, want %q", model2.config.LLM.Model, "gpt-4o")
	}
	if llmClient.model != "gpt-4o" {
		t.Errorf("llm client model = %q, want %q", llmClient.model, "gpt-4o")
	}
}

func TestCommandModel_Alias(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	cfg.Models["smart"] = api.ModelAlias{Provider: "openai", Model: "gpt-4o"}
	session := &api.Session{ID: "s1", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()

	updated, cmd := m.Update(input.SendMsg{Content: "/model smart"})
	model := updated.(*Model)
	msg := cmd()
	updated2, _ := model.Update(msg)
	model2 := updated2.(*Model)

	if model2.modelName != "gpt-4o" {
		t.Errorf("modelName = %q, want %q", model2.modelName, "gpt-4o")
	}
}

func TestCommandModel_Unknown(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "s1", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()

	updated, cmd := m.Update(input.SendMsg{Content: "/model not-a-real-model"})
	model := updated.(*Model)

	if cmd != nil {
		t.Error("expected no command for unknown model")
	}
	found := false
	for _, msg := range model.messages {
		if strings.Contains(msg.Content, "unknown model") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected unknown model error, got: %v", model.messages)
	}
}

func TestCommandGoal(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "s1", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()

	store := &fakeStore{}
	m.SetStore(store)

	updated, cmd := m.Update(input.SendMsg{Content: "/goal write tests"})
	model := updated.(*Model)
	if cmd == nil {
		t.Fatal("expected async command for /goal")
	}

	msg := cmd()
	updated2, asyncCmd := model.Update(msg)
	model2 := updated2.(*Model)

	if model2.goal != "write tests" {
		t.Errorf("goal = %q, want %q", model2.goal, "write tests")
	}
	if model2.statusText != "Goal: write tests" {
		t.Errorf("statusText = %q, want %q", model2.statusText, "Goal: write tests")
	}

	// Run the async system-message append command.
	if asyncCmd == nil {
		t.Fatal("expected async command for goal append")
	}
	if m := asyncCmd(); m != nil {
		errMsg, ok := m.(ErrorMsg)
		if ok {
			t.Fatalf("unexpected error: %v", errMsg.Err)
		}
	}
	var systemMsgFound bool
	for _, sm := range store.messages {
		if sm.Role == api.RoleSystem && strings.Contains(sm.Content, "User goal: write tests") {
			systemMsgFound = true
			break
		}
	}
	if !systemMsgFound {
		t.Errorf("expected system goal message in store, got %v", store.messages)
	}
}

func TestCommandBTW(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "s1", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()

	updated, cmd := m.Update(input.SendMsg{Content: "/btw remember this"})
	model := updated.(*Model)
	if cmd == nil {
		t.Fatal("expected async command for /btw")
	}

	msg := cmd()
	updated2, _ := model.Update(msg)
	model2 := updated2.(*Model)

	if model2.btwNote != "remember this" {
		t.Errorf("btwNote = %q, want %q", model2.btwNote, "remember this")
	}

	// Next message should include the BTW note.
	tm := &fakeTurnManager{}
	model2.SetTurnManager(tm)
	model2.Update(input.SendMsg{Content: "hello"})

	// fakeTurnManager is not captured directly; instead verify the displayed user message.
	found := false
	for _, msg := range model2.messages {
		if strings.Contains(msg.Content, "remember this") && strings.Contains(msg.Content, "hello") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected BTW note prepended to next message, got: %v", model2.messages)
	}
}

func TestCommandVersion(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "s1", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()

	updated, cmd := m.Update(input.SendMsg{Content: "/version"})
	model := updated.(*Model)

	if cmd != nil {
		t.Error("expected no async command for /version")
	}

	found := false
	for _, msg := range model.messages {
		if strings.Contains(msg.Content, "kimi-lite version") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected version message, got: %v", model.messages)
	}
}

// Verify fakeSessionManager satisfies the interface used by the TUI.
var _ sessionManager = (*fakeSessionManager)(nil)

// Ensure command messages are recognized as tea.Msg.
var _ tea.Msg = (*SetTitleMsg)(nil)
var _ tea.Msg = (*ForkResultMsg)(nil)
var _ tea.Msg = (*ModelSwitchedMsg)(nil)
var _ tea.Msg = (*GoalSetMsg)(nil)
var _ tea.Msg = (*BTWMsg)(nil)
var _ tea.Msg = (*ExportResultMsg)(nil)
var _ tea.Msg = (*ImportResultMsg)(nil)
var _ api.Store = (*fakeStore)(nil)
var _ api.LLMClient = (*fakeLLMClient)(nil)
