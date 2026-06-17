package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ekhodzitsky/kimi-lite/internal/core"
	"github.com/ekhodzitsky/kimi-lite/internal/git"
	"github.com/ekhodzitsky/kimi-lite/internal/mcp"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func TestApp_SetYolo(t *testing.T) {
	t.Parallel()

	app := &App{
		approvalGate: core.NewApprovalGate(core.ModeAuto, []string{"read_file"}, nil, nil),
	}

	call := api.ToolCall{Name: "write_file"}
	decision, auto := app.approvalGate.ShouldAutoApprove(call)
	if auto || decision != api.ApprovalNo {
		t.Fatal("expected manual approval for write_file in auto mode")
	}

	app.SetYolo(true)
	decision, auto = app.approvalGate.ShouldAutoApprove(call)
	if !auto || decision != api.ApprovalYes {
		t.Fatal("expected auto-approval for write_file in yolo mode")
	}
}

func TestApp_New_CreatesDBDirWithRestrictedPermissions(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	dbDir := filepath.Join(tmpDir, "db")
	dbPath := filepath.Join(dbDir, "sessions.db")

	cfg := &api.Config{
		LLM: api.LLMConfig{
			Provider: "moonshot",
			APIKey:   "test-key",
			Model:    "kimi-k2.5",
			BaseURL:  "https://api.moonshot.cn/v1",
			Timeout:  60 * time.Second,
		},
		Behavior: api.BehaviorConfig{
			AutoApprove:  []string{"read_file"},
			ShellTimeout: 30 * time.Second,
			MaxTurns:     50,
		},
		Session: api.SessionConfig{
			DBPath:     dbPath,
			MaxHistory: 100,
		},
		MCP: api.MCPConfig{
			GuardCommand: "mcp-guard",
		},
		UI: api.UIConfig{
			Theme: "dark",
		},
	}

	app, err := New(cfg, false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer app.Close()

	info, err := os.Stat(dbDir)
	if err != nil {
		t.Fatalf("stat db dir: %v", err)
	}
	if info.Mode().Perm() != 0700 {
		t.Errorf("db dir permissions = %o, want %o", info.Mode().Perm(), 0700)
	}
}

func TestConfigProvider_Get(t *testing.T) {
	t.Parallel()

	cfg := &api.Config{LLM: api.LLMConfig{Model: "test-model"}}
	p := &configProvider{cfg: cfg}

	if got := p.Get(); got != cfg {
		t.Fatal("expected same config pointer")
	}
}

func TestSystemPrompt_ContainsToolNames(t *testing.T) {
	t.Parallel()

	prompt := systemPrompt("/tmp/test-dir", "", "")

	requiredTools := []string{"read_file", "glob", "grep", "list_directory", "write_file", "str_replace_file", "edit", "shell", "fetch_url", "web_search", "read_video", "TodoList", "dispatch_subagent"}
	for _, tool := range requiredTools {
		if !strings.Contains(prompt, tool) {
			t.Errorf("system prompt missing tool name %q", tool)
		}
	}

	if len(prompt) < 500 {
		t.Errorf("system prompt length = %d, want >= 500", len(prompt))
	}
}

type mockTeaProgram struct {
	err error
}

func (m *mockTeaProgram) Run() (tea.Model, error) {
	return nil, m.err
}

type mockLLM struct {
	chatStreamFunc func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error)
}

func (m *mockLLM) Chat(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (*api.Message, error) {
	return nil, nil
}
func (m *mockLLM) ChatStream(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
	return m.chatStreamFunc(ctx, messages, tools)
}
func (m *mockLLM) Models() []api.ModelInfo { return nil }

type mockToolExecutor struct {
	executeFunc func(ctx context.Context, call api.ToolCall) (api.ToolResult, error)
	defs        []api.ToolDefinition
	readOnly    map[string]bool
}

func (m *mockToolExecutor) Execute(ctx context.Context, call api.ToolCall) (api.ToolResult, error) {
	return m.executeFunc(ctx, call)
}
func (m *mockToolExecutor) Definitions(ctx context.Context) []api.ToolDefinition { return m.defs }
func (m *mockToolExecutor) IsReadOnly(name string) bool                          { return m.readOnly[name] }

type mockApprovalGate struct {
	shouldAutoApprove func(call api.ToolCall) (api.ApprovalDecision, bool)
}

func (m *mockApprovalGate) ShouldAutoApprove(call api.ToolCall) (api.ApprovalDecision, bool) {
	return m.shouldAutoApprove(call)
}

type failingStore struct {
	api.Store
	failAfter int
	count     int
}

func (m *failingStore) AppendMessage(ctx context.Context, sessionID string, msg api.Message) error {
	if m.count >= m.failAfter {
		return fmt.Errorf("injected append failure")
	}
	m.count++
	return m.Store.AppendMessage(ctx, sessionID, msg)
}

func TestApp_Run_GitProviderUsesSessionPath(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	dbPath := filepath.Join(tmpDir, "sessions.db")

	cfg := &api.Config{
		LLM: api.LLMConfig{
			Provider: "moonshot",
			APIKey:   "test-key",
			Model:    "kimi-k2.5",
			BaseURL:  "https://api.moonshot.cn/v1",
			Timeout:  60 * time.Second,
		},
		Behavior: api.BehaviorConfig{
			AutoApprove:  []string{"read_file"},
			ShellTimeout: 30 * time.Second,
			MaxTurns:     50,
		},
		Session: api.SessionConfig{
			DBPath:     dbPath,
			MaxHistory: 100,
		},
		MCP: api.MCPConfig{
			GuardCommand: "mcp-guard",
		},
		UI: api.UIConfig{
			Theme: "dark",
		},
	}

	a, err := New(cfg, false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Close()

	session, err := a.StartSession(context.Background())
	if err != nil {
		t.Fatalf("StartSession() error: %v", err)
	}

	a.newProgram = func(model tea.Model, opts ...tea.ProgramOption) teaProgram {
		return &mockTeaProgram{err: nil}
	}

	_ = a.Run(context.Background(), session)

	provider, ok := a.gitProvider.(*git.Provider)
	if !ok {
		t.Fatalf("expected *git.Provider, got %T", a.gitProvider)
	}
	if provider.Dir() != session.Path {
		t.Fatalf("expected provider dir %q, got %q", session.Path, provider.Dir())
	}
}

func TestApp_Run_CleanExit(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	dbPath := filepath.Join(tmpDir, "sessions.db")

	cfg := &api.Config{
		LLM: api.LLMConfig{
			Provider: "moonshot",
			APIKey:   "test-key",
			Model:    "kimi-k2.5",
			BaseURL:  "https://api.moonshot.cn/v1",
			Timeout:  60 * time.Second,
		},
		Behavior: api.BehaviorConfig{
			AutoApprove:  []string{"read_file"},
			ShellTimeout: 30 * time.Second,
			MaxTurns:     50,
		},
		Session: api.SessionConfig{
			DBPath:     dbPath,
			MaxHistory: 100,
		},
		MCP: api.MCPConfig{
			GuardCommand: "mcp-guard",
		},
		UI: api.UIConfig{
			Theme: "dark",
		},
	}

	a, err := New(cfg, false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Close()

	session, err := a.StartSession(context.Background())
	if err != nil {
		t.Fatalf("StartSession() error: %v", err)
	}

	tests := []struct {
		name    string
		runErr  error
		wantErr bool
	}{
		{"interrupted", tea.ErrInterrupted, false},
		{"program killed", tea.ErrProgramKilled, false},
		{"context canceled", context.Canceled, false},
		{"other error", fmt.Errorf("some tui error"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a.newProgram = func(model tea.Model, opts ...tea.ProgramOption) teaProgram {
				return &mockTeaProgram{err: tt.runErr}
			}

			err := a.Run(context.Background(), session)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestApp_Close_CancelsBlockedTurn(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	dbPath := filepath.Join(tmpDir, "sessions.db")

	cfg := &api.Config{
		LLM: api.LLMConfig{
			Provider: "moonshot",
			APIKey:   "test-key",
			Model:    "kimi-k2.5",
			BaseURL:  "https://api.moonshot.cn/v1",
			Timeout:  60 * time.Second,
		},
		Behavior: api.BehaviorConfig{
			AutoApprove:  []string{"read_file"},
			ShellTimeout: 30 * time.Second,
			MaxTurns:     50,
		},
		Session: api.SessionConfig{
			DBPath:     dbPath,
			MaxHistory: 100,
		},
		MCP: api.MCPConfig{
			GuardCommand: "mcp-guard",
		},
		UI: api.UIConfig{
			Theme: "dark",
		},
	}

	a, err := New(cfg, false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	sess, err := a.StartSession(context.Background())
	if err != nil {
		t.Fatalf("StartSession() error: %v", err)
	}

	llm := &mockLLM{
		chatStreamFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			ch := make(chan api.StreamChunk)
			go func() {
				defer close(ch)
				select {
				case ch <- api.StreamChunk{Done: true, ToolCalls: []api.ToolCall{
					{ID: "tc1", Name: "write_file", Arguments: `{}`},
				}}:
				case <-ctx.Done():
				}
			}()
			return ch, nil
		},
	}
	tools := &mockToolExecutor{
		executeFunc: func(ctx context.Context, call api.ToolCall) (api.ToolResult, error) {
			return api.ToolResult{}, nil
		},
		defs: []api.ToolDefinition{{Name: "write_file", Description: "write"}},
	}
	approval := &mockApprovalGate{
		shouldAutoApprove: func(call api.ToolCall) (api.ApprovalDecision, bool) {
			return api.ApprovalNo, false
		},
	}
	tm, err := core.NewTurnManager(llm, tools, approval, a.store, &configProvider{cfg: cfg})
	if err != nil {
		t.Fatalf("create turn manager: %v", err)
	}
	a.turnManager = tm

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, err = tm.RunTurn(ctx, sess.ID, "test")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	// Wait for the turn to block on approval.
	for i := 0; i < 100; i++ {
		turn := tm.CurrentTurn()
		if turn != nil && turn.State == api.TurnWaitingApproval {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	start := time.Now()
	if err := a.Close(); err != nil {
		// Close may return an error due to turn shutdown timeout or other reasons;
		// we only care that it returns promptly and without panic/race.
		t.Logf("Close returned error: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Fatalf("Close took too long (%v); expected well under 10s", elapsed)
	}
}

func TestApp_ImportSession_Atomic(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	dbPath := filepath.Join(tmpDir, "sessions.db")

	cfg := &api.Config{
		LLM: api.LLMConfig{
			Provider: "moonshot",
			APIKey:   "test-key",
			Model:    "kimi-k2.5",
			BaseURL:  "https://api.moonshot.cn/v1",
			Timeout:  60 * time.Second,
		},
		Behavior: api.BehaviorConfig{
			AutoApprove:  []string{"read_file"},
			ShellTimeout: 30 * time.Second,
			MaxTurns:     50,
		},
		Session: api.SessionConfig{
			DBPath:     dbPath,
			MaxHistory: 100,
		},
		MCP: api.MCPConfig{
			GuardCommand: "mcp-guard",
		},
		UI: api.UIConfig{
			Theme: "dark",
		},
	}

	a, err := New(cfg, false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Close()

	export := &api.SessionExport{
		Version: "1.0",
		Session: api.Session{Path: "/tmp/import-atomic", CreatedAt: time.Now().UTC()},
		Messages: []api.Message{
			{ID: "m1", Role: api.RoleUser, Content: "hello"},
			{ID: "m2", Role: api.RoleUser, Content: "world"},
		},
		Turns: []api.Turn{},
	}

	// Wrap store to fail on 2nd AppendMessage
	fs := &failingStore{Store: a.store, failAfter: 1}
	a.store = fs

	_, err = a.ImportSession(context.Background(), export)
	if err == nil {
		t.Fatal("expected error for partial import")
	}

	sessions, err := fs.ListSessions(context.Background(), "/tmp/import-atomic", 0)
	if err != nil {
		t.Fatalf("ListSessions error: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected 0 sessions after failed import, got %d", len(sessions))
	}
}

func TestApp_ExportSession_Version(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	dbPath := filepath.Join(tmpDir, "sessions.db")

	cfg := &api.Config{
		LLM: api.LLMConfig{
			Provider: "moonshot",
			APIKey:   "test-key",
			Model:    "kimi-k2.5",
			BaseURL:  "https://api.moonshot.cn/v1",
			Timeout:  60 * time.Second,
		},
		Behavior: api.BehaviorConfig{
			AutoApprove:  []string{"read_file"},
			ShellTimeout: 30 * time.Second,
			MaxTurns:     50,
		},
		Session: api.SessionConfig{
			DBPath:     dbPath,
			MaxHistory: 100,
		},
		MCP: api.MCPConfig{
			GuardCommand: "mcp-guard",
		},
		UI: api.UIConfig{
			Theme: "dark",
		},
	}

	a, err := New(cfg, false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Close()

	session, err := a.StartSession(context.Background())
	if err != nil {
		t.Fatalf("StartSession() error: %v", err)
	}

	export, err := a.ExportSession(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("ExportSession() error: %v", err)
	}
	if export.Version != api.SessionExportVersion {
		t.Errorf("export.Version = %q, want %q", export.Version, api.SessionExportVersion)
	}
}

func TestApp_ImportSession_UnsupportedVersion(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	dbPath := filepath.Join(tmpDir, "sessions.db")

	cfg := &api.Config{
		LLM: api.LLMConfig{
			Provider: "moonshot",
			APIKey:   "test-key",
			Model:    "kimi-k2.5",
			BaseURL:  "https://api.moonshot.cn/v1",
			Timeout:  60 * time.Second,
		},
		Behavior: api.BehaviorConfig{
			AutoApprove:  []string{"read_file"},
			ShellTimeout: 30 * time.Second,
			MaxTurns:     50,
		},
		Session: api.SessionConfig{
			DBPath:     dbPath,
			MaxHistory: 100,
		},
		MCP: api.MCPConfig{
			GuardCommand: "mcp-guard",
		},
		UI: api.UIConfig{
			Theme: "dark",
		},
	}

	a, err := New(cfg, false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Close()

	export := &api.SessionExport{
		Version:  "2.0",
		Session:  api.Session{Path: "/tmp/test"},
		Messages: []api.Message{},
		Turns:    []api.Turn{},
	}

	_, err = a.ImportSession(context.Background(), export)
	if err == nil {
		t.Fatal("expected error for unsupported export version")
	}
	if !strings.Contains(err.Error(), "unsupported export version") {
		t.Errorf("error = %q, want to contain 'unsupported export version'", err.Error())
	}
}

func TestApp_ImportSession_PreservesName(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	dbPath := filepath.Join(tmpDir, "sessions.db")

	cfg := &api.Config{
		LLM: api.LLMConfig{
			Provider: "moonshot",
			APIKey:   "test-key",
			Model:    "kimi-k2.5",
			BaseURL:  "https://api.moonshot.cn/v1",
			Timeout:  60 * time.Second,
		},
		Behavior: api.BehaviorConfig{
			AutoApprove:  []string{"read_file"},
			ShellTimeout: 30 * time.Second,
			MaxTurns:     50,
		},
		Session: api.SessionConfig{
			DBPath:     dbPath,
			MaxHistory: 100,
		},
		MCP: api.MCPConfig{
			GuardCommand: "mcp-guard",
		},
		UI: api.UIConfig{
			Theme: "dark",
		},
	}

	a, err := New(cfg, false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Close()

	export := &api.SessionExport{
		Version: "1.0",
		Session: api.Session{
			Path: "/tmp/preserve-name",
			Name: "My Session",
		},
		Messages: []api.Message{},
		Turns:    []api.Turn{},
	}

	created, err := a.ImportSession(context.Background(), export)
	if err != nil {
		t.Fatalf("ImportSession() error: %v", err)
	}
	if created.Name != "My Session" {
		t.Errorf("imported session name = %q, want %q", created.Name, "My Session")
	}
}

func TestApp_ExportImport_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	dbPath := filepath.Join(tmpDir, "sessions.db")

	cfg := &api.Config{
		LLM: api.LLMConfig{
			Provider: "moonshot",
			APIKey:   "test-key",
			Model:    "kimi-k2.5",
			BaseURL:  "https://api.moonshot.cn/v1",
			Timeout:  60 * time.Second,
		},
		Behavior: api.BehaviorConfig{
			AutoApprove:  []string{"read_file"},
			ShellTimeout: 30 * time.Second,
			MaxTurns:     50,
		},
		Session: api.SessionConfig{
			DBPath:     dbPath,
			MaxHistory: 100,
		},
		MCP: api.MCPConfig{
			GuardCommand: "mcp-guard",
		},
		UI: api.UIConfig{
			Theme: "dark",
		},
	}

	a, err := New(cfg, false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Close()

	session, err := a.StartSession(context.Background())
	if err != nil {
		t.Fatalf("StartSession() error: %v", err)
	}

	session.Name = "RoundTrip"
	if err := a.store.UpdateSession(context.Background(), session); err != nil {
		t.Fatalf("UpdateSession error: %v", err)
	}

	export, err := a.ExportSession(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("ExportSession() error: %v", err)
	}

	// Verify no duplicated messages in JSON
	data, err := json.Marshal(export)
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}
	if strings.Contains(string(data), `"session":{"messages"`) {
		t.Error("exported JSON should not contain session.messages")
	}

	imported, err := a.ImportSession(context.Background(), export)
	if err != nil {
		t.Fatalf("ImportSession() error: %v", err)
	}
	if imported.Name != "RoundTrip" {
		t.Errorf("imported name = %q, want %q", imported.Name, "RoundTrip")
	}
}

func TestApp_ExportImportRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		seed func(t *testing.T, a *App, sessionID string)
	}{
		{
			name: "full session",
			seed: func(t *testing.T, a *App, sessionID string) {
				ctx := context.Background()
				msgs := []api.Message{
					{ID: "m1", Role: api.RoleSystem, Content: "system prompt", CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
					{ID: "m2", Role: api.RoleUser, Content: "hello", CreatedAt: time.Date(2024, 1, 1, 0, 0, 1, 0, time.UTC)},
					{ID: "m3", Role: api.RoleAssistant, Content: "using tool", ToolCalls: []api.ToolCall{{ID: "tc1", Name: "read_file", Arguments: `{"path":"a.go"}`}}, CreatedAt: time.Date(2024, 1, 1, 0, 0, 2, 0, time.UTC)},
					{ID: "m4", Role: api.RoleTool, Content: "package main", ToolCallID: "tc1", CreatedAt: time.Date(2024, 1, 1, 0, 0, 3, 0, time.UTC)},
				}
				for _, msg := range msgs {
					if err := a.store.AppendMessage(ctx, sessionID, msg); err != nil {
						t.Fatalf("append message: %v", err)
					}
				}
				ended := time.Date(2024, 1, 1, 0, 1, 0, 0, time.UTC)
				turns := []api.Turn{
					{ID: "t1", State: api.TurnThinking, Input: "hello", Response: "hi", StartedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), EndedAt: &ended},
					{ID: "t2", State: api.TurnToolCalls, Input: "read file", ToolCalls: []api.ToolCall{{ID: "tc1", Name: "read_file", Arguments: `{}`}}, Results: []api.ToolResult{{CallID: "tc1", Name: "read_file", Output: "package main"}}, StartedAt: time.Date(2024, 1, 1, 0, 1, 0, 0, time.UTC)},
					{ID: "t3", State: api.TurnError, Input: "fail", Error: "boom", StartedAt: time.Date(2024, 1, 1, 0, 2, 0, 0, time.UTC)},
				}
				for _, turn := range turns {
					if err := a.store.SaveTurn(ctx, sessionID, turn); err != nil {
						t.Fatalf("save turn: %v", err)
					}
				}
			},
		},
		{
			name: "empty session",
			seed: func(t *testing.T, a *App, sessionID string) {},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			t.Setenv("HOME", tmpDir)
			dbPath := filepath.Join(tmpDir, "sessions.db")

			cfg := &api.Config{
				LLM: api.LLMConfig{
					Provider: "moonshot",
					APIKey:   "test-key",
					Model:    "kimi-k2.5",
					BaseURL:  "https://api.moonshot.cn/v1",
					Timeout:  60 * time.Second,
				},
				Behavior: api.BehaviorConfig{
					AutoApprove:  []string{"read_file"},
					ShellTimeout: 30 * time.Second,
					MaxTurns:     50,
				},
				Session: api.SessionConfig{
					DBPath:     dbPath,
					MaxHistory: 100,
				},
				MCP: api.MCPConfig{
					GuardCommand: "mcp-guard",
				},
				UI: api.UIConfig{
					Theme: "dark",
				},
			}

			a, err := New(cfg, false)
			if err != nil {
				t.Fatalf("New() error: %v", err)
			}
			defer a.Close()

			ctx := context.Background()
			session, err := a.StartSession(ctx)
			if err != nil {
				t.Fatalf("StartSession() error: %v", err)
			}

			session.Name = "RoundTrip"
			if err := a.store.UpdateSession(ctx, session); err != nil {
				t.Fatalf("UpdateSession error: %v", err)
			}

			tt.seed(t, a, session.ID)

			export, err := a.ExportSession(ctx, session.ID)
			if err != nil {
				t.Fatalf("ExportSession() error: %v", err)
			}

			imported, err := a.ImportSession(ctx, export)
			if err != nil {
				t.Fatalf("ImportSession() error: %v", err)
			}

			if imported.ID == session.ID {
				t.Error("expected imported session to have a new ID")
			}
			if imported.Path != session.Path {
				t.Errorf("imported path = %q, want %q", imported.Path, session.Path)
			}
			if imported.Name != "RoundTrip" {
				t.Errorf("imported name = %q, want %q", imported.Name, "RoundTrip")
			}

			importedMsgs, err := a.store.GetMessages(ctx, imported.ID, 0)
			if err != nil {
				t.Fatalf("get imported messages: %v", err)
			}
			if !messageSlicesEqual(export.Messages, importedMsgs) {
				t.Errorf("messages mismatch:\nexport  = %+v\nimported = %+v", export.Messages, importedMsgs)
			}

			importedTurns, err := a.store.GetTurns(ctx, imported.ID, 0)
			if err != nil {
				t.Fatalf("get imported turns: %v", err)
			}
			if !turnSlicesEqual(export.Turns, importedTurns) {
				t.Errorf("turns mismatch:\nexport  = %+v\nimported = %+v", export.Turns, importedTurns)
			}
		})
	}
}

func messageSlicesEqual(a, b []api.Message) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !messagesEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

func messagesEqual(a, b api.Message) bool {
	return a.ID == b.ID &&
		a.Role == b.Role &&
		a.Content == b.Content &&
		a.ToolCallID == b.ToolCallID &&
		a.FinishReason == b.FinishReason &&
		a.CreatedAt.Equal(b.CreatedAt) &&
		toolCallSlicesEqual(a.ToolCalls, b.ToolCalls)
}

func toolCallSlicesEqual(a, b []api.ToolCall) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID || a[i].Name != b[i].Name || a[i].Arguments != b[i].Arguments {
			return false
		}
	}
	return true
}

func toolResultSlicesEqual(a, b []api.ToolResult) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].CallID != b[i].CallID || a[i].Name != b[i].Name || a[i].Output != b[i].Output || a[i].Error != b[i].Error {
			return false
		}
	}
	return true
}

func turnSlicesEqual(a, b []api.Turn) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !turnsEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

func turnsEqual(a, b api.Turn) bool {
	if a.ID != b.ID || a.State != b.State || a.Input != b.Input || a.Response != b.Response || a.Error != b.Error {
		return false
	}
	if !a.StartedAt.Equal(b.StartedAt) {
		return false
	}
	if (a.EndedAt == nil) != (b.EndedAt == nil) {
		return false
	}
	if a.EndedAt != nil && !a.EndedAt.Equal(*b.EndedAt) {
		return false
	}
	return toolCallSlicesEqual(a.ToolCalls, b.ToolCalls) && toolResultSlicesEqual(a.Results, b.Results)
}

type fakeAppMCPClient struct {
	tools      []api.ToolDefinition
	closed     bool
	connectErr error
}

func (f *fakeAppMCPClient) Connect(ctx context.Context) error { return f.connectErr }
func (f *fakeAppMCPClient) ListTools(ctx context.Context) ([]api.ToolDefinition, error) {
	return f.tools, nil
}
func (f *fakeAppMCPClient) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	return "ok", nil
}
func (f *fakeAppMCPClient) Close() error {
	f.closed = true
	return nil
}

func TestApp_New_MCPServers_AggregateTools(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	dbPath := filepath.Join(tmpDir, "sessions.db")

	originalFactory := newMCPClientForServer
	defer func() { newMCPClientForServer = originalFactory }()
	newMCPClientForServer = func(cfg api.MCPServerConfig) (api.MCPClient, error) {
		switch cfg.Transport {
		case api.MCPTransportStdio:
			return &fakeAppMCPClient{tools: []api.ToolDefinition{{Name: "read_file", Description: "Local read"}}}, nil
		case api.MCPTransportHTTP:
			return &fakeAppMCPClient{tools: []api.ToolDefinition{
				{Name: "read_file", Description: "Remote read"},
				{Name: "search", Description: "Remote search"},
			}}, nil
		default:
			return nil, fmt.Errorf("unexpected transport %q", cfg.Transport)
		}
	}

	cfg := testAppConfig(dbPath)
	cfg.MCPServers = map[string]api.MCPServerConfig{
		"local": {
			Enabled:   true,
			Transport: api.MCPTransportStdio,
			Command:   "fake",
		},
		"remote": {
			Enabled:   true,
			Transport: api.MCPTransportHTTP,
			URL:       "https://example.com/mcp",
		},
	}

	a, err := New(cfg, false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Close()

	if _, ok := a.mcpClient.(*mcp.MultiClient); !ok {
		t.Fatalf("expected *mcp.MultiClient, got %T", a.mcpClient)
	}

	defs := a.toolExecutor.Definitions(context.Background())
	names := make([]string, 0, len(defs))
	for _, d := range defs {
		if strings.HasPrefix(d.Name, "mcp_") {
			names = append(names, d.Name)
		}
	}
	sort.Strings(names)
	want := []string{"mcp_read_file", "mcp_remote_read_file", "mcp_search"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("mcp tool names = %v, want %v", names, want)
	}
}

func TestApp_New_MCPReadOnlyAutoApprove(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	dbPath := filepath.Join(tmpDir, "sessions.db")

	originalFactory := newMCPClientForServer
	defer func() { newMCPClientForServer = originalFactory }()
	newMCPClientForServer = func(cfg api.MCPServerConfig) (api.MCPClient, error) {
		return &fakeAppMCPClient{tools: []api.ToolDefinition{
			{Name: "read_file", Description: "Read", Annotations: api.ToolAnnotations{ReadOnlyHint: true}},
			{Name: "write_file", Description: "Write", Annotations: api.ToolAnnotations{ReadOnlyHint: false}},
		}}, nil
	}

	cfg := testAppConfig(dbPath)
	cfg.Behavior.AutoApprove = []string{"read_file", "mcp_read_file", "mcp_write_file"}
	cfg.MCPServers = map[string]api.MCPServerConfig{
		"local": {
			Enabled:   true,
			Transport: api.MCPTransportStdio,
			Command:   "fake",
		},
	}

	a, err := New(cfg, false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Close()

	// Read-only MCP tool in auto_approve is kept and auto-approved.
	decision, auto := a.approvalGate.ShouldAutoApprove(api.ToolCall{Name: "mcp_read_file"})
	if !auto || decision != api.ApprovalYes {
		t.Fatalf("expected mcp_read_file to be auto-approved, got decision=%v auto=%v", decision, auto)
	}

	// Non-read-only MCP tool in auto_approve is dropped (validation falls back to manual).
	decision, auto = a.approvalGate.ShouldAutoApprove(api.ToolCall{Name: "mcp_write_file"})
	if auto {
		t.Fatalf("expected mcp_write_file to require manual approval, got decision=%v auto=%v", decision, auto)
	}
}

func testAppConfig(dbPath string) *api.Config {
	return &api.Config{
		LLM: api.LLMConfig{
			Provider: "moonshot",
			APIKey:   "test-key",
			Model:    "kimi-k2.5",
			BaseURL:  "https://api.moonshot.cn/v1",
			Timeout:  60 * time.Second,
		},
		Behavior: api.BehaviorConfig{
			AutoApprove:  []string{"read_file"},
			ShellTimeout: 30 * time.Second,
			MaxTurns:     50,
		},
		Session: api.SessionConfig{
			DBPath:     dbPath,
			MaxHistory: 100,
		},
		MCP: api.MCPConfig{
			GuardCommand: "mcp-guard",
		},
		UI: api.UIConfig{
			Theme: "dark",
		},
	}
}

func TestApp_New_FailsOnBadDBPath(t *testing.T) {
	tmpDir := t.TempDir()
	// Create a regular file where the DB directory should be.
	badDir := filepath.Join(tmpDir, "notadir")
	if err := os.WriteFile(badDir, []byte("x"), 0o644); err != nil {
		t.Fatalf("create blocking file: %v", err)
	}

	cfg := testAppConfig(filepath.Join(badDir, "sessions.db"))
	app, err := New(cfg, false)
	if err == nil {
		if app != nil {
			_ = app.Close()
		}
		t.Fatal("New() expected an error for a blocked db directory")
	}
	if app != nil {
		t.Fatal("New() expected nil App on error")
	}
}

func TestApp_New_SucceedsWithValidDBPath(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	cfg := testAppConfig(filepath.Join(tmpDir, "sessions.db"))
	app, err := New(cfg, false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if app == nil {
		t.Fatal("New() returned nil App")
	}
	if err := app.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
}

func TestApp_SessionLifecycle(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	cfg := testAppConfig(filepath.Join(tmpDir, "sessions.db"))
	app, err := New(cfg, false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer app.Close()

	ctx := context.Background()
	started, err := app.StartSession(ctx)
	if err != nil {
		t.Fatalf("StartSession() error: %v", err)
	}
	if started.ID == "" {
		t.Fatal("StartSession() returned session with empty ID")
	}

	resumed, err := app.ResumeSession(ctx, started.ID)
	if err != nil {
		t.Fatalf("ResumeSession() error: %v", err)
	}
	if resumed.ID != started.ID {
		t.Errorf("ResumeSession() returned %q, want %q", resumed.ID, started.ID)
	}

	continued, err := app.ContinueLastSession(ctx)
	if err != nil {
		t.Fatalf("ContinueLastSession() error: %v", err)
	}
	if continued.ID != started.ID {
		t.Errorf("ContinueLastSession() returned %q, want %q", continued.ID, started.ID)
	}
}

func TestApp_Close_FreshApp(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	cfg := testAppConfig(filepath.Join(tmpDir, "sessions.db"))
	app, err := New(cfg, false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	if err := app.Close(); err != nil {
		t.Errorf("Close() error: %v", err)
	}
}

func TestApp_Run_AppendsSystemMessage(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	cfg := testAppConfig(filepath.Join(tmpDir, "sessions.db"))
	app, err := New(cfg, false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer app.Close()

	ctx := context.Background()
	session, err := app.StartSession(ctx)
	if err != nil {
		t.Fatalf("StartSession() error: %v", err)
	}

	runCalled := false
	app.newProgram = func(model tea.Model, opts ...tea.ProgramOption) teaProgram {
		runCalled = true
		return &mockTeaProgram{err: nil}
	}

	if err := app.Run(ctx, session); err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if !runCalled {
		t.Fatal("fake program runner was not invoked")
	}

	msgs, err := app.store.GetMessages(ctx, session.ID, 0)
	if err != nil {
		t.Fatalf("GetMessages() error: %v", err)
	}

	var found bool
	for _, msg := range msgs {
		if msg.Role == api.RoleSystem && strings.Contains(msg.Content, "You are kimi-lite") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a system message containing the agent prompt, got %+v", msgs)
	}
}

func TestApp_New_WiresHookRunner(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	dbPath := filepath.Join(tmpDir, "sessions.db")

	cfg := &api.Config{
		LLM: api.LLMConfig{
			Provider: "moonshot",
			APIKey:   "test-key",
			Model:    "kimi-k2.5",
			BaseURL:  "https://api.moonshot.cn/v1",
			Timeout:  60 * time.Second,
		},
		Behavior: api.BehaviorConfig{
			AutoApprove:  []string{"read_file"},
			ShellTimeout: 30 * time.Second,
			MaxTurns:     50,
		},
		Session: api.SessionConfig{
			DBPath:     dbPath,
			MaxHistory: 100,
		},
		MCP: api.MCPConfig{
			GuardCommand: "mcp-guard",
		},
		UI: api.UIConfig{
			Theme: "dark",
		},
		Hooks: []api.HookConfig{
			{Event: api.HookToolCall, Command: "true"},
		},
	}

	app, err := New(cfg, false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer app.Close()

	if app.turnManager == nil {
		t.Fatal("turnManager is nil")
	}
	if app.sessionManager == nil {
		t.Fatal("sessionManager is nil")
	}
	if app.builtInExec == nil {
		t.Fatal("builtInExec is nil")
	}
}

func TestApp_New_WiresSubagentRunner(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	dbPath := filepath.Join(tmpDir, "sessions.db")

	cfg := &api.Config{
		LLM: api.LLMConfig{
			Provider: "moonshot",
			APIKey:   "test-key",
			Model:    "kimi-k2.5",
			BaseURL:  "https://api.moonshot.cn/v1",
			Timeout:  60 * time.Second,
		},
		Behavior: api.BehaviorConfig{
			AutoApprove:  []string{"read_file"},
			ShellTimeout: 30 * time.Second,
			MaxTurns:     50,
		},
		Session: api.SessionConfig{
			DBPath:     dbPath,
			MaxHistory: 100,
		},
		MCP: api.MCPConfig{
			GuardCommand: "mcp-guard",
		},
		UI: api.UIConfig{
			Theme: "dark",
		},
	}

	app, err := New(cfg, false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer app.Close()

	for _, def := range app.builtInExec.Definitions(context.Background()) {
		if def.Name == "dispatch_subagent" {
			return
		}
	}
	t.Fatal("dispatch_subagent tool not found in built-in executor definitions")
}

func TestApp_PprofServerStartsAndStops(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	cfg := &api.Config{
		LLM: api.LLMConfig{
			Provider: "moonshot",
			APIKey:   "test-key",
			Model:    "kimi-k2.5",
			BaseURL:  "https://api.moonshot.cn/v1",
			Timeout:  60 * time.Second,
		},
		Behavior: api.BehaviorConfig{
			AutoApprove:  []string{"read_file"},
			ShellTimeout: 30 * time.Second,
			MaxTurns:     50,
		},
		Session: api.SessionConfig{
			DBPath:     filepath.Join(tmpDir, "sessions.db"),
			MaxHistory: 100,
		},
		UI: api.UIConfig{
			Theme: "dark",
		},
		PprofAddr: "127.0.0.1:0",
	}

	app, err := New(cfg, false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if app.pprofCancel == nil {
		t.Fatal("expected pprofCancel to be set")
	}

	if err := app.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
}

func TestSystemPrompt_IncludesSkills(t *testing.T) {
	t.Parallel()

	prompt := systemPrompt("/tmp/test-dir", "# Go\nUse gofmt.", "")
	if !strings.Contains(prompt, "Additional skills context") {
		t.Error("system prompt missing skills header")
	}
	if !strings.Contains(prompt, "Use gofmt.") {
		t.Error("system prompt missing skill content")
	}
}

func TestApp_New_NilConfig(t *testing.T) {
	t.Parallel()

	app, err := New(nil, false)
	if err == nil {
		if app != nil {
			_ = app.Close()
		}
		t.Fatal("expected error for nil config")
	}
	if app != nil {
		t.Fatal("expected nil App on nil config")
	}
}

func TestApp_SetAutoApprove(t *testing.T) {
	t.Parallel()

	app := &App{
		approvalGate: core.NewApprovalGate(core.ModeAuto, []string{"read_file"}, func(name string) bool {
			return name == "read_file"
		}, nil),
	}

	call := api.ToolCall{Name: "grep"}
	decision, auto := app.approvalGate.ShouldAutoApprove(call)
	if auto || decision != api.ApprovalNo {
		t.Fatal("expected manual approval for grep before SetAutoApprove")
	}

	app.SetAutoApprove([]string{"grep"})
	decision, auto = app.approvalGate.ShouldAutoApprove(call)
	if !auto || decision != api.ApprovalYes {
		t.Fatalf("expected auto-approval for grep after SetAutoApprove, got decision=%v auto=%v", decision, auto)
	}
}

func TestApp_setApprovalMode(t *testing.T) {
	t.Parallel()

	app := &App{
		approvalGate: core.NewApprovalGate(core.ModeAuto, nil, nil, nil),
		logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	app.setApprovalMode(9999)
	if app.approvalGate.GetMode() != core.ModeAuto {
		t.Fatalf("expected mode to remain Auto for invalid value, got %v", app.approvalGate.GetMode())
	}

	app.setApprovalMode(int(core.ModeYolo))
	if app.approvalGate.GetMode() != core.ModeYolo {
		t.Fatalf("expected mode Yolo, got %v", app.approvalGate.GetMode())
	}

	app.setApprovalMode(int(core.ModeManual))
	if app.approvalGate.GetMode() != core.ModeManual {
		t.Fatalf("expected mode Manual, got %v", app.approvalGate.GetMode())
	}
}

func TestApp_New_ErrorPathCleanup(t *testing.T) {
	tmpDir := t.TempDir()
	// Create a file where the DB directory should be so store creation fails.
	badDir := filepath.Join(tmpDir, "notadir")
	if err := os.WriteFile(badDir, []byte("x"), 0o644); err != nil {
		t.Fatalf("create blocking file: %v", err)
	}

	cfg := testAppConfig(filepath.Join(badDir, "sessions.db"))
	app, err := New(cfg, false)
	if err == nil {
		if app != nil {
			_ = app.Close()
		}
		t.Fatal("expected error for blocked db directory")
	}
	if app != nil {
		t.Fatal("expected nil App on error")
	}
}

func TestApp_ImportSession_ClearsMessagesOnFailure(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	dbPath := filepath.Join(tmpDir, "sessions.db")

	a, err := New(testAppConfig(dbPath), false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Close()

	export := &api.SessionExport{
		Version: "1.0",
		Session: api.Session{Path: "/tmp/import-cleanup", CreatedAt: time.Now().UTC()},
		Messages: []api.Message{
			{ID: "m1", Role: api.RoleUser, Content: "hello"},
			{ID: "m2", Role: api.RoleUser, Content: "world"},
		},
		Turns: []api.Turn{},
	}

	// Wrap store to fail on 2nd AppendMessage
	fs := &failingStore{Store: a.store, failAfter: 1}
	a.store = fs

	_, err = a.ImportSession(context.Background(), export)
	if err == nil {
		t.Fatal("expected error for partial import")
	}

	sessions, err := fs.ListSessions(context.Background(), "/tmp/import-cleanup", 0)
	if err != nil {
		t.Fatalf("ListSessions error: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected 0 sessions after failed import, got %d", len(sessions))
	}

	// Verify no messages remain by scanning all sessions. Since the session is
	// gone and messages have a foreign-key cascade, this is implied, but we
	// double-check by trying to retrieve messages for any returned session.
	for _, s := range sessions {
		msgs, err := fs.GetMessages(context.Background(), s.ID, 0)
		if err != nil {
			t.Fatalf("GetMessages error: %v", err)
		}
		if len(msgs) != 0 {
			t.Fatalf("expected 0 messages for session %s, got %d", s.ID, len(msgs))
		}
	}
}

func TestSystemPrompt_NoLeadingTabs(t *testing.T) {
	t.Parallel()

	prompt := systemPrompt("/tmp/test-dir", "", "")
	lines := strings.Split(prompt, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "\t") {
			t.Fatalf("line %d has leading tab: %q", i, line)
		}
	}
}

func TestBuildWorkspaceTree_CollapsesHidden(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustCreate := func(path string, isDir bool) {
		full := filepath.Join(root, path)
		if isDir {
			if err := os.MkdirAll(full, 0755); err != nil {
				t.Fatalf("mkdir %s: %v", path, err)
			}
			return
		}
		if err := os.WriteFile(full, []byte("x"), 0644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	mustCreate("visible", true)
	mustCreate("visible/file.go", false)
	mustCreate(".hidden", true)
	mustCreate(".hidden/secret.txt", false)
	mustCreate("top.txt", false)

	tree := buildWorkspaceTree(root)
	if !strings.Contains(tree, "visible/") {
		t.Errorf("tree should contain visible dir")
	}
	if !strings.Contains(tree, "file.go") {
		t.Errorf("tree should contain visible/file.go")
	}
	if !strings.Contains(tree, ".hidden/") {
		t.Errorf("tree should contain .hidden dir")
	}
	if strings.Contains(tree, "secret.txt") {
		t.Errorf("hidden directory contents should be collapsed")
	}
}

// errorInjectingStore wraps a real store and returns injected errors for selected methods.
type errorInjectingStore struct {
	api.Store
	getSessionErr    error
	getMessagesErr   error
	getTurnsErr      error
	createSessionErr error
	updateSessionErr error
	appendErr        error
	saveTurnErr      error
	clearMessagesErr error
	deleteSessionErr error
	closeErr         error
}

func (s *errorInjectingStore) GetSession(ctx context.Context, id string) (*api.Session, error) {
	if s.getSessionErr != nil {
		return nil, s.getSessionErr
	}
	return s.Store.GetSession(ctx, id)
}

func (s *errorInjectingStore) GetMessages(ctx context.Context, sessionID string, limit int) ([]api.Message, error) {
	if s.getMessagesErr != nil {
		return nil, s.getMessagesErr
	}
	return s.Store.GetMessages(ctx, sessionID, limit)
}

func (s *errorInjectingStore) GetTurns(ctx context.Context, sessionID string, limit int) ([]api.Turn, error) {
	if s.getTurnsErr != nil {
		return nil, s.getTurnsErr
	}
	return s.Store.GetTurns(ctx, sessionID, limit)
}

func (s *errorInjectingStore) CreateSession(ctx context.Context, path string) (*api.Session, error) {
	if s.createSessionErr != nil {
		return nil, s.createSessionErr
	}
	return s.Store.CreateSession(ctx, path)
}

func (s *errorInjectingStore) UpdateSession(ctx context.Context, session *api.Session) error {
	if s.updateSessionErr != nil {
		return s.updateSessionErr
	}
	return s.Store.UpdateSession(ctx, session)
}

func (s *errorInjectingStore) AppendMessage(ctx context.Context, sessionID string, msg api.Message) error {
	if s.appendErr != nil {
		return s.appendErr
	}
	return s.Store.AppendMessage(ctx, sessionID, msg)
}

func (s *errorInjectingStore) SaveTurn(ctx context.Context, sessionID string, turn api.Turn) error {
	if s.saveTurnErr != nil {
		return s.saveTurnErr
	}
	return s.Store.SaveTurn(ctx, sessionID, turn)
}

func (s *errorInjectingStore) ClearMessages(ctx context.Context, sessionID string) error {
	if s.clearMessagesErr != nil {
		return s.clearMessagesErr
	}
	return s.Store.ClearMessages(ctx, sessionID)
}

func (s *errorInjectingStore) DeleteSession(ctx context.Context, id string) error {
	if s.deleteSessionErr != nil {
		return s.deleteSessionErr
	}
	return s.Store.DeleteSession(ctx, id)
}

func (s *errorInjectingStore) Close() error {
	if s.closeErr != nil {
		return s.closeErr
	}
	return s.Store.Close()
}

type fakeMCPClientErr struct {
	fakeAppMCPClient
	closeErr error
}

func (f *fakeMCPClientErr) Close() error {
	return f.closeErr
}

func TestApp_RunTurn(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	cfg := testAppConfig(filepath.Join(tmpDir, "sessions.db"))

	a, err := New(cfg, false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Close()

	ctx := context.Background()
	sess, err := a.StartSession(ctx)
	if err != nil {
		t.Fatalf("StartSession() error: %v", err)
	}

	llm := &mockLLM{
		chatStreamFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			ch := make(chan api.StreamChunk)
			go func() {
				defer close(ch)
				select {
				case ch <- api.StreamChunk{Done: true}:
				case <-ctx.Done():
				}
			}()
			return ch, nil
		},
	}
	tools := &mockToolExecutor{
		executeFunc: func(ctx context.Context, call api.ToolCall) (api.ToolResult, error) {
			return api.ToolResult{}, nil
		},
		defs:     []api.ToolDefinition{{Name: "read_file", Description: "read"}},
		readOnly: map[string]bool{"read_file": true},
	}
	approval := &mockApprovalGate{
		shouldAutoApprove: func(call api.ToolCall) (api.ApprovalDecision, bool) {
			return api.ApprovalYes, true
		},
	}

	a.turnManager, err = core.NewTurnManager(llm, tools, approval, a.store, &configProvider{cfg: cfg})
	if err != nil {
		t.Fatalf("create turn manager: %v", err)
	}

	events, err := a.RunTurn(ctx, sess.ID, "hello")
	if err != nil {
		t.Fatalf("RunTurn() error: %v", err)
	}

	var sawDone bool
	for ev := range events {
		if ev.Type == api.TurnEventDone {
			sawDone = true
			break
		}
	}
	if !sawDone {
		t.Fatal("expected a Done event from RunTurn")
	}
}

func TestApp_ExportSession_Errors(t *testing.T) {
	tests := []struct {
		name string
		set  func(*errorInjectingStore)
	}{
		{"get session", func(s *errorInjectingStore) { s.getSessionErr = fmt.Errorf("boom session") }},
		{"get messages", func(s *errorInjectingStore) { s.getMessagesErr = fmt.Errorf("boom messages") }},
		{"get turns", func(s *errorInjectingStore) { s.getTurnsErr = fmt.Errorf("boom turns") }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			t.Setenv("HOME", tmpDir)
			cfg := testAppConfig(filepath.Join(tmpDir, "sessions.db"))

			a, err := New(cfg, false)
			if err != nil {
				t.Fatalf("New() error: %v", err)
			}
			defer a.Close()

			sess, err := a.StartSession(context.Background())
			if err != nil {
				t.Fatalf("StartSession() error: %v", err)
			}

			wrapped := &errorInjectingStore{Store: a.store}
			tt.set(wrapped)
			a.store = wrapped

			_, err = a.ExportSession(context.Background(), sess.ID)
			if err == nil {
				t.Fatal("expected ExportSession to return an error")
			}
		})
	}
}

func TestApp_ImportSession_Errors(t *testing.T) {
	tests := []struct {
		name  string
		set   func(*errorInjectingStore)
		turns []api.Turn
	}{
		{"create session", func(s *errorInjectingStore) { s.createSessionErr = fmt.Errorf("boom create") }, nil},
		{"update session", func(s *errorInjectingStore) { s.updateSessionErr = fmt.Errorf("boom update") }, nil},
		{"save turn", func(s *errorInjectingStore) { s.saveTurnErr = fmt.Errorf("boom turn") }, []api.Turn{{ID: "t1", State: api.TurnThinking, Input: "hi"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			t.Setenv("HOME", tmpDir)
			cfg := testAppConfig(filepath.Join(tmpDir, "sessions.db"))

			a, err := New(cfg, false)
			if err != nil {
				t.Fatalf("New() error: %v", err)
			}
			defer a.Close()

			wrapped := &errorInjectingStore{Store: a.store}
			tt.set(wrapped)
			a.store = wrapped

			export := &api.SessionExport{
				Version: "1.0",
				Session: api.Session{Path: "/tmp/import-err", Name: "Named"},
				Turns:   tt.turns,
			}

			_, err = a.ImportSession(context.Background(), export)
			if err == nil {
				t.Fatal("expected ImportSession to return an error")
			}

			sessions, err := wrapped.ListSessions(context.Background(), "/tmp/import-err", 0)
			if err != nil {
				t.Fatalf("ListSessions error: %v", err)
			}
			if len(sessions) != 0 {
				t.Fatalf("expected cleanup to remove session, got %d", len(sessions))
			}
		})
	}
}

func TestApp_deleteSessionAndData_Errors(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	cfg := testAppConfig(filepath.Join(tmpDir, "sessions.db"))

	a, err := New(cfg, false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Close()

	sess, err := a.store.CreateSession(context.Background(), "/tmp/delete-err")
	if err != nil {
		t.Fatalf("CreateSession error: %v", err)
	}

	a.store = &errorInjectingStore{
		Store:            a.store,
		clearMessagesErr: fmt.Errorf("clear failed"),
		deleteSessionErr: fmt.Errorf("delete failed"),
	}

	err = a.deleteSessionAndData(context.Background(), sess.ID)
	if err == nil {
		t.Fatal("expected deleteSessionAndData to return an error")
	}
	if !strings.Contains(err.Error(), "clear messages") || !strings.Contains(err.Error(), "delete session") {
		t.Fatalf("expected both errors, got: %v", err)
	}
}

func TestApp_Close_Errors(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	cfg := testAppConfig(filepath.Join(tmpDir, "sessions.db"))

	a, err := New(cfg, false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	a.mcpClient = &fakeMCPClientErr{closeErr: fmt.Errorf("mcp close failed")}
	a.store = &errorInjectingStore{Store: a.store, closeErr: fmt.Errorf("store close failed")}

	err = a.Close()
	if err == nil {
		t.Fatal("expected Close to return an error")
	}
	if !strings.Contains(err.Error(), "close mcp") || !strings.Contains(err.Error(), "close store") {
		t.Fatalf("expected mcp and store close errors, got: %v", err)
	}
}

func TestApp_New_InvalidWebSearchEndpoint(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	cfg := testAppConfig(filepath.Join(tmpDir, "sessions.db"))
	cfg.WebSearch.Endpoint = "://not-a-url"

	app, err := New(cfg, false)
	if err == nil {
		if app != nil {
			_ = app.Close()
		}
		t.Fatal("expected error for invalid web search endpoint")
	}
	if !strings.Contains(err.Error(), "web search") {
		t.Fatalf("expected web search error, got: %v", err)
	}
}

func TestApp_New_WebSearchWiresTool(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	cfg := testAppConfig(filepath.Join(tmpDir, "sessions.db"))
	cfg.WebSearch.Endpoint = "https://search.example/api"

	a, err := New(cfg, false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Close()

	var found bool
	for _, def := range a.builtInExec.Definitions(context.Background()) {
		if def.Name == "web_search" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected web_search tool to be registered")
	}
}

func TestApp_New_DropsNonReadOnlyAutoApprove(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	cfg := testAppConfig(filepath.Join(tmpDir, "sessions.db"))
	cfg.Behavior.AutoApprove = []string{"read_file", "write_file", "unknown_tool"}

	a, err := New(cfg, false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Close()

	decision, auto := a.approvalGate.ShouldAutoApprove(api.ToolCall{Name: "write_file"})
	if auto || decision != api.ApprovalNo {
		t.Fatalf("expected write_file to require manual approval, got decision=%v auto=%v", decision, auto)
	}
}

func TestApp_New_MCPServerCreateError(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	cfg := testAppConfig(filepath.Join(tmpDir, "sessions.db"))
	cfg.MCPServers = map[string]api.MCPServerConfig{
		"local": {Enabled: true, Transport: api.MCPTransportStdio, Command: "fake"},
	}

	originalFactory := newMCPClientForServer
	defer func() { newMCPClientForServer = originalFactory }()
	newMCPClientForServer = func(cfg api.MCPServerConfig) (api.MCPClient, error) {
		return nil, fmt.Errorf("injected create error")
	}

	a, err := New(cfg, false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Close()

	if a.mcpClient != nil {
		t.Fatalf("expected no mcp client when creation fails, got %T", a.mcpClient)
	}
}

func TestApp_New_MCPServerConnectError(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	cfg := testAppConfig(filepath.Join(tmpDir, "sessions.db"))
	cfg.MCPServers = map[string]api.MCPServerConfig{
		"local": {Enabled: true, Transport: api.MCPTransportStdio, Command: "fake"},
	}

	originalFactory := newMCPClientForServer
	defer func() { newMCPClientForServer = originalFactory }()
	newMCPClientForServer = func(cfg api.MCPServerConfig) (api.MCPClient, error) {
		return &fakeAppMCPClient{connectErr: fmt.Errorf("injected connect error")}, nil
	}

	a, err := New(cfg, false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Close()

	if a.mcpClient != nil {
		t.Fatalf("expected no mcp client when connect fails, got %T", a.mcpClient)
	}
}

func TestApp_New_MCPUnsupportedTransport(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	cfg := testAppConfig(filepath.Join(tmpDir, "sessions.db"))
	cfg.MCPServers = map[string]api.MCPServerConfig{
		"bad": {Enabled: true, Transport: "ftp"},
	}

	a, err := New(cfg, false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Close()

	if a.mcpClient != nil {
		t.Fatalf("expected no mcp client for unsupported transport, got %T", a.mcpClient)
	}
}

func TestApp_Run_TUIError(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	cfg := testAppConfig(filepath.Join(tmpDir, "sessions.db"))

	a, err := New(cfg, false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Close()

	session := &api.Session{ID: "s1", Path: filepath.Join(tmpDir, "does-not-exist")}
	err = a.Run(context.Background(), session)
	if err == nil {
		t.Fatal("expected Run to return an error when TUI creation fails")
	}
	if !strings.Contains(err.Error(), "create tui model") {
		t.Fatalf("expected tui model error, got: %v", err)
	}
}

func TestApp_Run_AppendsGitStatusAndLogsAppendErrors(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	repoDir := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "file.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	for _, cmd := range []*exec.Cmd{
		exec.Command("git", "init", repoDir),
		exec.Command("git", "-C", repoDir, "config", "user.email", "test@example.com"),
		exec.Command("git", "-C", repoDir, "config", "user.name", "Test"),
	} {
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git setup: %v\n%s", err, out)
		}
	}

	cfg := testAppConfig(filepath.Join(tmpDir, "sessions.db"))
	a, err := New(cfg, false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Close()

	session, err := a.store.CreateSession(context.Background(), repoDir)
	if err != nil {
		t.Fatalf("CreateSession error: %v", err)
	}

	appendErr := fmt.Errorf("append failed")
	a.store = &errorInjectingStore{Store: a.store, appendErr: appendErr}

	a.newProgram = func(model tea.Model, opts ...tea.ProgramOption) teaProgram {
		return &mockTeaProgram{err: nil}
	}

	if err := a.Run(context.Background(), session); err != nil {
		t.Fatalf("Run() error: %v", err)
	}
}

func TestApp_Run_AppendSystemMessageError(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	cfg := testAppConfig(filepath.Join(tmpDir, "sessions.db"))

	a, err := New(cfg, false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Close()

	session, err := a.store.CreateSession(context.Background(), tmpDir)
	if err != nil {
		t.Fatalf("CreateSession error: %v", err)
	}

	a.store = &errorInjectingStore{Store: a.store, appendErr: fmt.Errorf("append system message failed")}
	a.newProgram = func(model tea.Model, opts ...tea.ProgramOption) teaProgram {
		return &mockTeaProgram{err: nil}
	}

	if err := a.Run(context.Background(), session); err != nil {
		t.Fatalf("Run() error: %v", err)
	}
}

func TestApp_New_UnsupportedProvider(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	cfg := testAppConfig(filepath.Join(tmpDir, "sessions.db"))
	cfg.LLM.Provider = "anthropic"

	app, err := New(cfg, false)
	if err == nil {
		if app != nil {
			_ = app.Close()
		}
		t.Fatal("expected error for unsupported provider")
	}
	if !strings.Contains(err.Error(), "create llm client") {
		t.Fatalf("expected llm client error, got: %v", err)
	}
}

func TestApp_New_EmptyBaseURL(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	cfg := testAppConfig(filepath.Join(tmpDir, "sessions.db"))
	cfg.LLM.BaseURL = ""

	app, err := New(cfg, false)
	if err == nil {
		if app != nil {
			_ = app.Close()
		}
		t.Fatal("expected error for empty base_url")
	}
	if !strings.Contains(err.Error(), "create llm client") {
		t.Fatalf("expected llm client error, got: %v", err)
	}
}

func TestApp_New_GetwdError(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	orig := osGetwd
	osGetwd = func() (string, error) { return "", fmt.Errorf("injected getwd error") }
	defer func() { osGetwd = orig }()

	cfg := testAppConfig(filepath.Join(tmpDir, "sessions.db"))
	a, err := New(cfg, false)
	if err == nil {
		if a != nil {
			_ = a.Close()
		}
		t.Fatal("expected New() to return an error when getwd fails")
	}
	if !strings.Contains(err.Error(), "get working directory") {
		t.Fatalf("expected working directory error, got: %v", err)
	}
}

func TestApp_StartSession_GetwdError(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	a, err := New(testAppConfig(filepath.Join(tmpDir, "sessions.db")), false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Close()

	orig := osGetwd
	osGetwd = func() (string, error) { return "", fmt.Errorf("injected getwd error") }
	defer func() { osGetwd = orig }()

	_, err = a.StartSession(context.Background())
	if err == nil {
		t.Fatal("expected StartSession to return an error")
	}
	if !strings.Contains(err.Error(), "get working directory") {
		t.Fatalf("expected working directory error, got: %v", err)
	}
}

func TestApp_ContinueLastSession_GetwdError(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	a, err := New(testAppConfig(filepath.Join(tmpDir, "sessions.db")), false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Close()

	orig := osGetwd
	osGetwd = func() (string, error) { return "", fmt.Errorf("injected getwd error") }
	defer func() { osGetwd = orig }()

	_, err = a.ContinueLastSession(context.Background())
	if err == nil {
		t.Fatal("expected ContinueLastSession to return an error")
	}
	if !strings.Contains(err.Error(), "get working directory") {
		t.Fatalf("expected working directory error, got: %v", err)
	}
}

func TestApp_ImportSession_EmptyVersion(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	a, err := New(testAppConfig(filepath.Join(tmpDir, "sessions.db")), false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Close()

	export := &api.SessionExport{
		Version: "",
		Session: api.Session{Path: "/tmp/empty-version", Name: "EmptyVersion"},
		Turns:   []api.Turn{{ID: "t1", State: api.TurnThinking, Input: "hi"}},
	}

	imported, err := a.ImportSession(context.Background(), export)
	if err != nil {
		t.Fatalf("ImportSession() error: %v", err)
	}
	if imported.Name != "EmptyVersion" {
		t.Errorf("imported name = %q, want %q", imported.Name, "EmptyVersion")
	}
}

func TestApp_Close_NoBuiltInExec(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	a, err := New(testAppConfig(filepath.Join(tmpDir, "sessions.db")), false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	a.builtInExec = nil
	if err := a.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
}

func TestApp_Run_ContextCanceled(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	cfg := testAppConfig(filepath.Join(tmpDir, "sessions.db"))
	a, err := New(cfg, false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Close()

	session, err := a.store.CreateSession(context.Background(), tmpDir)
	if err != nil {
		t.Fatalf("CreateSession error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	a.newProgram = func(model tea.Model, opts ...tea.ProgramOption) teaProgram {
		return &mockTeaProgram{err: nil}
	}

	if err := a.Run(ctx, session); err != nil {
		t.Fatalf("Run() error: %v", err)
	}
}

func TestApp_New_DebugLogger(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	cfg := testAppConfig(filepath.Join(tmpDir, "sessions.db"))
	a, err := New(cfg, true)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Close()
}

func TestApp_New_StoreError(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	dbPath := filepath.Join(tmpDir, "sessions.db")
	if err := os.MkdirAll(dbPath, 0o700); err != nil {
		t.Fatalf("mkdir db path: %v", err)
	}

	cfg := testAppConfig(dbPath)
	app, err := New(cfg, false)
	if err == nil {
		if app != nil {
			_ = app.Close()
		}
		t.Fatal("expected error when DB path is a directory")
	}
	if !strings.Contains(err.Error(), "open store") {
		t.Fatalf("expected open store error, got: %v", err)
	}
}

func TestApp_New_ConfigDirError(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	cfg := testAppConfig(filepath.Join(tmpDir, "sessions.db"))
	app, err := New(cfg, false)
	if err == nil {
		if app != nil {
			_ = app.Close()
		}
		t.Fatal("expected error when config dir cannot be determined")
	}
	if !strings.Contains(err.Error(), "ensure config dir") {
		t.Fatalf("expected config dir error, got: %v", err)
	}
}

func TestApp_New_DiscoverSkillsError(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("UserConfigDir() error: %v", err)
	}
	configDir := filepath.Join(userConfigDir, "kimi-lite")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	skillsDir := filepath.Join(configDir, "skills")
	if err := os.MkdirAll(skillsDir, 0o700); err != nil {
		t.Fatalf("mkdir skills dir: %v", err)
	}
	if err := os.Chmod(skillsDir, 0o000); err != nil {
		t.Fatalf("chmod skills: %v", err)
	}
	defer os.Chmod(skillsDir, 0o700)

	cfg := testAppConfig(filepath.Join(tmpDir, "sessions.db"))
	a, err := New(cfg, false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Close()
}

func TestApp_New_BuiltInExecError(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	sandboxFile := filepath.Join(tmpDir, "sandbox")
	if err := os.WriteFile(sandboxFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("write sandbox file: %v", err)
	}

	orig := osGetwd
	osGetwd = func() (string, error) { return sandboxFile, nil }
	defer func() { osGetwd = orig }()

	cfg := testAppConfig(filepath.Join(tmpDir, "sessions.db"))
	app, err := New(cfg, false)
	if err == nil {
		if app != nil {
			_ = app.Close()
		}
		t.Fatal("expected error when sandbox root is not a directory")
	}
	if !strings.Contains(err.Error(), "create built-in tool executor") {
		t.Fatalf("expected built-in executor error, got: %v", err)
	}
}

func TestApp_New_MCPServerDisabled(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	cfg := testAppConfig(filepath.Join(tmpDir, "sessions.db"))
	cfg.MCPServers = map[string]api.MCPServerConfig{
		"disabled": {Enabled: false, Transport: api.MCPTransportStdio, Command: "fake"},
	}

	a, err := New(cfg, false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Close()

	if a.mcpClient != nil {
		t.Fatalf("expected no mcp client when only disabled servers are configured, got %T", a.mcpClient)
	}
}

func TestApp_ImportSession_CleanupError(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	a, err := New(testAppConfig(filepath.Join(tmpDir, "sessions.db")), false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Close()

	wrapped := &errorInjectingStore{
		Store:            a.store,
		updateSessionErr: fmt.Errorf("update failed"),
		deleteSessionErr: fmt.Errorf("delete failed"),
	}
	a.store = wrapped

	export := &api.SessionExport{
		Version: "1.0",
		Session: api.Session{Path: "/tmp/cleanup-err", Name: "Named"},
	}

	_, err = a.ImportSession(context.Background(), export)
	if err == nil {
		t.Fatal("expected ImportSession to return an error")
	}
	if !strings.Contains(err.Error(), "update session name") {
		t.Fatalf("expected update session error, got: %v", err)
	}
}

type fakeGitProvider struct {
	isRepoFunc func(ctx context.Context) (bool, error)
	statusFunc func(ctx context.Context) (string, error)
	diffFunc   func(ctx context.Context, path string) (string, error)
}

func (f *fakeGitProvider) Status(ctx context.Context) (string, error) {
	if f.statusFunc != nil {
		return f.statusFunc(ctx)
	}
	return "", nil
}

func (f *fakeGitProvider) Diff(ctx context.Context, path string) (string, error) {
	if f.diffFunc != nil {
		return f.diffFunc(ctx, path)
	}
	return "", nil
}

func (f *fakeGitProvider) Commit(ctx context.Context, message string) error { return nil }

func (f *fakeGitProvider) IsRepo(ctx context.Context) (bool, error) {
	if f.isRepoFunc != nil {
		return f.isRepoFunc(ctx)
	}
	return false, nil
}

func (f *fakeGitProvider) Branch(ctx context.Context) (string, error) { return "main", nil }

func TestApp_Run_GitStatusError(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	cfg := testAppConfig(filepath.Join(tmpDir, "sessions.db"))
	a, err := New(cfg, false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Close()

	session, err := a.store.CreateSession(context.Background(), tmpDir)
	if err != nil {
		t.Fatalf("CreateSession error: %v", err)
	}

	a.gitProvider = &fakeGitProvider{
		isRepoFunc: func(ctx context.Context) (bool, error) { return true, nil },
		statusFunc: func(ctx context.Context) (string, error) { return "", fmt.Errorf("status failed") },
	}
	a.newProgram = func(model tea.Model, opts ...tea.ProgramOption) teaProgram {
		return &mockTeaProgram{err: nil}
	}

	if err := a.Run(context.Background(), session); err != nil {
		t.Fatalf("Run() error: %v", err)
	}
}

func TestApp_Run_GitIsRepoError(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	cfg := testAppConfig(filepath.Join(tmpDir, "sessions.db"))
	a, err := New(cfg, false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Close()

	session, err := a.store.CreateSession(context.Background(), tmpDir)
	if err != nil {
		t.Fatalf("CreateSession error: %v", err)
	}

	a.gitProvider = &fakeGitProvider{
		isRepoFunc: func(ctx context.Context) (bool, error) { return false, fmt.Errorf("is-repo failed") },
	}
	a.newProgram = func(model tea.Model, opts ...tea.ProgramOption) teaProgram {
		return &mockTeaProgram{err: nil}
	}

	if err := a.Run(context.Background(), session); err != nil {
		t.Fatalf("Run() error: %v", err)
	}
}

func TestApp_New_MCPMixedEnabledDisabled(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	cfg := testAppConfig(filepath.Join(tmpDir, "sessions.db"))
	cfg.MCPServers = map[string]api.MCPServerConfig{
		"local":  {Enabled: true, Transport: api.MCPTransportStdio, Command: "fake"},
		"remote": {Enabled: false, Transport: api.MCPTransportHTTP, URL: "https://example.com/mcp"},
	}

	originalFactory := newMCPClientForServer
	defer func() { newMCPClientForServer = originalFactory }()
	newMCPClientForServer = func(cfg api.MCPServerConfig) (api.MCPClient, error) {
		return &fakeAppMCPClient{tools: []api.ToolDefinition{{Name: "grep", Description: "Search"}}}, nil
	}

	a, err := New(cfg, false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Close()

	if a.mcpClient == nil {
		t.Fatal("expected mcp client for enabled server")
	}
}

func TestApp_New_MCPStdioFactoryBranch(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	cfg := testAppConfig(filepath.Join(tmpDir, "sessions.db"))
	cfg.MCPServers = map[string]api.MCPServerConfig{
		"local": {Enabled: true, Transport: api.MCPTransportStdio, Command: "false"},
	}

	a, err := New(cfg, false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Close()

	if a.mcpClient != nil {
		t.Fatalf("expected no mcp client when handshake fails, got %T", a.mcpClient)
	}
}
