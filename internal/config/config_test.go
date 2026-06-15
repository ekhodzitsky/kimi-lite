package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// TestMain isolates the package from the host environment before running
// tests: it moves HOME/XDG_CONFIG_HOME to an empty temp dir and clears
// KIMI_* and provider API-key env vars so that CI-set configuration does not
// override values expected from temporary config files.
func TestMain(m *testing.M) {
	extraKeys := map[string]struct{}{
		"MOONSHOT_API_KEY":   {},
		"OPENAI_API_KEY":     {},
		"ANTHROPIC_API_KEY":  {},
		"WEB_SEARCH_API_KEY": {},
	}

	var preserved [][2]string
	for _, e := range os.Environ() {
		if i := strings.IndexByte(e, '='); i >= 0 {
			key := e[:i]
			if strings.HasPrefix(key, "KIMI_") {
				preserved = append(preserved, [2]string{key, e[i+1:]})
			} else if _, ok := extraKeys[key]; ok {
				preserved = append(preserved, [2]string{key, e[i+1:]})
			}
		}
	}
	for _, kv := range preserved {
		_ = os.Unsetenv(kv[0])
	}

	tmpHome, err := os.MkdirTemp("", "kimi-config-test-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create temp home: %v\n", err)
		os.Exit(1)
	}
	oldHome, _ := os.LookupEnv("HOME")
	oldXDG, _ := os.LookupEnv("XDG_CONFIG_HOME")
	_ = os.Setenv("HOME", tmpHome)
	_ = os.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpHome, ".config"))

	code := m.Run()

	_ = os.Setenv("HOME", oldHome)
	_ = os.Setenv("XDG_CONFIG_HOME", oldXDG)
	_ = os.RemoveAll(tmpHome)
	for _, kv := range preserved {
		_ = os.Setenv(kv[0], kv[1])
	}
	os.Exit(code)
}

// clearEnvVars unsets environment variables that can leak into config tests
// from the CI environment and override values from temporary config files.
func clearEnvVars(t *testing.T, keys ...string) {
	t.Helper()
	for _, key := range keys {
		old, ok := os.LookupEnv(key)
		if ok {
			os.Unsetenv(key)
			t.Cleanup(func() { _ = os.Setenv(key, old) })
		}
	}
}

// clearConfigEnv unsets all KIMI_* env vars plus provider API keys that may
// be present in CI so that tests load values from their temp config files.
func clearConfigEnv(t *testing.T) {
	t.Helper()
	var keys []string
	for _, e := range os.Environ() {
		if i := strings.IndexByte(e, '='); i > 0 {
			key := e[:i]
			if strings.HasPrefix(key, "KIMI_") {
				keys = append(keys, key)
			}
		}
	}
	// Also clear common provider API keys that config files may reference.
	keys = append(keys, "MOONSHOT_API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY")
	clearEnvVars(t, keys...)
}

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
	wantAutoApprove := []string{"read_file", "grep", "glob", "fetch_url", "list_directory", "web_search"}
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
	if !cfg.Behavior.AllowShell {
		t.Error("expected behavior.allow_shell true by default")
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
	if cfg.Permission.RiskThreshold != api.RiskLevelMedium {
		t.Errorf("expected permission.risk_threshold medium, got %q", cfg.Permission.RiskThreshold)
	}
	if cfg.Permission.RiskRules == nil {
		t.Error("expected permission.risk_rules non-nil")
	}
}

func TestLoaderLoad_WithTempConfigFile(t *testing.T) {
	clearConfigEnv(t)

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
			clearConfigEnv(t)
			t.Setenv("TEST_API_KEY", tt.envValue)

			tmpDir := t.TempDir()
			t.Setenv("HOME", tmpDir)
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
	clearConfigEnv(t)
	t.Setenv("TEST_API_KEY_MISSING", "")

	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	content := `[llm]
provider = "moonshot"
api_key = "$TEST_API_KEY_MISSING"
model = "kimi-k2.5"
`
	configPath := filepath.Join(tmpDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(tmpDir)

	loader := NewLoader()
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.LLM.APIKey != "" {
		t.Errorf("expected empty API key when env var is set to empty, got %q", cfg.LLM.APIKey)
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

	dir, err := EnsureConfigDirAt(tmpDir)
	if err != nil {
		t.Fatalf("EnsureConfigDirAt() error: %v", err)
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

	if err := WriteDefaultConfigTo(tmpDir); err != nil {
		t.Fatalf("WriteDefaultConfigTo() error: %v", err)
	}

	configPath := filepath.Join(tmpDir, "config.toml")
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
	expectedPath := filepath.Join(tmpDir, "config.toml")

	if err := WriteDefaultConfigTo(tmpDir); err != nil {
		t.Fatalf("WriteDefaultConfigTo() error: %v", err)
	}

	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Fatalf("expected config file at %s", expectedPath)
	}

	// Verify it's idempotent
	if err := WriteDefaultConfigTo(tmpDir); err != nil {
		t.Fatalf("WriteDefaultConfigTo() second call error: %v", err)
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
	t.Setenv("MOONSHOT_API_KEY", "")
	configPath := filepath.Join(tmpDir, "config.toml")

	if err := WriteDefaultConfigTo(tmpDir); err != nil {
		t.Fatalf("WriteDefaultConfigTo() error: %v", err)
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
	_ = os.Unsetenv("UNSET_VAR")
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
		{"$UNSET_VAR", ""},
		{"${UNSET_VAR}", ""},
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
				Behavior: api.BehaviorConfig{ShellTimeout: 30 * time.Second, MaxTurns: 50, MaxToolRounds: 10},
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
				Behavior: api.BehaviorConfig{ShellTimeout: 30 * time.Second, MaxTurns: 50, MaxToolRounds: 10},
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
				Behavior: api.BehaviorConfig{ShellTimeout: 30 * time.Second, MaxTurns: 50, MaxToolRounds: 10},
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
				Behavior: api.BehaviorConfig{ShellTimeout: 30 * time.Second, MaxTurns: 50, MaxToolRounds: 10},
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
				Behavior: api.BehaviorConfig{ShellTimeout: 30 * time.Second, MaxTurns: 50, MaxToolRounds: 10},
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
				Behavior: api.BehaviorConfig{ShellTimeout: 30 * time.Second, MaxTurns: 50, MaxToolRounds: 10},
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
				Behavior: api.BehaviorConfig{ShellTimeout: 30 * time.Second, MaxTurns: 50, MaxToolRounds: 10},
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

func TestValidate_MCPServers(t *testing.T) {
	t.Parallel()

	validStdio := DefaultConfig()
	validStdio.MCPServers = map[string]api.MCPServerConfig{
		"fs": {
			Enabled:          true,
			Transport:        api.MCPTransportStdio,
			Command:          "npx",
			Args:             []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp"},
			StartupTimeoutMs: 5000,
			ToolTimeoutMs:    30000,
		},
	}
	if err := Validate(validStdio); err != nil {
		t.Fatalf("expected valid stdio server, got: %v", err)
	}

	validHTTP := DefaultConfig()
	validHTTP.MCPServers = map[string]api.MCPServerConfig{
		"remote": {
			Enabled:          true,
			Transport:        api.MCPTransportHTTP,
			URL:              "https://example.com/mcp",
			StartupTimeoutMs: 5000,
			ToolTimeoutMs:    30000,
		},
	}
	if err := Validate(validHTTP); err != nil {
		t.Fatalf("expected valid http server, got: %v", err)
	}

	noCommand := DefaultConfig()
	noCommand.MCPServers = map[string]api.MCPServerConfig{
		"fs": {Enabled: true, Transport: api.MCPTransportStdio, Command: "", StartupTimeoutMs: 5000, ToolTimeoutMs: 30000},
	}
	if err := Validate(noCommand); err == nil || !strings.Contains(err.Error(), "command") {
		t.Fatalf("expected command validation error, got: %v", err)
	}

	badTransport := DefaultConfig()
	badTransport.MCPServers = map[string]api.MCPServerConfig{
		"x": {Enabled: true, Transport: "ws", Command: "cmd", StartupTimeoutMs: 5000, ToolTimeoutMs: 30000},
	}
	if err := Validate(badTransport); err == nil || !strings.Contains(err.Error(), "transport") {
		t.Fatalf("expected transport validation error, got: %v", err)
	}

	emptyURL := DefaultConfig()
	emptyURL.MCPServers = map[string]api.MCPServerConfig{
		"x": {Enabled: true, Transport: api.MCPTransportHTTP, URL: "", StartupTimeoutMs: 5000, ToolTimeoutMs: 30000},
	}
	if err := Validate(emptyURL); err == nil || !strings.Contains(err.Error(), "url") {
		t.Fatalf("expected url validation error, got: %v", err)
	}

	nonLocalHTTP := DefaultConfig()
	nonLocalHTTP.MCPServers = map[string]api.MCPServerConfig{
		"x": {Enabled: true, Transport: api.MCPTransportHTTP, URL: "http://example.com/mcp", StartupTimeoutMs: 5000, ToolTimeoutMs: 30000},
	}
	if err := Validate(nonLocalHTTP); err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("expected https validation error, got: %v", err)
	}
}

func TestLoader_MCPServers(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	content := `
[mcp_servers.fs]
transport = "stdio"
command = "npx"
args = ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
enabled = true
startup_timeout_ms = 10000
tool_timeout_ms = 30000
enabled_tools = ["read_file"]
disabled_tools = ["write_file"]

[mcp_servers.remote]
transport = "http"
url = "https://example.com/mcp"
enabled = true
startup_timeout_ms = 5000
tool_timeout_ms = 30000
headers = { X-Custom = "value" }
bearer_token_env_var = "MCP_TOKEN"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	loader := NewLoader()
	loader.SetConfigFile(configPath)
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	fs, ok := cfg.MCPServers["fs"]
	if !ok {
		t.Fatal("expected mcp_servers.fs")
	}
	if fs.Transport != api.MCPTransportStdio {
		t.Errorf("fs.transport = %q, want stdio", fs.Transport)
	}
	if fs.Command != "npx" {
		t.Errorf("fs.command = %q, want npx", fs.Command)
	}
	if len(fs.Args) != 3 || fs.Args[0] != "-y" {
		t.Errorf("fs.args = %v, unexpected", fs.Args)
	}
	if fs.StartupTimeoutMs != 10000 {
		t.Errorf("fs.startup_timeout_ms = %d, want 10000", fs.StartupTimeoutMs)
	}
	if fs.ToolTimeoutMs != 30000 {
		t.Errorf("fs.tool_timeout_ms = %d, want 30000", fs.ToolTimeoutMs)
	}
	if len(fs.EnabledTools) != 1 || fs.EnabledTools[0] != "read_file" {
		t.Errorf("fs.enabled_tools = %v, want [read_file]", fs.EnabledTools)
	}
	if len(fs.DisabledTools) != 1 || fs.DisabledTools[0] != "write_file" {
		t.Errorf("fs.disabled_tools = %v, want [write_file]", fs.DisabledTools)
	}

	remote, ok := cfg.MCPServers["remote"]
	if !ok {
		t.Fatal("expected mcp_servers.remote")
	}
	if remote.URL != "https://example.com/mcp" {
		t.Errorf("remote.url = %q, want https://example.com/mcp", remote.URL)
	}
	var headerVal string
	for k, v := range remote.Headers {
		if strings.EqualFold(k, "X-Custom") {
			headerVal = v
			break
		}
	}
	if headerVal != "value" {
		t.Errorf("remote.headers = %v, want X-Custom=value", remote.Headers)
	}
	if remote.BearerTokenEnvVar != "MCP_TOKEN" {
		t.Errorf("remote.bearer_token_env_var = %q, want MCP_TOKEN", remote.BearerTokenEnvVar)
	}
}

func TestValidate_AccumulatesErrors(t *testing.T) {
	cfg := api.Config{
		LLM: api.LLMConfig{
			Model:   "kimi-k2.5",
			Timeout: -1 * time.Second,
		},
		Behavior: api.BehaviorConfig{
			ShellTimeout:  30 * time.Second,
			MaxTurns:      50,
			MaxToolRounds: 10,
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

func TestValidate_RiskRules(t *testing.T) {
	t.Parallel()

	valid := DefaultConfig()
	valid.Permission.RiskThreshold = api.RiskLevelLow
	valid.Permission.RiskRules = []api.RiskRule{
		{Tool: "shell", Level: api.RiskLevelHigh},
	}
	if err := Validate(valid); err != nil {
		t.Fatalf("expected valid config, got: %v", err)
	}

	invalid := DefaultConfig()
	invalid.Permission.RiskThreshold = api.RiskLevel("extreme")
	invalid.Permission.RiskRules = []api.RiskRule{
		{Tool: "", Level: api.RiskLevelLow},
		{Tool: "shell", Level: api.RiskLevel("extreme")},
	}
	err := Validate(invalid)
	if err == nil {
		t.Fatal("expected validation error")
	}
	msg := err.Error()
	for _, want := range []string{"risk_threshold must be one of", "tool must not be empty", "level must be one of"} {
		if !strings.Contains(msg, want) {
			t.Errorf("expected error to contain %q, got: %s", want, msg)
		}
	}
}

func TestValidate_Hooks(t *testing.T) {
	t.Parallel()

	valid := DefaultConfig()
	valid.Hooks = []api.HookConfig{
		{Event: api.HookToolCall, Command: "echo", Timeout: 5 * time.Second},
	}
	if err := Validate(valid); err != nil {
		t.Fatalf("expected valid config, got: %v", err)
	}

	zeroTimeout := DefaultConfig()
	zeroTimeout.Hooks = []api.HookConfig{
		{Event: api.HookToolCall, Command: "echo", Timeout: 0},
	}
	if err := Validate(zeroTimeout); err != nil {
		t.Fatalf("expected zero timeout to mean no timeout, got: %v", err)
	}

	invalid := DefaultConfig()
	invalid.Hooks = []api.HookConfig{
		{Event: "unknown_event", Command: "echo"},
		{Event: api.HookToolCall, Command: ""},
		{Event: api.HookTurnStart, Command: "echo", Timeout: -1 * time.Second},
	}
	err := Validate(invalid)
	if err == nil {
		t.Fatal("expected validation error")
	}
	msg := err.Error()
	for _, want := range []string{"hooks[0].event", "hooks[1].command", "hooks[2].timeout"} {
		if !strings.Contains(msg, want) {
			t.Errorf("expected error to contain %q, got: %s", want, msg)
		}
	}
}

func TestValidate_Nil(t *testing.T) {
	err := Validate(nil)
	if err == nil || !strings.Contains(err.Error(), "nil") {
		t.Fatalf("expected nil config error, got: %v", err)
	}
}

func TestLoader_MCPServerEnvAndCWD(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("MCP_SECRET", "super-secret")
	configPath := filepath.Join(tmpDir, "config.toml")
	content := `
[llm]
provider = "moonshot"
api_key = "key"
model = "kimi-k2.5"

[mcp_servers.fs]
transport = "stdio"
command = "npx"
enabled = true
startup_timeout_ms = 5000
tool_timeout_ms = 30000
cwd = "~/mcp"
[mcp_servers.fs.env]
SECRET = "$MCP_SECRET"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	loader := NewLoader()
	loader.SetConfigFile(configPath)
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	srv, ok := cfg.MCPServers["fs"]
	if !ok {
		t.Fatal("expected mcp_servers.fs")
	}
	if got := srv.Env["SECRET"]; got != "super-secret" {
		t.Errorf("expected env SECRET resolved to %q, got %q", "super-secret", got)
	}
	wantCWD := filepath.Join(tmpDir, "mcp")
	if srv.CWD != wantCWD {
		t.Errorf("expected cwd %q, got %q", wantCWD, srv.CWD)
	}
}

func TestLoader_ExplicitConfigError(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	if err := os.WriteFile(configPath, []byte("not valid toml [[["), 0644); err != nil {
		t.Fatal(err)
	}

	loader := NewLoader()
	loader.SetConfigFile(configPath)
	if _, err := loader.Load(); err == nil {
		t.Fatal("expected error for invalid explicit config")
	}
}

func TestLoader_MissingDefaultConfigFallsBack(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("MOONSHOT_API_KEY", "default-key")
	t.Chdir(tmpDir)

	loader := NewLoader()
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.LLM.APIKey != "default-key" {
		t.Errorf("expected API key resolved from env, got %q", cfg.LLM.APIKey)
	}
}

func TestLoader_ExplicitMissingConfigReturnsError(t *testing.T) {
	tmpDir := t.TempDir()
	loader := NewLoader()
	loader.SetConfigFile(filepath.Join(tmpDir, "does-not-exist.toml"))
	if _, err := loader.Load(); err == nil {
		t.Fatal("expected error for missing explicit config")
	}
}
