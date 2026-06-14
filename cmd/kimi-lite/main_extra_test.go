package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ekhodzitsky/kimi-lite/internal/store"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// failingWriter fails writes after a configured number of bytes.
type failingWriter struct {
	failAfter int
	written   int
	err       error
}

func (w *failingWriter) Write(p []byte) (int, error) {
	if w.failAfter >= 0 && w.written >= w.failAfter {
		return 0, w.err
	}
	n := len(p)
	w.written += n
	return n, nil
}

// mockLLMClient is a test double for api.LLMClient used by runDoctor.
type mockLLMClient struct {
	chatReturn *api.Message
	chatErr    error
}

func (m *mockLLMClient) Chat(_ context.Context, _ []api.Message, _ []api.ToolDefinition) (*api.Message, error) {
	return m.chatReturn, m.chatErr
}
func (m *mockLLMClient) ChatStream(_ context.Context, _ []api.Message, _ []api.ToolDefinition) (<-chan api.StreamChunk, error) {
	return nil, nil
}
func (m *mockLLMClient) Models() []api.ModelInfo { return nil }

// mockMCPClient is a test double for api.MCPClient used by runDoctor.
type mockMCPClient struct {
	connectErr error
	closeErr   error
}

func (m *mockMCPClient) Connect(_ context.Context) error { return m.connectErr }
func (m *mockMCPClient) ListTools(_ context.Context) ([]api.ToolDefinition, error) {
	return nil, nil
}
func (m *mockMCPClient) CallTool(_ context.Context, _ string, _ map[string]any) (string, error) {
	return "", nil
}
func (m *mockMCPClient) Close() error { return m.closeErr }

// captureStdout replaces os.Stdout with a pipe while fn runs and returns the
// captured output.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("create pipe: %v", err)
	}
	os.Stdout = w

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	fn()

	_ = w.Close()
	os.Stdout = old
	return <-done
}

func makeTestConfig(t *testing.T, extra ...string) (tmpDir, configPath string) {
	t.Helper()
	tmpDir = t.TempDir()
	base := `[llm]
provider = "moonshot"
api_key = "test-key"
model = "kimi-k2.5"
`
	configPath = filepath.Join(tmpDir, "config.toml")
	content := base + strings.Join(extra, "\n")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(tmpDir)
	t.Setenv("HOME", tmpDir)
	return tmpDir, configPath
}

func TestMain_Success(t *testing.T) {
	origArgs := os.Args
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		os.Args = origArgs
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	_, configPath := makeTestConfig(t)
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) {
		return &mockApp{startSessionReturn: &api.Session{ID: "sess-main", Path: "/tmp"}}, nil
	}
	writeDefaultConfig = func() error { return nil }

	os.Args = []string{binaryName, "--config", configPath}
	main()
}

func TestMain_ErrorExits(t *testing.T) {
	origArgs := os.Args
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	origOsExit := osExit
	defer func() {
		os.Args = origArgs
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
		osExit = origOsExit
	}()

	_, configPath := makeTestConfig(t)
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) {
		return nil, errors.New("app failure")
	}
	writeDefaultConfig = func() error { return nil }

	var exitCode int
	osExit = func(code int) { exitCode = code }

	os.Args = []string{binaryName, "--config", configPath}
	main()

	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
}

func TestLoadConfig_DefaultFallback(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, ".config"))

	// No config file exists, so Load() falls back to defaults.
	cfg, err := loadConfig("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected config")
	}
}

func TestRun_ModelAlias(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	tmpDir := t.TempDir()
	configContent := `[llm]
provider = "moonshot"
api_key = "test-key"
model = "alias-name"
[models.alias-name]
model = "resolved-model"
`
	configPath := filepath.Join(tmpDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(tmpDir)
	t.Setenv("HOME", tmpDir)

	var capturedCfg *api.Config
	mock := &mockApp{startSessionReturn: &api.Session{ID: "sess-alias", Path: "/tmp"}}
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) {
		capturedCfg = cfg
		return mock, nil
	}
	writeDefaultConfig = func() error { return nil }

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--model", "alias-name", "--config", configPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedCfg == nil || capturedCfg.LLM.Model != "resolved-model" {
		t.Fatalf("expected model alias to resolve, got %v", capturedCfg)
	}
}

func TestRun_ResolveProviderError(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	tmpDir := t.TempDir()
	// Empty base_url triggers resolve provider error.
	configContent := `[llm]
provider = "moonshot"
api_key = "test-key"
model = "kimi-k2.5"
base_url = ""
`
	configPath := filepath.Join(tmpDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(tmpDir)
	t.Setenv("HOME", tmpDir)

	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return &mockApp{}, nil }
	writeDefaultConfig = func() error { return nil }

	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--config", configPath})
	err := cmd.Execute()

	if err == nil {
		t.Fatal("expected error for missing base_url")
	}
	if !strings.Contains(err.Error(), "resolve provider") {
		t.Errorf("expected resolve provider error, got: %v", err)
	}
}

func TestRun_NewAppError(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	_, configPath := makeTestConfig(t)
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) {
		return nil, errors.New("constructor failure")
	}
	writeDefaultConfig = func() error { return nil }

	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--config", configPath})
	err := cmd.Execute()

	if err == nil {
		t.Fatal("expected error for newApp failure")
	}
	if !strings.Contains(err.Error(), "initialize app") {
		t.Errorf("expected initialize app error, got: %v", err)
	}
}

func TestRun_StartSessionError(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	_, configPath := makeTestConfig(t)
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) {
		return &mockApp{startSessionErr: errors.New("session failure")}, nil
	}
	writeDefaultConfig = func() error { return nil }

	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--config", configPath})
	err := cmd.Execute()

	if err == nil {
		t.Fatal("expected error for start session failure")
	}
	if !strings.Contains(err.Error(), "start session") {
		t.Errorf("expected start session error, got: %v", err)
	}
}

func TestRun_ApplicationRunError(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	_, configPath := makeTestConfig(t)
	mock := &mockApp{
		startSessionReturn: &api.Session{ID: "sess-run", Path: "/tmp"},
		runErr:             errors.New("run failure"),
		closeErr:           errors.New("close failure"),
	}
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return mock, nil }
	writeDefaultConfig = func() error { return nil }

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

	if err == nil {
		t.Fatal("expected error for application.Run failure")
	}
	if !strings.Contains(err.Error(), "run failure") {
		t.Errorf("expected run failure error, got: %v", err)
	}
	if !strings.Contains(buf.String(), "shutdown: close failure") {
		t.Errorf("expected close error on stderr, got: %q", buf.String())
	}
}

func TestRun_ResumeSessionError(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	_, configPath := makeTestConfig(t)
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) {
		return &mockApp{resumeSessionErr: errors.New("resume failure")}, nil
	}
	writeDefaultConfig = func() error { return nil }

	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--session", "sess-missing", "--config", configPath})
	err := cmd.Execute()

	if err == nil {
		t.Fatal("expected error for resume session failure")
	}
	if !strings.Contains(err.Error(), "resume session") {
		t.Errorf("expected resume session error, got: %v", err)
	}
}

func TestRunPrompt_RunTurnError(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	_, configPath := makeTestConfig(t)
	mock := &mockApp{
		startSessionReturn: &api.Session{ID: "sess-rt", Path: "/tmp"},
		runTurnErr:         errors.New("turn failure"),
	}
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return mock, nil }
	writeDefaultConfig = func() error { return nil }

	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--prompt", "go", "--config", configPath})
	err := cmd.Execute()

	if err == nil {
		t.Fatal("expected error for RunTurn failure")
	}
	if !strings.Contains(err.Error(), "run turn") {
		t.Errorf("expected run turn error, got: %v", err)
	}
}

func TestRunPrompt_TurnEventError_Text(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	origStdout := stdout
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
		stdout = origStdout
	}()

	ch := make(chan api.TurnEvent, 1)
	ch <- api.TurnEvent{Type: api.TurnEventError, Error: errors.New("model error")}
	close(ch)

	mock := &mockApp{
		startSessionReturn: &api.Session{ID: "sess-terr", Path: "/tmp"},
		runTurnReturn:      ch,
	}
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return mock, nil }
	writeDefaultConfig = func() error { return nil }

	_, configPath := makeTestConfig(t)
	cmd := newRootCmd()
	var buf bytes.Buffer
	stdout = &buf
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--prompt", "go", "--config", configPath})
	err := cmd.Execute()

	if err == nil {
		t.Fatal("expected error for turn event error")
	}
	if !strings.Contains(err.Error(), "turn error") {
		t.Errorf("expected turn error, got: %v", err)
	}
}

func TestRunPrompt_TurnEventError_JSON(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	origStdout := stdout
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
		stdout = origStdout
	}()

	ch := make(chan api.TurnEvent, 1)
	ch <- api.TurnEvent{Type: api.TurnEventError, Error: errors.New("model error")}
	close(ch)

	mock := &mockApp{
		startSessionReturn: &api.Session{ID: "sess-terr-json", Path: "/tmp"},
		runTurnReturn:      ch,
	}
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return mock, nil }
	writeDefaultConfig = func() error { return nil }

	_, configPath := makeTestConfig(t)
	cmd := newRootCmd()
	var buf bytes.Buffer
	stdout = &buf
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--prompt", "go", "--output-format", "json", "--config", configPath})
	err := cmd.Execute()

	if err == nil {
		t.Fatal("expected error for turn event error")
	}
}

func TestRunPrompt_NilErrorContinues(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	origStdout := stdout
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
		stdout = origStdout
	}()

	ch := make(chan api.TurnEvent, 2)
	ch <- api.TurnEvent{Type: api.TurnEventError, Error: nil}
	ch <- api.TurnEvent{Type: api.TurnEventDone}
	close(ch)

	mock := &mockApp{
		startSessionReturn: &api.Session{ID: "sess-nil-err", Path: "/tmp"},
		runTurnReturn:      ch,
	}
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return mock, nil }
	writeDefaultConfig = func() error { return nil }

	_, configPath := makeTestConfig(t)
	cmd := newRootCmd()
	var buf bytes.Buffer
	stdout = &buf
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--prompt", "go", "--config", configPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunPrompt_WriteContentError(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	origStdout := stdout
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
		stdout = origStdout
	}()

	ch := make(chan api.TurnEvent, 1)
	ch <- api.TurnEvent{Type: api.TurnEventContent, Content: "hello"}
	close(ch)

	mock := &mockApp{
		startSessionReturn: &api.Session{ID: "sess-wcerr", Path: "/tmp"},
		runTurnReturn:      ch,
	}
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return mock, nil }
	writeDefaultConfig = func() error { return nil }

	_, configPath := makeTestConfig(t)
	stdout = &failingWriter{failAfter: 0, err: errors.New("write failed")}
	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--prompt", "go", "--config", configPath})
	err := cmd.Execute()

	if err == nil {
		t.Fatal("expected error for write content failure")
	}
	if !strings.Contains(err.Error(), "write content") {
		t.Errorf("expected write content error, got: %v", err)
	}
}

func TestRunPrompt_WriteNewlineError(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	origStdout := stdout
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
		stdout = origStdout
	}()

	ch := make(chan api.TurnEvent, 1)
	ch <- api.TurnEvent{Type: api.TurnEventDone}
	close(ch)

	mock := &mockApp{
		startSessionReturn: &api.Session{ID: "sess-nlerr", Path: "/tmp"},
		runTurnReturn:      ch,
	}
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return mock, nil }
	writeDefaultConfig = func() error { return nil }

	_, configPath := makeTestConfig(t)
	stdout = &failingWriter{failAfter: 0, err: errors.New("write failed")}
	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--prompt", "go", "--config", configPath})
	err := cmd.Execute()

	if err == nil {
		t.Fatal("expected error for write newline failure")
	}
	if !strings.Contains(err.Error(), "write newline") {
		t.Errorf("expected write newline error, got: %v", err)
	}
}

func TestRunPrompt_EncodeError(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	origStdout := stdout
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
		stdout = origStdout
	}()

	ch := make(chan api.TurnEvent, 1)
	ch <- api.TurnEvent{Type: api.TurnEventContent, Content: "hello"}
	close(ch)

	mock := &mockApp{
		startSessionReturn: &api.Session{ID: "sess-encerr", Path: "/tmp"},
		runTurnReturn:      ch,
	}
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return mock, nil }
	writeDefaultConfig = func() error { return nil }

	_, configPath := makeTestConfig(t)
	stdout = &failingWriter{failAfter: 0, err: errors.New("encode failed")}
	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--prompt", "go", "--output-format", "json", "--config", configPath})
	err := cmd.Execute()

	if err == nil {
		t.Fatal("expected error for encode failure")
	}
	if !strings.Contains(err.Error(), "encode content event") {
		t.Errorf("expected encode content event error, got: %v", err)
	}
}

func TestRunExport_Success(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	tmpDir, configPath := makeTestConfig(t)
	outPath := filepath.Join(tmpDir, "export.json")
	mock := &mockApp{
		exportReturn: &api.SessionExport{Version: "1.0", Session: api.Session{ID: "sess-exp"}},
	}
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return mock, nil }
	writeDefaultConfig = func() error { return nil }

	out := captureStdout(t, func() {
		if err := runExport(context.Background(), "sess-1", outPath, flags{configPath: configPath}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if !strings.Contains(out, "Exported session sess-1") {
		t.Errorf("expected export message, got: %q", out)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	if !strings.Contains(string(data), `"version": "1.0"`) {
		t.Errorf("expected export JSON, got: %s", data)
	}
}

func TestRunExport_ExportSessionError(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	tmpDir, configPath := makeTestConfig(t)
	mock := &mockApp{exportErr: errors.New("export failure")}
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return mock, nil }
	writeDefaultConfig = func() error { return nil }

	err := runExport(context.Background(), "sess-1", filepath.Join(tmpDir, "out.json"), flags{configPath: configPath})
	if err == nil {
		t.Fatal("expected error for export session failure")
	}
	if !strings.Contains(err.Error(), "export session") {
		t.Errorf("expected export session error, got: %v", err)
	}
}

func TestRunExport_WriteFileError(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	_, configPath := makeTestConfig(t)
	mock := &mockApp{exportReturn: &api.SessionExport{Version: "1.0"}}
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return mock, nil }
	writeDefaultConfig = func() error { return nil }

	// Use a directory path as output file to force a write error.
	err := runExport(context.Background(), "sess-1", t.TempDir(), flags{configPath: configPath})
	if err == nil {
		t.Fatal("expected error for write file failure")
	}
	if !strings.Contains(err.Error(), "write export file") {
		t.Errorf("expected write export file error, got: %v", err)
	}
}

func TestRunImport_Success(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	tmpDir, configPath := makeTestConfig(t)
	inPath := filepath.Join(tmpDir, "import.json")
	export := api.SessionExport{Version: "1.0", Session: api.Session{ID: "imported-id", Path: "/tmp"}}
	data, err := json.Marshal(export)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	mock := &mockApp{importReturn: &api.Session{ID: "imported-id", Path: "/tmp"}}
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return mock, nil }
	writeDefaultConfig = func() error { return nil }

	out := captureStdout(t, func() {
		if err := runImport(context.Background(), inPath, flags{configPath: configPath}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if !strings.Contains(out, "Imported session as imported-id") {
		t.Errorf("expected import message, got: %q", out)
	}
}

func TestRunImport_ReadFileError(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	_, configPath := makeTestConfig(t)
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return &mockApp{}, nil }
	writeDefaultConfig = func() error { return nil }

	err := runImport(context.Background(), "/nonexistent/path.json", flags{configPath: configPath})
	if err == nil {
		t.Fatal("expected error for missing import file")
	}
	if !strings.Contains(err.Error(), "read import file") {
		t.Errorf("expected read import file error, got: %v", err)
	}
}

func TestRunImport_UnmarshalError(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	tmpDir, configPath := makeTestConfig(t)
	inPath := filepath.Join(tmpDir, "import.json")
	if err := os.WriteFile(inPath, []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return &mockApp{}, nil }
	writeDefaultConfig = func() error { return nil }

	err := runImport(context.Background(), inPath, flags{configPath: configPath})
	if err == nil {
		t.Fatal("expected error for invalid import JSON")
	}
	if !strings.Contains(err.Error(), "parse import file") {
		t.Errorf("expected parse import file error, got: %v", err)
	}
}

func TestRunImport_ImportSessionError(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	tmpDir, configPath := makeTestConfig(t)
	inPath := filepath.Join(tmpDir, "import.json")
	export := api.SessionExport{Version: "1.0"}
	data, err := json.Marshal(export)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	mock := &mockApp{importErr: errors.New("import failure")}
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return mock, nil }
	writeDefaultConfig = func() error { return nil }

	err = runImport(context.Background(), inPath, flags{configPath: configPath})
	if err == nil {
		t.Fatal("expected error for import session failure")
	}
	if !strings.Contains(err.Error(), "import session") {
		t.Errorf("expected import session error, got: %v", err)
	}
}

func TestRunACP_Success(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	origStdin := os.Stdin
	origStdout := os.Stdout
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
		os.Stdin = origStdin
		os.Stdout = origStdout
	}()

	_, configPath := makeTestConfig(t)
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) {
		return &mockApp{}, nil
	}
	writeDefaultConfig = func() error { return nil }

	// Closed stdin pipe causes the ACP server to see EOF immediately.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	os.Stdin = r

	// Capture stdout to avoid polluting test output.
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = outW
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, outR)
		done <- buf.String()
	}()

	err = runACP(context.Background(), flags{configPath: configPath})
	outW.Close()
	<-done

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunACP_BadConfig(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	if err := os.WriteFile(configPath, []byte("invalid toml {"), 0644); err != nil {
		t.Fatal(err)
	}
	writeDefaultConfig = func() error { return nil }

	err := runACP(context.Background(), flags{configPath: configPath})
	if err == nil {
		t.Fatal("expected error for bad config")
	}
	if !strings.Contains(err.Error(), "load config") {
		t.Errorf("expected load config error, got: %v", err)
	}
}

func TestRunACP_MissingAPIKey(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	tmpDir := t.TempDir()
	configContent := `[llm]
provider = "moonshot"
api_key = ""
model = "kimi-k2.5"
`
	configPath := filepath.Join(tmpDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}
	writeDefaultConfig = func() error { return nil }

	err := runACP(context.Background(), flags{configPath: configPath})
	if err == nil {
		t.Fatal("expected error for missing API key")
	}
	if !strings.Contains(err.Error(), "API key is not configured") {
		t.Errorf("expected API key error, got: %v", err)
	}
}

func TestRunACP_NewAppError(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	_, configPath := makeTestConfig(t)
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) {
		return nil, errors.New("app failure")
	}
	writeDefaultConfig = func() error { return nil }

	err := runACP(context.Background(), flags{configPath: configPath})
	if err == nil {
		t.Fatal("expected error for newApp failure")
	}
	if !strings.Contains(err.Error(), "initialize app") {
		t.Errorf("expected initialize app error, got: %v", err)
	}
}

func TestRunDoctor_AllChecksPass(t *testing.T) {
	origWriteDefaultConfig := writeDefaultConfig
	origNewStore := newStore
	origNewLLMClient := newLLMClient
	origNewMCPClient := newMCPClient
	defer func() {
		writeDefaultConfig = origWriteDefaultConfig
		newStore = origNewStore
		newLLMClient = origNewLLMClient
		newMCPClient = origNewMCPClient
	}()

	_, configPath := makeTestConfig(t)
	writeDefaultConfig = func() error { return nil }
	newStore = func(dbPath string) (api.Store, error) {
		return &closingStore{}, nil
	}
	newLLMClient = func(cfg *api.Config, httpClient *http.Client) (api.LLMClient, error) {
		return &mockLLMClient{chatReturn: &api.Message{Role: api.RoleAssistant, Content: "pong"}}, nil
	}
	newMCPClient = func(cfg api.MCPConfig) api.MCPClient {
		return &mockMCPClient{}
	}

	out := captureStdout(t, func() {
		if err := runDoctor(context.Background(), configPath); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if !strings.Contains(out, "All checks passed") {
		t.Errorf("expected all checks passed, got: %q", out)
	}
}

func TestRunDoctor_ConfigLoadFailsExplicitPath(t *testing.T) {
	origWriteDefaultConfig := writeDefaultConfig
	defer func() { writeDefaultConfig = origWriteDefaultConfig }()

	writeDefaultConfig = func() error { return nil }
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	if err := os.WriteFile(configPath, []byte("invalid toml {"), 0644); err != nil {
		t.Fatal(err)
	}

	err := runDoctor(context.Background(), configPath)
	if err == nil {
		t.Fatal("expected error for explicit bad config")
	}
	if !strings.Contains(err.Error(), "load config") {
		t.Errorf("expected load config error, got: %v", err)
	}
}

func TestRunDoctor_DBDirCreateFails(t *testing.T) {
	origWriteDefaultConfig := writeDefaultConfig
	defer func() { writeDefaultConfig = origWriteDefaultConfig }()

	tmpDir := t.TempDir()
	// Create a file where the DB directory should be to make MkdirAll fail.
	blockingFile := filepath.Join(tmpDir, "blocked")
	if err := os.WriteFile(blockingFile, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	configContent := `[llm]
provider = "moonshot"
api_key = "test-key"
model = "kimi-k2.5"
[session]
db_path = "` + filepath.Join(blockingFile, "sessions.db") + `"
`
	configPath := filepath.Join(tmpDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}
	writeDefaultConfig = func() error { return nil }

	out := captureStdout(t, func() {
		if err := runDoctor(context.Background(), configPath); err == nil {
			t.Fatal("expected error for DB dir failure")
		}
	})

	if !strings.Contains(out, "[FAIL] DB directory") {
		t.Errorf("expected DB directory failure, got: %q", out)
	}
}

func TestRunDoctor_DBOpenFails(t *testing.T) {
	origWriteDefaultConfig := writeDefaultConfig
	defer func() { writeDefaultConfig = origWriteDefaultConfig }()

	tmpDir := t.TempDir()
	// Use a directory as the DB path to force NewSQLite to fail.
	dbPath := filepath.Join(tmpDir, "dbdir")
	if err := os.MkdirAll(dbPath, 0700); err != nil {
		t.Fatal(err)
	}

	configContent := `[llm]
provider = "moonshot"
api_key = "test-key"
model = "kimi-k2.5"
[session]
db_path = "` + dbPath + `"
`
	configPath := filepath.Join(tmpDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}
	writeDefaultConfig = func() error { return nil }

	out := captureStdout(t, func() {
		if err := runDoctor(context.Background(), configPath); err == nil {
			t.Fatal("expected error for DB open failure")
		}
	})

	if !strings.Contains(out, "[FAIL] DB open") {
		t.Errorf("expected DB open failure, got: %q", out)
	}
}

func TestRunDoctor_LLMProviderFail(t *testing.T) {
	origWriteDefaultConfig := writeDefaultConfig
	defer func() { writeDefaultConfig = origWriteDefaultConfig }()

	tmpDir := t.TempDir()
	configContent := `[llm]
provider = "moonshot"
api_key = "test-key"
model = "kimi-k2.5"
base_url = ""
`
	configPath := filepath.Join(tmpDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}
	writeDefaultConfig = func() error { return nil }

	out := captureStdout(t, func() {
		if err := runDoctor(context.Background(), configPath); err == nil {
			t.Fatal("expected error for LLM provider failure")
		}
	})

	if !strings.Contains(out, "[FAIL] LLM provider") {
		t.Errorf("expected LLM provider failure, got: %q", out)
	}
}

func TestRunDoctor_LLMAPIKeyMissing(t *testing.T) {
	origWriteDefaultConfig := writeDefaultConfig
	defer func() { writeDefaultConfig = origWriteDefaultConfig }()

	tmpDir := t.TempDir()
	configContent := `[llm]
provider = "moonshot"
api_key = ""
model = "kimi-k2.5"
base_url = "https://example.com"
`
	configPath := filepath.Join(tmpDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}
	writeDefaultConfig = func() error { return nil }

	out := captureStdout(t, func() {
		if err := runDoctor(context.Background(), configPath); err == nil {
			t.Fatal("expected error for missing API key")
		}
	})

	if !strings.Contains(out, "[FAIL] LLM API key") {
		t.Errorf("expected LLM API key failure, got: %q", out)
	}
}

func TestRunDoctor_LLMClientCreateFail(t *testing.T) {
	origWriteDefaultConfig := writeDefaultConfig
	origNewLLMClient := newLLMClient
	defer func() {
		writeDefaultConfig = origWriteDefaultConfig
		newLLMClient = origNewLLMClient
	}()

	_, configPath := makeTestConfig(t)
	writeDefaultConfig = func() error { return nil }
	newLLMClient = func(cfg *api.Config, httpClient *http.Client) (api.LLMClient, error) {
		return nil, errors.New("client create failure")
	}

	out := captureStdout(t, func() {
		if err := runDoctor(context.Background(), configPath); err == nil {
			t.Fatal("expected error for LLM client create failure")
		}
	})

	if !strings.Contains(out, "[FAIL] LLM client") {
		t.Errorf("expected LLM client failure, got: %q", out)
	}
}

func TestRunDoctor_LLMConnectivitySuccess(t *testing.T) {
	origWriteDefaultConfig := writeDefaultConfig
	origNewStore := newStore
	origNewLLMClient := newLLMClient
	origNewMCPClient := newMCPClient
	defer func() {
		writeDefaultConfig = origWriteDefaultConfig
		newStore = origNewStore
		newLLMClient = origNewLLMClient
		newMCPClient = origNewMCPClient
	}()

	_, configPath := makeTestConfig(t)
	writeDefaultConfig = func() error { return nil }
	newStore = func(dbPath string) (api.Store, error) { return &closingStore{}, nil }
	newLLMClient = func(cfg *api.Config, httpClient *http.Client) (api.LLMClient, error) {
		return &mockLLMClient{chatReturn: &api.Message{Role: api.RoleAssistant, Content: "pong"}}, nil
	}
	newMCPClient = func(cfg api.MCPConfig) api.MCPClient { return &mockMCPClient{} }

	out := captureStdout(t, func() {
		if err := runDoctor(context.Background(), configPath); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if !strings.Contains(out, "[OK]   LLM connectivity") {
		t.Errorf("expected LLM connectivity OK, got: %q", out)
	}
}

func TestRunDoctor_LLMConnectivityAuthFail(t *testing.T) {
	origWriteDefaultConfig := writeDefaultConfig
	origNewLLMClient := newLLMClient
	defer func() {
		writeDefaultConfig = origWriteDefaultConfig
		newLLMClient = origNewLLMClient
	}()

	_, configPath := makeTestConfig(t)
	writeDefaultConfig = func() error { return nil }
	newLLMClient = func(cfg *api.Config, httpClient *http.Client) (api.LLMClient, error) {
		return &mockLLMClient{chatErr: &api.APIError{StatusCode: 401, Body: "unauthorized"}}, nil
	}

	out := captureStdout(t, func() {
		if err := runDoctor(context.Background(), configPath); err == nil {
			t.Fatal("expected error for LLM auth failure")
		}
	})

	if !strings.Contains(out, "[FAIL] LLM auth") {
		t.Errorf("expected LLM auth failure, got: %q", out)
	}
}

func TestRunDoctor_LLMConnectivityRequestFail(t *testing.T) {
	origWriteDefaultConfig := writeDefaultConfig
	origNewLLMClient := newLLMClient
	defer func() {
		writeDefaultConfig = origWriteDefaultConfig
		newLLMClient = origNewLLMClient
	}()

	_, configPath := makeTestConfig(t)
	writeDefaultConfig = func() error { return nil }
	newLLMClient = func(cfg *api.Config, httpClient *http.Client) (api.LLMClient, error) {
		return &mockLLMClient{chatErr: &api.APIError{StatusCode: 400, Body: "bad request"}}, nil
	}

	out := captureStdout(t, func() {
		if err := runDoctor(context.Background(), configPath); err == nil {
			t.Fatal("expected error for LLM request failure")
		}
	})

	if !strings.Contains(out, "[FAIL] LLM request") {
		t.Errorf("expected LLM request failure, got: %q", out)
	}
}

func TestRunDoctor_LLMConnectivityWarning(t *testing.T) {
	origWriteDefaultConfig := writeDefaultConfig
	origNewLLMClient := newLLMClient
	defer func() {
		writeDefaultConfig = origWriteDefaultConfig
		newLLMClient = origNewLLMClient
	}()

	_, configPath := makeTestConfig(t)
	writeDefaultConfig = func() error { return nil }
	newLLMClient = func(cfg *api.Config, httpClient *http.Client) (api.LLMClient, error) {
		return &mockLLMClient{chatErr: errors.New("network timeout")}, nil
	}

	out := captureStdout(t, func() {
		if err := runDoctor(context.Background(), configPath); err == nil {
			t.Fatal("expected error for LLM connectivity warning")
		}
	})

	if !strings.Contains(out, "[WARN] LLM connectivity") {
		t.Errorf("expected LLM connectivity warning, got: %q", out)
	}
}

func TestRunDoctor_MCPServersSuccess(t *testing.T) {
	origWriteDefaultConfig := writeDefaultConfig
	origNewStore := newStore
	origNewLLMClient := newLLMClient
	origNewMCPClientFromServerConfig := newMCPClientFromServerConfig
	origNewMCPMultiClient := newMCPMultiClient
	defer func() {
		writeDefaultConfig = origWriteDefaultConfig
		newStore = origNewStore
		newLLMClient = origNewLLMClient
		newMCPClientFromServerConfig = origNewMCPClientFromServerConfig
		newMCPMultiClient = origNewMCPMultiClient
	}()

	_, configPath := makeTestConfig(t,
		"[mcp_servers.test]",
		"enabled = true",
		`transport = "http"`,
		`url = "https://example.com"`)
	writeDefaultConfig = func() error { return nil }
	newStore = func(dbPath string) (api.Store, error) { return &closingStore{}, nil }
	newLLMClient = func(cfg *api.Config, httpClient *http.Client) (api.LLMClient, error) {
		return &mockLLMClient{chatReturn: &api.Message{Role: api.RoleAssistant, Content: "pong"}}, nil
	}
	newMCPClientFromServerConfig = func(cfg api.MCPServerConfig, httpClient *http.Client) (api.MCPClient, error) {
		return &mockMCPClient{}, nil
	}
	newMCPMultiClient = func(clients map[string]api.MCPClient, configs map[string]api.MCPServerConfig) api.MCPClient {
		return &mockMCPClient{}
	}

	out := captureStdout(t, func() {
		if err := runDoctor(context.Background(), configPath); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if !strings.Contains(out, "[OK]   MCP connected") {
		t.Errorf("expected MCP connected OK, got: %q", out)
	}
}

func TestRunDoctor_MCPServersDisabledAndInvalid(t *testing.T) {
	origWriteDefaultConfig := writeDefaultConfig
	origNewMCPClientFromServerConfig := newMCPClientFromServerConfig
	origNewMCPMultiClient := newMCPMultiClient
	defer func() {
		writeDefaultConfig = origWriteDefaultConfig
		newMCPClientFromServerConfig = origNewMCPClientFromServerConfig
		newMCPMultiClient = origNewMCPMultiClient
	}()

	tmpDir := t.TempDir()
	configContent := `[llm]
provider = "moonshot"
api_key = "test-key"
model = "kimi-k2.5"
base_url = "https://example.com"
[mcp_servers.disabled]
enabled = false
[mcp_servers.invalid]
enabled = true
transport = "http"
url = "https://example.com"
`
	configPath := filepath.Join(tmpDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}
	writeDefaultConfig = func() error { return nil }
	newMCPClientFromServerConfig = func(cfg api.MCPServerConfig, httpClient *http.Client) (api.MCPClient, error) {
		if cfg.Enabled {
			return nil, errors.New("invalid config")
		}
		return nil, errors.New("should not be called")
	}
	newMCPMultiClient = func(clients map[string]api.MCPClient, configs map[string]api.MCPServerConfig) api.MCPClient {
		return &mockMCPClient{connectErr: errors.New("no clients")}
	}

	out := captureStdout(t, func() {
		if err := runDoctor(context.Background(), configPath); err == nil {
			t.Fatal("expected error for MCP failure")
		}
	})

	if !strings.Contains(out, "[WARN] MCP server invalid: invalid config") {
		t.Errorf("expected invalid MCP server warning, got: %q", out)
	}
}

func TestRunDoctor_LegacyMCPConnectFail(t *testing.T) {
	origWriteDefaultConfig := writeDefaultConfig
	origNewMCPClient := newMCPClient
	defer func() {
		writeDefaultConfig = origWriteDefaultConfig
		newMCPClient = origNewMCPClient
	}()

	tmpDir := t.TempDir()
	configContent := `[llm]
provider = "moonshot"
api_key = "test-key"
model = "kimi-k2.5"
base_url = "https://example.com"
[mcp]
transport = "stdio"
command = "false"
`
	configPath := filepath.Join(tmpDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}
	writeDefaultConfig = func() error { return nil }
	newMCPClient = func(cfg api.MCPConfig) api.MCPClient {
		return &mockMCPClient{connectErr: errors.New("mcp not available")}
	}

	out := captureStdout(t, func() {
		if err := runDoctor(context.Background(), configPath); err == nil {
			t.Fatal("expected error for MCP connect failure")
		}
	})

	if !strings.Contains(out, "[WARN] MCP: mcp not available") {
		t.Errorf("expected MCP warning, got: %q", out)
	}
}

// closingStore is a minimal api.Store usable by runDoctor; only Close is called.
type closingStore struct{}

func (c *closingStore) Close() error { return nil }
func (c *closingStore) CreateSession(_ context.Context, _ string) (*api.Session, error) {
	return nil, nil
}
func (c *closingStore) GetSession(_ context.Context, _ string) (*api.Session, error) {
	return nil, nil
}
func (c *closingStore) GetLastSession(_ context.Context, _ string) (*api.Session, error) {
	return nil, nil
}
func (c *closingStore) ListSessions(_ context.Context, _ string, _ int) ([]api.Session, error) {
	return nil, nil
}
func (c *closingStore) UpdateSession(_ context.Context, _ *api.Session) error { return nil }
func (c *closingStore) DeleteSession(_ context.Context, _ string) error       { return nil }
func (c *closingStore) AppendMessage(_ context.Context, _ string, _ api.Message) error {
	return nil
}
func (c *closingStore) GetMessages(_ context.Context, _ string, _ int) ([]api.Message, error) {
	return nil, nil
}
func (c *closingStore) ClearMessages(_ context.Context, _ string) error { return nil }
func (c *closingStore) ReplaceMessages(_ context.Context, _ string, _ []api.Message) error {
	return nil
}
func (c *closingStore) SaveTurn(_ context.Context, _ string, _ api.Turn) error { return nil }
func (c *closingStore) GetTurns(_ context.Context, _ string, _ int) ([]api.Turn, error) {
	return nil, nil
}
func (c *closingStore) CountTurns(_ context.Context, _ string, _ api.TurnState) (int, error) {
	return 0, nil
}

func TestACPCmd_ExecutesClosure(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	origStdin := os.Stdin
	origStdout := os.Stdout
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
		os.Stdin = origStdin
		os.Stdout = origStdout
	}()

	_, configPath := makeTestConfig(t)
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return &mockApp{}, nil }
	writeDefaultConfig = func() error { return nil }

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	os.Stdin = r

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = outW
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, outR)
		done <- buf.String()
	}()

	cmd := newRootCmd()
	cmd.SetArgs([]string{"acp", "--config", configPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	outW.Close()
	<-done
}

func TestDoctorCmd_ExecutesClosure(t *testing.T) {
	origWriteDefaultConfig := writeDefaultConfig
	origNewStore := newStore
	origNewLLMClient := newLLMClient
	origNewMCPClient := newMCPClient
	defer func() {
		writeDefaultConfig = origWriteDefaultConfig
		newStore = origNewStore
		newLLMClient = origNewLLMClient
		newMCPClient = origNewMCPClient
	}()

	_, configPath := makeTestConfig(t)
	writeDefaultConfig = func() error { return nil }
	newStore = func(dbPath string) (api.Store, error) { return &closingStore{}, nil }
	newLLMClient = func(cfg *api.Config, httpClient *http.Client) (api.LLMClient, error) {
		return &mockLLMClient{chatReturn: &api.Message{Role: api.RoleAssistant, Content: "pong"}}, nil
	}
	newMCPClient = func(cfg api.MCPConfig) api.MCPClient { return &mockMCPClient{} }

	cmd := newRootCmd()
	cmd.SetArgs([]string{"doctor", "--config", configPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExportCmd_ExecutesClosure(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	tmpDir, configPath := makeTestConfig(t)
	outPath := filepath.Join(tmpDir, "export.json")
	mock := &mockApp{exportReturn: &api.SessionExport{Version: "1.0"}}
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return mock, nil }
	writeDefaultConfig = func() error { return nil }

	cmd := newRootCmd()
	cmd.SetArgs([]string{"export", "--session", "sess-1", "--output", outPath, "--config", configPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("expected export file: %v", err)
	}
}

func TestImportCmd_ExecutesClosure(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	tmpDir, configPath := makeTestConfig(t)
	inPath := filepath.Join(tmpDir, "import.json")
	export := api.SessionExport{Version: "1.0", Session: api.Session{ID: "imported-id", Path: "/tmp"}}
	data, err := json.Marshal(export)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	mock := &mockApp{importReturn: &api.Session{ID: "imported-id", Path: "/tmp"}}
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return mock, nil }
	writeDefaultConfig = func() error { return nil }

	cmd := newRootCmd()
	cmd.SetArgs([]string{"import", "--input", inPath, "--config", configPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRun_PprofAddr(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	_, configPath := makeTestConfig(t)
	mock := &mockApp{startSessionReturn: &api.Session{ID: "sess-pprof", Path: "/tmp"}}
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return mock, nil }
	writeDefaultConfig = func() error { return nil }

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--pprof", "localhost:6060", "--config", configPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRun_ContinueLast_StartSessionError(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	_, configPath := makeTestConfig(t)
	mock := &mockApp{
		continueLastErr: store.ErrSessionNotFound,
		startSessionErr: errors.New("start failure"),
	}
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return mock, nil }
	writeDefaultConfig = func() error { return nil }

	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--continue", "--config", configPath})
	err := cmd.Execute()

	if err == nil {
		t.Fatal("expected error for start session failure after continue not found")
	}
	if !strings.Contains(err.Error(), "start session") {
		t.Errorf("expected start session error, got: %v", err)
	}
}

func TestRun_ResolveModelWarning(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	origResolveModel := resolveModelFromConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
		resolveModelFromConfig = origResolveModel
	}()

	_, configPath := makeTestConfig(t)
	mock := &mockApp{startSessionReturn: &api.Session{ID: "sess-model", Path: "/tmp"}}
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return mock, nil }
	writeDefaultConfig = func() error { return nil }
	resolveModelFromConfig = func(cfg *api.Config) (string, error) {
		return "", errors.New("model resolution failure")
	}

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
	if !strings.Contains(buf.String(), "Warning: could not resolve model") {
		t.Errorf("expected model warning, got: %q", buf.String())
	}
}

func TestRunPrompt_EncodeToolResultError(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	origStdout := stdout
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
		stdout = origStdout
	}()

	ch := make(chan api.TurnEvent, 1)
	ch <- api.TurnEvent{Type: api.TurnEventToolResult, Result: api.ToolResult{CallID: "call-1"}}
	close(ch)

	mock := &mockApp{
		startSessionReturn: &api.Session{ID: "sess-tre", Path: "/tmp"},
		runTurnReturn:      ch,
	}
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return mock, nil }
	writeDefaultConfig = func() error { return nil }

	_, configPath := makeTestConfig(t)
	stdout = &failingWriter{failAfter: 0, err: errors.New("encode failed")}
	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--prompt", "go", "--output-format", "json", "--config", configPath})
	err := cmd.Execute()

	if err == nil {
		t.Fatal("expected error for encode tool result failure")
	}
	if !strings.Contains(err.Error(), "encode tool result event") {
		t.Errorf("expected encode tool result event error, got: %v", err)
	}
}

func TestRunPrompt_EncodeDoneError(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	origStdout := stdout
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
		stdout = origStdout
	}()

	ch := make(chan api.TurnEvent, 1)
	ch <- api.TurnEvent{Type: api.TurnEventDone}
	close(ch)

	mock := &mockApp{
		startSessionReturn: &api.Session{ID: "sess-de", Path: "/tmp"},
		runTurnReturn:      ch,
	}
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return mock, nil }
	writeDefaultConfig = func() error { return nil }

	_, configPath := makeTestConfig(t)
	stdout = &failingWriter{failAfter: 0, err: errors.New("encode failed")}
	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--prompt", "go", "--output-format", "json", "--config", configPath})
	err := cmd.Execute()

	if err == nil {
		t.Fatal("expected error for encode done failure")
	}
	if !strings.Contains(err.Error(), "encode done event") {
		t.Errorf("expected encode done event error, got: %v", err)
	}
}

func TestExportCmd_WriteDefaultConfigWarning(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	tmpDir, configPath := makeTestConfig(t)
	mock := &mockApp{exportReturn: &api.SessionExport{Version: "1.0"}}
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return mock, nil }
	writeDefaultConfig = func() error { return errors.New("permission denied") }

	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	cmd := newRootCmd()
	outPath := filepath.Join(tmpDir, "export.json")
	cmd.SetArgs([]string{"export", "--session", "sess-1", "--output", outPath, "--config", configPath})
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
}

func TestExportCmd_LoadConfigError(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	if err := os.WriteFile(configPath, []byte("invalid toml {"), 0644); err != nil {
		t.Fatal(err)
	}
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return &mockApp{}, nil }
	writeDefaultConfig = func() error { return nil }

	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"export", "--session", "sess-1", "--output", filepath.Join(tmpDir, "out.json"), "--config", configPath})
	err := cmd.Execute()

	if err == nil {
		t.Fatal("expected error for bad config")
	}
	if !strings.Contains(err.Error(), "load config") {
		t.Errorf("expected load config error, got: %v", err)
	}
}

func TestExportCmd_NewAppError(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	_, configPath := makeTestConfig(t)
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return nil, errors.New("app failure") }
	writeDefaultConfig = func() error { return nil }

	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"export", "--session", "sess-1", "--output", "/tmp/out.json", "--config", configPath})
	err := cmd.Execute()

	if err == nil {
		t.Fatal("expected error for newApp failure")
	}
	if !strings.Contains(err.Error(), "initialize app") {
		t.Errorf("expected initialize app error, got: %v", err)
	}
}

func TestImportCmd_WriteDefaultConfigWarning(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	tmpDir, configPath := makeTestConfig(t)
	inPath := filepath.Join(tmpDir, "import.json")
	export := api.SessionExport{Version: "1.0", Session: api.Session{ID: "imported-id", Path: "/tmp"}}
	data, err := json.Marshal(export)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	mock := &mockApp{importReturn: &api.Session{ID: "imported-id", Path: "/tmp"}}
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return mock, nil }
	writeDefaultConfig = func() error { return errors.New("permission denied") }

	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	cmd := newRootCmd()
	cmd.SetArgs([]string{"import", "--input", inPath, "--config", configPath})
	err = cmd.Execute()

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
}

func TestImportCmd_LoadConfigError(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	if err := os.WriteFile(configPath, []byte("invalid toml {"), 0644); err != nil {
		t.Fatal(err)
	}
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return &mockApp{}, nil }
	writeDefaultConfig = func() error { return nil }

	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"import", "--input", filepath.Join(tmpDir, "in.json"), "--config", configPath})
	err := cmd.Execute()

	if err == nil {
		t.Fatal("expected error for bad config")
	}
	if !strings.Contains(err.Error(), "load config") {
		t.Errorf("expected load config error, got: %v", err)
	}
}

func TestImportCmd_NewAppError(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
	}()

	tmpDir, configPath := makeTestConfig(t)
	inPath := filepath.Join(tmpDir, "import.json")
	export := api.SessionExport{Version: "1.0"}
	data, err := json.Marshal(export)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inPath, data, 0644); err != nil {
		t.Fatal(err)
	}
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return nil, errors.New("app failure") }
	writeDefaultConfig = func() error { return nil }

	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"import", "--input", inPath, "--config", configPath})
	err = cmd.Execute()

	if err == nil {
		t.Fatal("expected error for newApp failure")
	}
	if !strings.Contains(err.Error(), "initialize app") {
		t.Errorf("expected initialize app error, got: %v", err)
	}
}

func TestACPCmd_WriteDefaultConfigWarning(t *testing.T) {
	origNewApp := newApp
	origWriteDefaultConfig := writeDefaultConfig
	origStdin := os.Stdin
	origStdout := os.Stdout
	defer func() {
		newApp = origNewApp
		writeDefaultConfig = origWriteDefaultConfig
		os.Stdin = origStdin
		os.Stdout = origStdout
	}()

	_, configPath := makeTestConfig(t)
	newApp = func(cfg *api.Config, debug bool) (appRunner, error) { return &mockApp{}, nil }
	writeDefaultConfig = func() error { return errors.New("permission denied") }

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	os.Stdin = r

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = outW
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, outR)
		done <- buf.String()
	}()

	oldStderr := os.Stderr
	errR, errW, _ := os.Pipe()
	os.Stderr = errW

	cmd := newRootCmd()
	cmd.SetArgs([]string{"acp", "--config", configPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	errW.Close()
	os.Stderr = oldStderr
	var errBuf bytes.Buffer
	io.Copy(&errBuf, errR)
	outW.Close()
	<-done

	if !strings.Contains(errBuf.String(), "Warning: could not write default config") {
		t.Errorf("expected stderr warning, got: %q", errBuf.String())
	}
}

func TestACPCmd_LoadConfigError(t *testing.T) {
	origWriteDefaultConfig := writeDefaultConfig
	defer func() { writeDefaultConfig = origWriteDefaultConfig }()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	if err := os.WriteFile(configPath, []byte("invalid toml {"), 0644); err != nil {
		t.Fatal(err)
	}
	writeDefaultConfig = func() error { return nil }

	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"acp", "--config", configPath})
	err := cmd.Execute()

	if err == nil {
		t.Fatal("expected error for bad config")
	}
	if !strings.Contains(err.Error(), "load config") {
		t.Errorf("expected load config error, got: %v", err)
	}
}

func TestDoctorCmd_DefaultConfigLoadFails(t *testing.T) {
	origWriteDefaultConfig := writeDefaultConfig
	defer func() { writeDefaultConfig = origWriteDefaultConfig }()

	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	configDir = filepath.Join(configDir, "kimi-lite")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte("invalid toml {"), 0644); err != nil {
		t.Fatal(err)
	}
	writeDefaultConfig = func() error { return nil }

	out := captureStdout(t, func() {
		if err := runDoctor(context.Background(), ""); err == nil {
			t.Fatal("expected error for default config failure")
		}
	})

	if !strings.Contains(out, "[FAIL] Config") {
		t.Errorf("expected config failure message, got: %q", out)
	}
}
