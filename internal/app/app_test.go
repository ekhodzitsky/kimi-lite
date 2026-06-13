package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ekhodzitsky/kimi-lite/internal/core"
	"github.com/ekhodzitsky/kimi-lite/internal/git"
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

	prompt := systemPrompt("/tmp/test-dir")

	requiredTools := []string{"read_file", "glob", "grep", "list_directory", "write_file", "str_replace_file", "shell", "fetch_url"}
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
}

func (m *mockToolExecutor) Execute(ctx context.Context, call api.ToolCall) (api.ToolResult, error) {
	return m.executeFunc(ctx, call)
}
func (m *mockToolExecutor) Definitions(ctx context.Context) []api.ToolDefinition { return m.defs }

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
	tm := core.NewTurnManager(llm, tools, approval, a.store, &configProvider{cfg: cfg})
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
