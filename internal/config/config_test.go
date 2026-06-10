package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg == nil {
		t.Fatal("DefaultConfig() returned nil")
	}
	if cfg.LLM.Provider != "moonshot" {
		t.Errorf("expected provider moonshot, got %s", cfg.LLM.Provider)
	}
	if cfg.LLM.Model != "kimi-k2.5" {
		t.Errorf("expected model kimi-k2.5, got %s", cfg.LLM.Model)
	}
	if cfg.LLM.BaseURL != "https://api.moonshot.cn/v1" {
		t.Errorf("unexpected base URL: %s", cfg.LLM.BaseURL)
	}
	if cfg.LLM.Timeout != 60*time.Second {
		t.Errorf("expected timeout 60s, got %v", cfg.LLM.Timeout)
	}
	if len(cfg.Behavior.AutoApprove) == 0 {
		t.Error("expected non-empty auto-approve list")
	}
	if cfg.Behavior.ShellTimeout != 30*time.Second {
		t.Errorf("expected shell timeout 30s, got %v", cfg.Behavior.ShellTimeout)
	}
	if cfg.Behavior.MaxTurns != 50 {
		t.Errorf("expected max turns 50, got %d", cfg.Behavior.MaxTurns)
	}
	if cfg.Session.DBPath != "~/.local/share/kimi-lite/sessions.db" {
		t.Errorf("unexpected db_path: %s", cfg.Session.DBPath)
	}
	if cfg.Session.MaxHistory != 100 {
		t.Errorf("expected max history 100, got %d", cfg.Session.MaxHistory)
	}
	if cfg.UI.Theme != "dark" {
		t.Errorf("expected theme dark, got %s", cfg.UI.Theme)
	}
	if !cfg.UI.ShowTokenCount {
		t.Error("expected show_token_count true")
	}
	if cfg.UI.Editor != "vim" {
		t.Errorf("expected editor vim, got %s", cfg.UI.Editor)
	}
}

func TestLoaderLoad_WithTempConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	content := `
[llm]
provider = "openai"
api_key = "test-api-key"
model = "gpt-4"
base_url = "https://api.openai.com/v1"
timeout = "120s"

[behavior]
auto_approve = ["read_file"]
shell_timeout = "60s"
max_turns = 10

[session]
db_path = "/tmp/test.db"
max_history = 50

[ui]
theme = "light"
show_token_count = false
editor = "nano"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	t.Chdir(tmpDir)
	t.Setenv("HOME", tmpDir)

	loader := NewLoader()
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.LLM.Provider != "openai" {
		t.Errorf("expected provider openai, got %s", cfg.LLM.Provider)
	}
	if cfg.LLM.APIKey != "test-api-key" {
		t.Errorf("expected api_key test-api-key, got %s", cfg.LLM.APIKey)
	}
	if cfg.LLM.Model != "gpt-4" {
		t.Errorf("expected model gpt-4, got %s", cfg.LLM.Model)
	}
	if cfg.LLM.BaseURL != "https://api.openai.com/v1" {
		t.Errorf("unexpected base URL: %s", cfg.LLM.BaseURL)
	}
	if cfg.LLM.Timeout != 120*time.Second {
		t.Errorf("expected timeout 120s, got %v", cfg.LLM.Timeout)
	}
	if len(cfg.Behavior.AutoApprove) != 1 || cfg.Behavior.AutoApprove[0] != "read_file" {
		t.Errorf("unexpected auto_approve: %v", cfg.Behavior.AutoApprove)
	}
	if cfg.Behavior.ShellTimeout != 60*time.Second {
		t.Errorf("expected shell timeout 60s, got %v", cfg.Behavior.ShellTimeout)
	}
	if cfg.Behavior.MaxTurns != 10 {
		t.Errorf("expected max turns 10, got %d", cfg.Behavior.MaxTurns)
	}
	if cfg.Session.DBPath != "/tmp/test.db" {
		t.Errorf("unexpected db_path: %s", cfg.Session.DBPath)
	}
	if cfg.Session.MaxHistory != 50 {
		t.Errorf("expected max history 50, got %d", cfg.Session.MaxHistory)
	}
	if cfg.UI.Theme != "light" {
		t.Errorf("expected theme light, got %s", cfg.UI.Theme)
	}
	if cfg.UI.ShowTokenCount {
		t.Error("expected show_token_count false")
	}
	if cfg.UI.Editor != "nano" {
		t.Errorf("expected editor nano, got %s", cfg.UI.Editor)
	}
}

func TestEnvVarResolution(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		config   string
		want     string
	}{
		{
			name:     "$VAR syntax",
			envValue: "resolved-key",
			config:   "$TEST_API_KEY",
			want:     "resolved-key",
		},
		{
			name:     "${VAR} syntax",
			envValue: "resolved-key",
			config:   "${TEST_API_KEY}",
			want:     "resolved-key",
		},
		{
			name:     "empty var returns empty",
			envValue: "",
			config:   "$TEST_API_KEY",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TEST_API_KEY", tt.envValue)

			tmpDir := t.TempDir()
			content := `[llm]
provider = "moonshot"
api_key = "` + tt.config + `"
model = "kimi-k2.5"
`
			configPath := filepath.Join(tmpDir, "config.toml")
			os.WriteFile(configPath, []byte(content), 0644)
			t.Chdir(tmpDir)

			loader := NewLoader()
			cfg, err := loader.Load()
			if err != nil {
				t.Fatalf("Load() error: %v", err)
			}

			if cfg.LLM.APIKey != tt.want {
				t.Errorf("expected API key %q, got %q", tt.want, cfg.LLM.APIKey)
			}
		})
	}
}

func TestEnvVarResolution_MissingVar(t *testing.T) {
	os.Unsetenv("TEST_API_KEY_MISSING")

	tmpDir := t.TempDir()
	content := `[llm]
provider = "moonshot"
api_key = "$TEST_API_KEY_MISSING"
model = "kimi-k2.5"
`
	configPath := filepath.Join(tmpDir, "config.toml")
	os.WriteFile(configPath, []byte(content), 0644)
	t.Chdir(tmpDir)

	loader := NewLoader()
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.LLM.APIKey != "$TEST_API_KEY_MISSING" {
		t.Errorf("expected API key %q, got %q", "$TEST_API_KEY_MISSING", cfg.LLM.APIKey)
	}
}

func TestExpandPath(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	tests := []struct {
		input    string
		expected string
	}{
		{"~/test.db", filepath.Join(tmpDir, "test.db")},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
		{"", ""},
	}

	for _, tt := range tests {
		got := expandPath(tt.input)
		if got != tt.expected {
			t.Errorf("expandPath(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestWriteDefaultConfig(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("UserConfigDir error: %v", err)
	}
	expectedPath := filepath.Join(configDir, "kimi-lite", "config.toml")

	// Remove if exists from previous test run
	os.RemoveAll(filepath.Join(configDir, "kimi-lite"))

	err = WriteDefaultConfig()
	if err != nil {
		t.Fatalf("WriteDefaultConfig() error: %v", err)
	}

	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Fatalf("expected config file at %s", expectedPath)
	}

	// Verify it's idempotent
	err = WriteDefaultConfig()
	if err != nil {
		t.Fatalf("WriteDefaultConfig() second call error: %v", err)
	}

	content, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "[llm]") {
		t.Error("expected config to contain [llm] section")
	}
	if !strings.Contains(string(content), "provider = \"moonshot\"") {
		t.Error("expected config to contain provider")
	}
}
