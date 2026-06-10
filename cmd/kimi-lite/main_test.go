package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

type mockApp struct {
	setYoloCalled        bool
	setAutoApproveCalled bool
	resumeSessionID      string
	continueLastCalled   bool
	startSessionCalled   bool
	runSession           *api.Session
	runCalled            bool

	resumeSessionReturn *api.Session
	continueLastReturn  *api.Session
	startSessionReturn  *api.Session
	resumeSessionErr    error
	continueLastErr     error
	startSessionErr     error
	runErr              error
}

func (m *mockApp) SetYolo(v bool)        { m.setYoloCalled = v }
func (m *mockApp) SetAutoApprove(v bool) { m.setAutoApproveCalled = v }
func (m *mockApp) Close() error          { return nil }
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
func (m *mockApp) ExportSession(_ context.Context, _ string) (*api.SessionExport, error) {
	return nil, nil
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
