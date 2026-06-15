package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func TestValidateProviderType(t *testing.T) {
	valid := []api.ProviderType{
		api.ProviderTypeOpenAI,
		api.ProviderTypeAnthropic,
		api.ProviderTypeKimi,
		api.ProviderTypeGoogleGenAI,
		api.ProviderTypeOpenAIResponses,
		api.ProviderTypeVertexAI,
	}
	for _, pt := range valid {
		if !validateProviderType(string(pt)) {
			t.Errorf("validateProviderType(%q) = false, want true", pt)
		}
	}
	if validateProviderType("unknown") {
		t.Error("validateProviderType(\"unknown\") = true, want false")
	}
}

func TestValidateProviders(t *testing.T) {
	base := func() *api.Config {
		cfg := DefaultConfig()
		cfg.Providers = nil
		cfg.Models = nil
		return cfg
	}

	tests := []struct {
		name    string
		cfg     *api.Config
		wantErr string
	}{
		{
			name: "valid provider and model",
			cfg: func() *api.Config {
				cfg := base()
				cfg.DefaultProvider = "openai"
				cfg.Providers = map[string]api.ProviderConfig{
					"openai": {
						Type:         api.ProviderTypeOpenAI,
						BaseURL:      "https://api.openai.com/v1",
						APIKey:       "key",
						DefaultModel: "gpt-4o",
					},
				}
				cfg.Models = map[string]api.ModelAlias{
					"gpt4o": {Model: "gpt-4o", Provider: "openai"},
				}
				return cfg
			}(),
		},
		{
			name: "missing default_provider",
			cfg: func() *api.Config {
				cfg := base()
				cfg.Providers = map[string]api.ProviderConfig{
					"openai": {Type: api.ProviderTypeOpenAI, BaseURL: "https://api.openai.com/v1", APIKey: "key", DefaultModel: "gpt-4o"},
				}
				return cfg
			}(),
			wantErr: "default_provider must be set",
		},
		{
			name: "default_provider not found",
			cfg: func() *api.Config {
				cfg := base()
				cfg.DefaultProvider = "missing"
				cfg.Providers = map[string]api.ProviderConfig{
					"openai": {Type: api.ProviderTypeOpenAI, BaseURL: "https://api.openai.com/v1", APIKey: "key", DefaultModel: "gpt-4o"},
				}
				return cfg
			}(),
			wantErr: "default_provider \"missing\" not found",
		},
		{
			name: "provider missing type",
			cfg: func() *api.Config {
				cfg := base()
				cfg.DefaultProvider = "openai"
				cfg.Providers = map[string]api.ProviderConfig{
					"openai": {BaseURL: "https://api.openai.com/v1", APIKey: "key", DefaultModel: "gpt-4o"},
				}
				return cfg
			}(),
			wantErr: "providers.openai.type must not be empty",
		},
		{
			name: "provider invalid type",
			cfg: func() *api.Config {
				cfg := base()
				cfg.DefaultProvider = "openai"
				cfg.Providers = map[string]api.ProviderConfig{
					"openai": {Type: "bad", BaseURL: "https://api.openai.com/v1", APIKey: "key", DefaultModel: "gpt-4o"},
				}
				return cfg
			}(),
			wantErr: "providers.openai.type",
		},
		{
			name: "provider missing base_url",
			cfg: func() *api.Config {
				cfg := base()
				cfg.DefaultProvider = "openai"
				cfg.Providers = map[string]api.ProviderConfig{
					"openai": {Type: api.ProviderTypeOpenAI, APIKey: "key", DefaultModel: "gpt-4o"},
				}
				return cfg
			}(),
			wantErr: "providers.openai.base_url must not be empty",
		},
		{
			name: "provider invalid base_url",
			cfg: func() *api.Config {
				cfg := base()
				cfg.DefaultProvider = "openai"
				cfg.Providers = map[string]api.ProviderConfig{
					"openai": {Type: api.ProviderTypeOpenAI, BaseURL: "ftp://x", APIKey: "key", DefaultModel: "gpt-4o"},
				}
				return cfg
			}(),
			wantErr: "http(s)",
		},
		{
			name: "provider missing api_key",
			cfg: func() *api.Config {
				cfg := base()
				cfg.DefaultProvider = "openai"
				cfg.Providers = map[string]api.ProviderConfig{
					"openai": {Type: api.ProviderTypeOpenAI, BaseURL: "https://api.openai.com/v1", DefaultModel: "gpt-4o"},
				}
				return cfg
			}(),
			wantErr: "providers.openai.api_key must not be empty",
		},
		{
			name: "provider missing default_model",
			cfg: func() *api.Config {
				cfg := base()
				cfg.DefaultProvider = "openai"
				cfg.Providers = map[string]api.ProviderConfig{
					"openai": {Type: api.ProviderTypeOpenAI, BaseURL: "https://api.openai.com/v1", APIKey: "key"},
				}
				return cfg
			}(),
			wantErr: "providers.openai.default_model must not be empty",
		},
		{
			name: "model missing provider",
			cfg: func() *api.Config {
				cfg := base()
				cfg.DefaultProvider = "openai"
				cfg.Providers = map[string]api.ProviderConfig{
					"openai": {Type: api.ProviderTypeOpenAI, BaseURL: "https://api.openai.com/v1", APIKey: "key", DefaultModel: "gpt-4o"},
				}
				cfg.Models = map[string]api.ModelAlias{"gpt4o": {Model: "gpt-4o"}}
				return cfg
			}(),
			wantErr: "models.gpt4o.provider must not be empty",
		},
		{
			name: "model provider not found",
			cfg: func() *api.Config {
				cfg := base()
				cfg.DefaultProvider = "openai"
				cfg.Providers = map[string]api.ProviderConfig{
					"openai": {Type: api.ProviderTypeOpenAI, BaseURL: "https://api.openai.com/v1", APIKey: "key", DefaultModel: "gpt-4o"},
				}
				cfg.Models = map[string]api.ModelAlias{"gpt4o": {Model: "gpt-4o", Provider: "missing"}}
				return cfg
			}(),
			wantErr: "models.gpt4o.provider \"missing\" not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.cfg)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
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

func TestValidateURL(t *testing.T) {
	tests := []struct {
		name       string
		value      string
		allowEmpty bool
		wantErr    string
	}{
		{name: "empty allowed", value: "", allowEmpty: true},
		{name: "empty not allowed", value: "", allowEmpty: false, wantErr: "must not be empty"},
		{name: "invalid parse", value: "://bad", allowEmpty: false, wantErr: "valid URL"},
		{name: "missing host", value: "http:///path", allowEmpty: false, wantErr: "host"},
		{name: "bad scheme", value: "ftp://example.com", allowEmpty: false, wantErr: "http(s)"},
		{name: "http localhost", value: "http://localhost:8080", allowEmpty: false},
		{name: "http 127.0.0.1", value: "http://127.0.0.1:8080", allowEmpty: false},
		{name: "http loopback ipv6", value: "http://[::1]:8080", allowEmpty: false},
		{name: "http non-loopback rejected", value: "http://example.com", allowEmpty: false, wantErr: "https"},
		{name: "https public ok", value: "https://example.com", allowEmpty: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateURL("url", tt.value, tt.allowEmpty)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
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

func TestValidateMCPServer(t *testing.T) {
	tests := []struct {
		name    string
		srv     api.MCPServerConfig
		wantErr string
	}{
		{name: "disabled returns nil", srv: api.MCPServerConfig{Enabled: false, Transport: "bad"}},
		{name: "stdio valid", srv: api.MCPServerConfig{Enabled: true, Transport: api.MCPTransportStdio, Command: "npx", StartupTimeoutMs: 5000, ToolTimeoutMs: 30000}},
		{name: "stdio missing command", srv: api.MCPServerConfig{Enabled: true, Transport: api.MCPTransportStdio, StartupTimeoutMs: 5000, ToolTimeoutMs: 30000}, wantErr: "command"},
		{name: "http valid", srv: api.MCPServerConfig{Enabled: true, Transport: api.MCPTransportHTTP, URL: "https://example.com/mcp", StartupTimeoutMs: 5000, ToolTimeoutMs: 30000}},
		{name: "http empty url", srv: api.MCPServerConfig{Enabled: true, Transport: api.MCPTransportHTTP, StartupTimeoutMs: 5000, ToolTimeoutMs: 30000}, wantErr: "url"},
		{name: "http non-localhost", srv: api.MCPServerConfig{Enabled: true, Transport: api.MCPTransportHTTP, URL: "http://example.com/mcp", StartupTimeoutMs: 5000, ToolTimeoutMs: 30000}, wantErr: "https"},
		{name: "zero startup timeout", srv: api.MCPServerConfig{Enabled: true, Transport: api.MCPTransportStdio, Command: "npx", StartupTimeoutMs: 0, ToolTimeoutMs: 30000}, wantErr: "startup_timeout_ms"},
		{name: "zero tool timeout", srv: api.MCPServerConfig{Enabled: true, Transport: api.MCPTransportStdio, Command: "npx", StartupTimeoutMs: 5000, ToolTimeoutMs: 0}, wantErr: "tool_timeout_ms"},
		{name: "negative startup timeout", srv: api.MCPServerConfig{Enabled: true, Transport: api.MCPTransportStdio, Command: "npx", StartupTimeoutMs: -1, ToolTimeoutMs: 30000}, wantErr: "startup_timeout_ms"},
		{name: "negative tool timeout", srv: api.MCPServerConfig{Enabled: true, Transport: api.MCPTransportStdio, Command: "npx", StartupTimeoutMs: 5000, ToolTimeoutMs: -1}, wantErr: "tool_timeout_ms"},
		{name: "bad transport", srv: api.MCPServerConfig{Enabled: true, Transport: "ws", Command: "npx", StartupTimeoutMs: 5000, ToolTimeoutMs: 30000}, wantErr: "transport"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateMCPServer("srv", tt.srv)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
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

func TestValidate_MoreBranches(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*api.Config)
		wantErr string
	}{
		{
			name: "shell timeout not positive",
			mutate: func(cfg *api.Config) {
				cfg.Behavior.ShellTimeout = 0
			},
			wantErr: "behavior.shell_timeout must be positive",
		},
		{
			name: "max turns not positive",
			mutate: func(cfg *api.Config) {
				cfg.Behavior.MaxTurns = 0
			},
			wantErr: "behavior.max_turns must be positive",
		},
		{
			name: "max tool rounds not positive",
			mutate: func(cfg *api.Config) {
				cfg.Behavior.MaxToolRounds = 0
			},
			wantErr: "behavior.max_tool_rounds must be positive",
		},
		{
			name: "session max history negative",
			mutate: func(cfg *api.Config) {
				cfg.Session.MaxHistory = -1
			},
			wantErr: "session.max_history must not be negative",
		},
		{
			name: "ui theme empty",
			mutate: func(cfg *api.Config) {
				cfg.UI.Theme = ""
			},
			wantErr: "ui.theme must not be empty",
		},
		{
			name: "web_search endpoint non-local http",
			mutate: func(cfg *api.Config) {
				cfg.WebSearch.Endpoint = "http://example.com/search"
				cfg.WebSearch.Timeout = 10 * time.Second
			},
			wantErr: "https",
		},
		{
			name: "web_search timeout not positive",
			mutate: func(cfg *api.Config) {
				cfg.WebSearch.Endpoint = "https://example.com/search"
				cfg.WebSearch.Timeout = 0
			},
			wantErr: "web_search.timeout must be positive",
		},
		{
			name: "disabled mcp server is ok",
			mutate: func(cfg *api.Config) {
				cfg.MCPServers["x"] = api.MCPServerConfig{Enabled: false}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.mutate(cfg)
			err := Validate(cfg)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
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

func TestSetFlag(t *testing.T) {
	clearConfigEnv(t)
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Chdir(tmpDir)

	loader := NewLoader()
	loader.SetFlag("llm.model", "flag-model")
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.LLM.Model != "flag-model" {
		t.Errorf("expected model \"flag-model\", got %q", cfg.LLM.Model)
	}
}

func TestLoad_UnreadableConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(`[llm]
provider = "moonshot"
api_key = "key"
model = "kimi-k2.5"
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(configPath, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(configPath, 0644) })

	loader := NewLoader()
	loader.SetConfigFile(configPath)
	if _, err := loader.Load(); err == nil {
		t.Fatal("expected error for unreadable config file")
	}
}

func TestLoad_UnmarshalError(t *testing.T) {
	clearConfigEnv(t)
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Chdir(tmpDir)

	loader := NewLoader()
	loader.SetFlag("llm.timeout", "not-a-duration")
	_, err := loader.Load()
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
	if !strings.Contains(err.Error(), "unmarshal") {
		t.Errorf("expected unmarshal error, got: %v", err)
	}
}

func TestLoad_EnvOverride(t *testing.T) {
	clearConfigEnv(t)
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	configPath := filepath.Join(tmpDir, "config.toml")
	content := `[llm]
provider = "moonshot"
api_key = "key"
model = "from-file"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KIMI_LLM_MODEL", "from-env")

	loader := NewLoader()
	loader.SetConfigFile(configPath)
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.LLM.Model != "from-env" {
		t.Errorf("expected env override \"from-env\", got %q", cfg.LLM.Model)
	}
}

func TestLoad_ProviderEnvAndHeaders(t *testing.T) {
	clearConfigEnv(t)
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("OPENAI_API_KEY", "resolved-key")
	t.Setenv("PROVIDER_ENV", "resolved-env")
	t.Setenv("HEADER_VAL", "resolved-header")

	configPath := filepath.Join(tmpDir, "config.toml")
	content := `default_provider = "openai"

[llm]
provider = "moonshot"
api_key = "key"
model = "kimi-k2.5"

[providers.openai]
type = "openai"
base_url = "https://api.openai.com/v1"
api_key = "$OPENAI_API_KEY"
default_model = "gpt-4o"
[providers.openai.env]
FOO = "$PROVIDER_ENV"
[providers.openai.custom_headers]
X-Key = "$HEADER_VAL"
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
	p, ok := cfg.Providers["openai"]
	if !ok {
		t.Fatal("expected providers.openai")
	}
	if p.APIKey != "resolved-key" {
		t.Errorf("expected api_key resolved, got %q", p.APIKey)
	}
	if p.Env["FOO"] != "resolved-env" {
		t.Errorf("expected env FOO resolved, got %q", p.Env["FOO"])
	}
	if p.CustomHeaders["X-Key"] != "resolved-header" {
		t.Errorf("expected header X-Key resolved, got %q", p.CustomHeaders["X-Key"])
	}
}

func TestLoad_WebSearchAPIKey(t *testing.T) {
	clearConfigEnv(t)
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("WEB_SEARCH_API_KEY", "search-key")

	configPath := filepath.Join(tmpDir, "config.toml")
	content := `[llm]
provider = "moonshot"
api_key = "key"
model = "kimi-k2.5"

[web_search]
endpoint = "https://search.example.com"
api_key = "$WEB_SEARCH_API_KEY"
timeout = "10s"
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
	if cfg.WebSearch.APIKey != "search-key" {
		t.Errorf("expected web search api key resolved, got %q", cfg.WebSearch.APIKey)
	}
}

func TestLoad_FallbackAPIKey(t *testing.T) {
	clearConfigEnv(t)
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("FALLBACK_KEY", "fallback-resolved")

	configPath := filepath.Join(tmpDir, "config.toml")
	content := `[llm]
provider = "moonshot"
api_key = "key"
model = "kimi-k2.5"

[llm.fallback]
provider = "moonshot"
api_key = "$FALLBACK_KEY"
model = "kimi-fallback"
timeout = "30s"
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
	if cfg.LLM.Fallback == nil {
		t.Fatal("expected fallback")
	}
	if cfg.LLM.Fallback.APIKey != "fallback-resolved" {
		t.Errorf("expected fallback api key resolved, got %q", cfg.LLM.Fallback.APIKey)
	}
}

func TestEnsureConfigDir(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	dir, err := EnsureConfigDir()
	if err != nil {
		t.Fatalf("EnsureConfigDir() error: %v", err)
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("UserConfigDir() error: %v", err)
	}
	want := filepath.Join(configDir, "kimi-lite")
	if dir != want {
		t.Errorf("EnsureConfigDir() = %q, want %q", dir, want)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("expected dir to exist: %v", err)
	}
}

func TestEnsureConfigDirAt_Error(t *testing.T) {
	tmpDir := t.TempDir()
	root := filepath.Join(tmpDir, "not-a-dir")
	if err := os.WriteFile(root, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := EnsureConfigDirAt(root); err == nil {
		t.Fatal("expected error when root is not a directory")
	}
}

func TestWriteDefaultConfig_UsesConfigDir(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	if err := WriteDefaultConfig(); err != nil {
		t.Fatalf("WriteDefaultConfig() error: %v", err)
	}

	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("UserConfigDir() error: %v", err)
	}
	configPath := filepath.Join(configDir, "kimi-lite", "config.toml")
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat config file: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("config file permissions = %o, want %o", info.Mode().Perm(), 0600)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "[llm]") {
		t.Error("expected config to contain [llm] section")
	}

	// Idempotent second call should succeed.
	if err := WriteDefaultConfig(); err != nil {
		t.Fatalf("WriteDefaultConfig() second call error: %v", err)
	}
}

func TestWriteDefaultConfigTo_Error(t *testing.T) {
	tmpDir := t.TempDir()
	root := filepath.Join(tmpDir, "not-a-dir")
	if err := os.WriteFile(root, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := WriteDefaultConfigTo(root); err == nil {
		t.Fatal("expected error when target is not a directory")
	}
}

func TestExpandPath_HomeError(t *testing.T) {
	t.Setenv("HOME", "")

	if got := expandPath("~"); got != "~" {
		t.Errorf("expandPath(\"~\") = %q, want \"~\"", got)
	}
	if got := expandPath("~/x"); got != "~/x" {
		t.Errorf("expandPath(\"~/x\") = %q, want \"~/x\"", got)
	}
}
