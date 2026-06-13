package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ekhodzitsky/kimi-lite/internal/store"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

type mockApp struct {
	setYoloCalled      bool
	resumeSessionID    string
	continueLastCalled bool
	startSessionCalled bool
	runSession         *api.Session
	runCalled          bool
	runTurnSessionID   string
	runTurnInput       string
	runTurnCalled      bool
	runTurnReturn      <-chan api.TurnEvent
	runTurnErr         error

	resumeSessionReturn *api.Session
	continueLastReturn  *api.Session
	startSessionReturn  *api.Session
	resumeSessionErr    error
	continueLastErr     error
	startSessionErr     error
	runErr              error

	exportReturn *api.SessionExport
	closeErr     error
}

func (m *mockApp) SetYolo(v bool) { m.setYoloCalled = v }
func (m *mockApp) Close() error   { return m.closeErr }
func (m *mockApp) ResumeSession(_ context.Context, id string) (*api.Session, error) {
	m.resumeSessionID = id
	return m.resumeSessionReturn, m.resumeSessionErr
}
func (m *mockApp) ContinueLastSession(_ context.Context) (*api.Session, error) {
	m.continueLastCalled = true
	return m.continueLastReturn, m.continueLastErr
}
func (m *mockApp) StartSession(_ context.Context) (*api.Session, error) {
	m.startSessionCalled = true
	return m.startSessionReturn, m.startSessionErr
}
func (m *mockApp) Run(_ context.Context, session *api.Session) error {
	m.runCalled = true
	m.runSession = session
	return m.runErr
}
func (m *mockApp) RunTurn(_ context.Context, sessionID string, input string) (<-chan api.TurnEvent, error) {
	m.runTurnCalled = true
	m.runTurnSessionID = sessionID
	m.runTurnInput = input
	if m.runTurnReturn == nil {
		ch := make(chan api.TurnEvent)
		close(ch)
		return ch, m.runTurnErr
	}
	return m.runTurnReturn, m.runTurnErr
}
func (m *mockApp) ExportSession(_ context.Context, _ string) (*api.SessionExport, error) {
	return m.exportReturn, nil
}
func (m *mockApp) ImportSession(_ context.Context, _ *api.SessionExport) (*api.Session, error) {
	return nil, nil
}

func TestVersionFlag(t *testing.T) {
	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--version"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "kimi-lite version") {
		t.Errorf("expected version output, got: %s", out)
	}
}

func TestMissingAPIKey(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	writeDefaultConfig = func() error { return nil }
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) {
		return &mockApp{}, nil
	}

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	content := `[llm]
provider = "moonshot"
api_key = ""
model = "kimi-k2.5"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(tmpDir)
	t.Setenv("HOME", tmpDir)

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--config", configPath})
	err := cmd.Execute()

	if err == nil {
		t.Fatal("expected error for missing API key")
	}
	if !strings.Contains(err.Error(), "API key is not configured") {
		t.Errorf("expected API key error, got: %v", err)
	}
}

func TestContinueFlag(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	mock := &mockApp{
		continueLastReturn: &api.Session{ID: "sess-1", Path: "/tmp"},
	}
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return mock, nil }
	writeDefaultConfig = func() error { return nil }

	tmpDir := t.TempDir()
	configContent := `[llm]
provider = "moonshot"
api_key = "test-key"
model = "kimi-k2.5"
`
	configPath := filepath.Join(tmpDir, "config.toml")
	os.WriteFile(configPath, []byte(configContent), 0644)
	t.Chdir(tmpDir)
	t.Setenv("HOME", tmpDir)

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--continue", "--config", configPath})
	err := cmd.Execute()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !mock.continueLastCalled {
		t.Error("expected ContinueLastSession to be called")
	}
	if !mock.runCalled {
		t.Error("expected Run to be called")
	}
}

func TestContinueFlag_NoPreviousSession(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	mock := &mockApp{
		continueLastErr:    store.ErrSessionNotFound,
		startSessionReturn: &api.Session{ID: "sess-new", Path: "/tmp"},
	}
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return mock, nil }
	writeDefaultConfig = func() error { return nil }

	tmpDir := t.TempDir()
	configContent := `[llm]
provider = "moonshot"
api_key = "test-key"
model = "kimi-k2.5"
`
	configPath := filepath.Join(tmpDir, "config.toml")
	os.WriteFile(configPath, []byte(configContent), 0644)
	t.Chdir(tmpDir)
	t.Setenv("HOME", tmpDir)

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--continue", "--config", configPath})
	err := cmd.Execute()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !mock.continueLastCalled {
		t.Error("expected ContinueLastSession to be called")
	}
	if !mock.startSessionCalled {
		t.Error("expected StartSession to be called when no previous session")
	}
	if !mock.runCalled {
		t.Error("expected Run to be called")
	}
}

func TestContinueFlag_NonNotFoundError(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	mock := &mockApp{
		continueLastErr: errors.New("database locked"),
	}
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return mock, nil }
	writeDefaultConfig = func() error { return nil }

	tmpDir := t.TempDir()
	configContent := `[llm]
provider = "moonshot"
api_key = "test-key"
model = "kimi-k2.5"
`
	configPath := filepath.Join(tmpDir, "config.toml")
	os.WriteFile(configPath, []byte(configContent), 0644)
	t.Chdir(tmpDir)
	t.Setenv("HOME", tmpDir)

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--continue", "--config", configPath})
	err := cmd.Execute()

	if err == nil {
		t.Fatal("expected error for non-not-found continue error")
	}
	if mock.startSessionCalled {
		t.Error("expected StartSession NOT to be called for non-not-found error")
	}
}

func TestSessionFlag(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	mock := &mockApp{
		resumeSessionReturn: &api.Session{ID: "sess-123", Path: "/tmp"},
	}
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return mock, nil }
	writeDefaultConfig = func() error { return nil }

	tmpDir := t.TempDir()
	configContent := `[llm]
provider = "moonshot"
api_key = "test-key"
model = "kimi-k2.5"
`
	configPath := filepath.Join(tmpDir, "config.toml")
	os.WriteFile(configPath, []byte(configContent), 0644)
	t.Chdir(tmpDir)
	t.Setenv("HOME", tmpDir)

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--session", "sess-123", "--config", configPath})
	err := cmd.Execute()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.resumeSessionID != "sess-123" {
		t.Errorf("expected session ID sess-123, got %s", mock.resumeSessionID)
	}
	if !mock.runCalled {
		t.Error("expected Run to be called")
	}
}

func TestYoloFlag(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	mock := &mockApp{
		startSessionReturn: &api.Session{ID: "sess-new", Path: "/tmp"},
	}
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return mock, nil }
	writeDefaultConfig = func() error { return nil }

	tmpDir := t.TempDir()
	configContent := `[llm]
provider = "moonshot"
api_key = "test-key"
model = "kimi-k2.5"
`
	configPath := filepath.Join(tmpDir, "config.toml")
	os.WriteFile(configPath, []byte(configContent), 0644)
	t.Chdir(tmpDir)
	t.Setenv("HOME", tmpDir)

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--yolo", "--config", configPath})
	err := cmd.Execute()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !mock.setYoloCalled {
		t.Error("expected SetYolo to be called")
	}
	if !mock.runCalled {
		t.Error("expected Run to be called")
	}
}

func TestMissingAPIKey_Unresolved(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	writeDefaultConfig = func() error { return nil }
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) {
		return &mockApp{}, nil
	}

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	content := `[llm]
provider = "moonshot"
api_key = "$UNRESOLVED_API_KEY"
model = "kimi-k2.5"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(tmpDir)

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--config", configPath})
	err := cmd.Execute()

	if err == nil {
		t.Fatal("expected error for unresolved API key")
	}
	if !strings.Contains(err.Error(), "API key is not configured") {
		t.Errorf("expected API key error, got: %v", err)
	}
}

func TestDegradedDefault_UnresolvedAPIKey(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	writeDefaultConfig = func() error { return nil }
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) {
		return &mockApp{}, nil
	}

	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	os.Unsetenv("MOONSHOT_API_KEY")

	// Create an invalid config file to force Load() to fail
	configPath := filepath.Join(tmpDir, "config.toml")
	os.WriteFile(configPath, []byte("invalid toml {"), 0644)

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--config", configPath})
	err := cmd.Execute()

	if err == nil {
		t.Fatal("expected error for unresolved API key in degraded default")
	}
	if !strings.Contains(err.Error(), "API key is not configured") {
		t.Errorf("expected API key error, got: %v", err)
	}
}

func TestRunExport_SurfacesCloseError(t *testing.T) {
	origNewApp := newApp
	defer func() { newApp = origNewApp }()

	mock := &mockApp{
		exportReturn: &api.SessionExport{Version: "1.0"},
		closeErr:     errors.New("store flush failed"),
	}
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return mock, nil }

	tmpDir := t.TempDir()
	configContent := `[llm]
provider = "moonshot"
api_key = "test-key"
model = "kimi-k2.5"
`
	configPath := filepath.Join(tmpDir, "config.toml")
	os.WriteFile(configPath, []byte(configContent), 0644)
	t.Chdir(tmpDir)
	t.Setenv("HOME", tmpDir)

	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	err := runExport(context.Background(), "sess-1", filepath.Join(tmpDir, "out.json"), "")

	w.Close()
	os.Stderr = oldStderr
	var buf bytes.Buffer
	io.Copy(&buf, r)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "shutdown: store flush failed") {
		t.Errorf("expected stderr to contain close error, got: %q", buf.String())
	}
}

func TestDoctor_UsesCustomConfigPath(t *testing.T) {
	tmpDir := t.TempDir()
	customDBDir := filepath.Join(tmpDir, "custom-db")
	customDBPath := filepath.Join(customDBDir, "sessions.db")

	configContent := `[llm]
provider = "moonshot"
api_key = "dummy-key"
model = "kimi-k2.5"
base_url = "http://localhost:1"
[session]
db_path = "` + customDBPath + `"
`
	configPath := filepath.Join(tmpDir, "config.toml")
	os.WriteFile(configPath, []byte(configContent), 0644)
	t.Setenv("HOME", tmpDir)

	// runDoctor may fail on LLM check, but the DB directory should be created.
	_ = runDoctor(context.Background(), configPath)

	if _, err := os.Stat(customDBDir); os.IsNotExist(err) {
		t.Fatalf("expected DB directory %s to be created using custom config", customDBDir)
	}
}

func TestYoloAndAutoFlag(t *testing.T) {
	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--yolo", "--auto"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for unknown --auto flag")
	}
	if !strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("expected unknown flag error, got: %v", err)
	}
}

func TestWriteDefaultConfig_Warning(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	writeDefaultConfig = func() error { return errors.New("permission denied") }
	mock := &mockApp{
		startSessionReturn: &api.Session{ID: "sess-new", Path: "/tmp"},
	}
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return mock, nil }

	tmpDir := t.TempDir()
	configContent := `[llm]
provider = "moonshot"
api_key = "test-key"
model = "kimi-k2.5"
`
	configPath := filepath.Join(tmpDir, "config.toml")
	os.WriteFile(configPath, []byte(configContent), 0644)
	t.Chdir(tmpDir)
	t.Setenv("HOME", tmpDir)

	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--config", configPath})
	err := cmd.Execute()

	w.Close()
	os.Stderr = oldStderr
	var buf bytes.Buffer
	io.Copy(&buf, r)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "Warning: could not write default config") {
		t.Errorf("expected stderr warning, got: %q", buf.String())
	}
	if !mock.startSessionCalled {
		t.Error("expected StartSession to be called despite writeDefaultConfig error")
	}
}

func TestPromptFlag_TextOutput(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	ch := make(chan api.TurnEvent, 4)
	ch <- api.TurnEvent{Type: api.TurnEventContent, Content: "Hello, "}
	ch <- api.TurnEvent{Type: api.TurnEventContent, Content: "world!"}
	ch <- api.TurnEvent{Type: api.TurnEventDone}
	close(ch)

	mock := &mockApp{
		startSessionReturn: &api.Session{ID: "sess-prompt", Path: "/tmp"},
		runTurnReturn:      ch,
	}
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return mock, nil }
	writeDefaultConfig = func() error { return nil }

	tmpDir := t.TempDir()
	configContent := `[llm]
provider = "moonshot"
api_key = "test-key"
model = "kimi-k2.5"
`
	configPath := filepath.Join(tmpDir, "config.toml")
	os.WriteFile(configPath, []byte(configContent), 0644)
	t.Chdir(tmpDir)
	t.Setenv("HOME", tmpDir)

	cmd := newRootCmd()
	var buf bytes.Buffer
	oldStdout := stdout
	stdout = &buf
	defer func() { stdout = oldStdout }()
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--prompt", "say hello", "--config", configPath})
	err := cmd.Execute()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !mock.startSessionCalled {
		t.Error("expected StartSession to be called")
	}
	if !mock.setYoloCalled {
		t.Error("expected yolo to be enabled for prompt mode")
	}
	if mock.runTurnInput != "say hello" {
		t.Errorf("expected input %q, got %q", "say hello", mock.runTurnInput)
	}
	if mock.runCalled {
		t.Error("expected TUI Run NOT to be called")
	}
	if buf.String() != "Hello, world!\n" {
		t.Errorf("expected stdout %q, got %q", "Hello, world!\n", buf.String())
	}
}

func TestPromptFlag_JSONOutput(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	ch := make(chan api.TurnEvent, 4)
	ch <- api.TurnEvent{Type: api.TurnEventContent, Content: "hi"}
	ch <- api.TurnEvent{Type: api.TurnEventToolResult, Result: api.ToolResult{CallID: "call-1", Name: "read_file", Output: "data"}}
	ch <- api.TurnEvent{Type: api.TurnEventDone}
	close(ch)

	mock := &mockApp{
		startSessionReturn: &api.Session{ID: "sess-json", Path: "/tmp"},
		runTurnReturn:      ch,
	}
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return mock, nil }
	writeDefaultConfig = func() error { return nil }

	tmpDir := t.TempDir()
	configContent := `[llm]
provider = "moonshot"
api_key = "test-key"
model = "kimi-k2.5"
`
	configPath := filepath.Join(tmpDir, "config.toml")
	os.WriteFile(configPath, []byte(configContent), 0644)
	t.Chdir(tmpDir)
	t.Setenv("HOME", tmpDir)

	cmd := newRootCmd()
	var buf bytes.Buffer
	oldStdout := stdout
	stdout = &buf
	defer func() { stdout = oldStdout }()
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--prompt", "go", "--output-format", "json", "--config", configPath})
	err := cmd.Execute()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 JSON lines, got %d: %q", len(lines), buf.String())
	}
	for _, line := range lines {
		var ev promptEventJSON
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("invalid JSON line %q: %v", line, err)
		}
	}
}

func TestPromptFlag_ApprovalRequestErrors(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	ch := make(chan api.TurnEvent, 2)
	ch <- api.TurnEvent{Type: api.TurnEventApprovalRequest, ToolCalls: []api.ToolCall{{ID: "call-1", Name: "write_file"}}}
	close(ch)

	mock := &mockApp{
		startSessionReturn: &api.Session{ID: "sess-approval", Path: "/tmp"},
		runTurnReturn:      ch,
	}
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return mock, nil }
	writeDefaultConfig = func() error { return nil }

	tmpDir := t.TempDir()
	configContent := `[llm]
provider = "moonshot"
api_key = "test-key"
model = "kimi-k2.5"
`
	configPath := filepath.Join(tmpDir, "config.toml")
	os.WriteFile(configPath, []byte(configContent), 0644)
	t.Chdir(tmpDir)
	t.Setenv("HOME", tmpDir)

	cmd := newRootCmd()
	var buf bytes.Buffer
	oldStdout := stdout
	stdout = &buf
	defer func() { stdout = oldStdout }()
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--prompt", "edit file", "--config", configPath})
	err := cmd.Execute()

	if err == nil {
		t.Fatal("expected error for approval request in prompt mode")
	}
	if !strings.Contains(err.Error(), "requires approval") {
		t.Errorf("expected approval error, got: %v", err)
	}
}

func TestPromptFlag_InvalidOutputFormat(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	mock := &mockApp{
		startSessionReturn: &api.Session{ID: "sess-fmt", Path: "/tmp"},
	}
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return mock, nil }
	writeDefaultConfig = func() error { return nil }

	tmpDir := t.TempDir()
	configContent := `[llm]
provider = "moonshot"
api_key = "test-key"
model = "kimi-k2.5"
`
	configPath := filepath.Join(tmpDir, "config.toml")
	os.WriteFile(configPath, []byte(configContent), 0644)
	t.Chdir(tmpDir)
	t.Setenv("HOME", tmpDir)

	cmd := newRootCmd()
	var buf bytes.Buffer
	oldStdout := stdout
	stdout = &buf
	defer func() { stdout = oldStdout }()
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--prompt", "go", "--output-format", "xml", "--config", configPath})
	err := cmd.Execute()

	if err == nil {
		t.Fatal("expected error for invalid output format")
	}
	if !strings.Contains(err.Error(), "unsupported output format") {
		t.Errorf("expected unsupported format error, got: %v", err)
	}
}
