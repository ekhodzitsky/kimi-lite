package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
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
	wantAutoApprove := []string{"read_file", "grep", "glob", "fetch_url", "list_directory"}
	if len(cfg.Behavior.AutoApprove) != len(wantAutoApprove) {
		t.Errorf("expected auto-approve list %v, got %v", wantAutoApprove, cfg.Behavior.AutoApprove)
	}
	for i, name := range wantAutoApprove {
		if cfg.Behavior.AutoApprove[i] != name {
			t.Errorf("expected auto-approve[%d] = %q, got %q", i, name, cfg.Behavior.AutoApprove[i])
		}
	}
	if cfg.Behavior.ShellTimeout != 30*time.Second {
		t.Errorf("expected shell timeout 30s, got %v", cfg.Behavior.ShellTimeout)
	}
	if cfg.Behavior.PassEnv {
		t.Error("expected pass_env false by default")
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

func TestEnsureConfigDir_Permissions(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	dir, err := EnsureConfigDir()
	if err != nil {
		t.Fatalf("EnsureConfigDir() error: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat config dir: %v", err)
	}
	if info.Mode().Perm() != 0700 {
		t.Errorf("config dir permissions = %o, want %o", info.Mode().Perm(), 0700)
	}
}

func TestWriteDefaultConfig_Permissions(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	configDir, _ := os.UserConfigDir()
	os.RemoveAll(filepath.Join(configDir, "kimi-lite"))

	if err := WriteDefaultConfig(); err != nil {
		t.Fatalf("WriteDefaultConfig() error: %v", err)
	}

	configPath := filepath.Join(configDir, "kimi-lite", "config.toml")
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat config file: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("config file permissions = %o, want %o", info.Mode().Perm(), 0600)
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

func TestWriteDefaultConfig_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("UserConfigDir error: %v", err)
	}
	configPath := filepath.Join(configDir, "kimi-lite", "config.toml")
	os.RemoveAll(filepath.Join(configDir, "kimi-lite"))

	if err := WriteDefaultConfig(); err != nil {
		t.Fatalf("WriteDefaultConfig() error: %v", err)
	}

	loader := NewLoader()
	loader.SetConfigFile(configPath)
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	want := *DefaultConfig()
	want.Session.DBPath = expandPath(want.Session.DBPath)
	want.MCP.GuardConfig = expandPath(want.MCP.GuardConfig)
	want.LLM.APIKey = resolveEnvVar(want.LLM.APIKey)
	if want.LLM.Fallback != nil {
		want.LLM.Fallback.APIKey = resolveEnvVar(want.LLM.Fallback.APIKey)
	}

	if !reflect.DeepEqual(cfg, &want) {
		t.Errorf("loaded config does not match DefaultConfig()\n got: %+v\nwant: %+v", cfg, &want)
	}
}

func TestDefault(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("MOONSHOT_API_KEY", "test-key")

	cfg, err := Default()
	if err != nil {
		t.Fatalf("Default() error: %v", err)
	}

	expectedDBPath := filepath.Join(tmpDir, ".local", "share", "kimi-lite", "sessions.db")
	if cfg.Session.DBPath != expectedDBPath {
		t.Errorf("expected DBPath %q, got %q", expectedDBPath, cfg.Session.DBPath)
	}

	if cfg.LLM.APIKey != "test-key" {
		t.Errorf("expected API key %q, got %q", "test-key", cfg.LLM.APIKey)
	}
}

func TestResolveEnvVar_Strict(t *testing.T) {
	t.Setenv("MYVAR", "resolved")
	tests := []struct {
		input string
		want  string
	}{
		{"${MYVAR}", "resolved"},
		{"$MYVAR", "resolved"},
		{"${MYVAR", "${MYVAR"},
		{"$MYVAR}", "$MYVAR}"},
		{"$abc123==", "$abc123=="},
		{"plain", "plain"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := resolveEnvVar(tt.input)
			if got != tt.want {
				t.Errorf("resolveEnvVar(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExpandPath_BareTilde(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	tests := []struct {
		input    string
		expected string
	}{
		{"~", tmpDir},
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

func TestValidateLLM(t *testing.T) {
	tests := []struct {
		name    string
		cfg     api.Config
		wantErr string
	}{
		{
			name: "valid https",
			cfg: api.Config{
				LLM: api.LLMConfig{
					Model:   "kimi-k2.5",
					BaseURL: "https://api.moonshot.cn/v1",
					Timeout: 60 * time.Second,
				},
				Behavior: api.BehaviorConfig{ShellTimeout: 30 * time.Second, MaxTurns: 50},
				Session:  api.SessionConfig{DBPath: "/tmp/test.db"},
				UI:       api.UIConfig{Theme: "dark"},
			},
		},
		{
			name: "http localhost",
			cfg: api.Config{
				LLM: api.LLMConfig{
					Model:   "kimi-k2.5",
					BaseURL: "http://localhost:8080",
					Timeout: 60 * time.Second,
				},
				Behavior: api.BehaviorConfig{ShellTimeout: 30 * time.Second, MaxTurns: 50},
				Session:  api.SessionConfig{DBPath: "/tmp/test.db"},
				UI:       api.UIConfig{Theme: "dark"},
			},
		},
		{
			name: "http 127.0.0.1",
			cfg: api.Config{
				LLM: api.LLMConfig{
					Model:   "kimi-k2.5",
					BaseURL: "http://127.0.0.1:8080",
					Timeout: 60 * time.Second,
				},
				Behavior: api.BehaviorConfig{ShellTimeout: 30 * time.Second, MaxTurns: 50},
				Session:  api.SessionConfig{DBPath: "/tmp/test.db"},
				UI:       api.UIConfig{Theme: "dark"},
			},
		},
		{
			name: "http non-localhost rejected",
			cfg: api.Config{
				LLM: api.LLMConfig{
					Model:   "kimi-k2.5",
					BaseURL: "http://api.example.com",
					Timeout: 60 * time.Second,
				},
				Behavior: api.BehaviorConfig{ShellTimeout: 30 * time.Second, MaxTurns: 50},
				Session:  api.SessionConfig{DBPath: "/tmp/test.db"},
				UI:       api.UIConfig{Theme: "dark"},
			},
			wantErr: "https",
		},
		{
			name: "ftp scheme rejected",
			cfg: api.Config{
				LLM: api.LLMConfig{
					Model:   "kimi-k2.5",
					BaseURL: "ftp://x",
					Timeout: 60 * time.Second,
				},
				Behavior: api.BehaviorConfig{ShellTimeout: 30 * time.Second, MaxTurns: 50},
				Session:  api.SessionConfig{DBPath: "/tmp/test.db"},
				UI:       api.UIConfig{Theme: "dark"},
			},
			wantErr: "http(s)",
		},
		{
			name: "empty host rejected",
			cfg: api.Config{
				LLM: api.LLMConfig{
					Model:   "kimi-k2.5",
					BaseURL: "http://",
					Timeout: 60 * time.Second,
				},
				Behavior: api.BehaviorConfig{ShellTimeout: 30 * time.Second, MaxTurns: 50},
				Session:  api.SessionConfig{DBPath: "/tmp/test.db"},
				UI:       api.UIConfig{Theme: "dark"},
			},
			wantErr: "host",
		},
		{
			name: "fallback empty model rejected",
			cfg: api.Config{
				LLM: api.LLMConfig{
					Model:   "kimi-k2.5",
					BaseURL: "https://api.moonshot.cn/v1",
					Timeout: 60 * time.Second,
					Fallback: &api.LLMConfig{
						Model:   "",
						BaseURL: "https://fallback.example.com",
						Timeout: 60 * time.Second,
					},
				},
				Behavior: api.BehaviorConfig{ShellTimeout: 30 * time.Second, MaxTurns: 50},
				Session:  api.SessionConfig{DBPath: "/tmp/test.db"},
				UI:       api.UIConfig{Theme: "dark"},
			},
			wantErr: "llm.fallback",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(&tt.cfg)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("expected no error, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

func TestValidate_AccumulatesErrors(t *testing.T) {
	cfg := api.Config{
		LLM: api.LLMConfig{
			Model:   "kimi-k2.5",
			Timeout: -1 * time.Second,
		},
		Behavior: api.BehaviorConfig{
			ShellTimeout: 30 * time.Second,
		},
		Session: api.SessionConfig{
			DBPath: "",
		},
		UI: api.UIConfig{
			Theme: "dark",
		},
	}
	err := Validate(&cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	errStr := err.Error()
	if !strings.Contains(errStr, "llm.timeout") {
		t.Errorf("expected error to contain 'llm.timeout', got: %s", errStr)
	}
	if !strings.Contains(errStr, "session.db_path") {
		t.Errorf("expected error to contain 'session.db_path', got: %s", errStr)
	}
}

func TestValidate_PermissionRules(t *testing.T) {
	t.Parallel()

	valid := DefaultConfig()
	valid.Permission.Rules = []api.PermissionRule{
		{Tool: "read_file", Decision: api.PermissionAsk, Scope: api.PermissionScopeUser},
	}
	if err := Validate(valid); err != nil {
		t.Fatalf("expected valid config, got: %v", err)
	}

	invalid := DefaultConfig()
	invalid.Permission.Rules = []api.PermissionRule{
		{Tool: "", Decision: api.PermissionAllow, Scope: api.PermissionScopeUser},
		{Tool: "read_file", Decision: "maybe", Scope: api.PermissionScopeUser},
		{Tool: "read_file", Decision: api.PermissionAllow, Scope: "forever"},
	}
	err := Validate(invalid)
	if err == nil {
		t.Fatal("expected validation error")
	}
	msg := err.Error()
	for _, want := range []string{"tool must not be empty", "decision must be one of", "scope must be one of"} {
		if !strings.Contains(msg, want) {
			t.Errorf("expected error to contain %q, got: %s", want, msg)
		}
	}
}
