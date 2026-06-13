package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// TestDecodePopulatesEveryConfigField verifies that a representative TOML
// exercising every api.Config field decodes without silent zero values.
// It guards against the drift class where a new field is added to api.Config
// but not mapped through the RawConfig decode path.
func TestDecodePopulatesEveryConfigField(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	content := `
[llm]
provider = "openai"
api_key = "test-api-key"
model = "gpt-test"
base_url = "https://api.openai.com/v1"
timeout = "120s"

[llm.fallback]
provider = "moonshot"
api_key = "fallback-key"
model = "kimi-fallback"
base_url = "https://api.moonshot.cn/v1"
timeout = "90s"

[behavior]
auto_approve = ["read_file", "grep"]
shell_timeout = "60s"
max_turns = 10
max_tool_rounds = 5
allow_shell = true
pass_env = true
compact_keep_recent = 7

[session]
db_path = "/tmp/test.db"
max_history = 50

[mcp]
guard_command = "mcp-guard"
guard_config = "/tmp/mcp.toml"

[mcp_servers.test]
transport = "stdio"
command = "npx"
args = ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
enabled = true
startup_timeout_ms = 10000
tool_timeout_ms = 30000
enabled_tools = ["read_file"]
disabled_tools = ["write_file"]

[web_search]
endpoint = "https://search.example.com"
api_key = "search-key"
timeout = "25s"

[ui]
theme = "light"
show_token_count = false

[keybindings]
send = "enter"
newline = "alt+enter"
cancel = "esc"
quit = "ctrl+c"
yolo = "ctrl+y"
toggle_sidebar = "ctrl+b"
focus_next = "tab"
focus_prev = "shift+tab"
approve_yes = "y"
approve_no = "n"
approve_always = "a"
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

	assertNotEmpty := func(name, got string) {
		t.Helper()
		if got == "" {
			t.Errorf("expected %s to be non-empty", name)
		}
	}
	assertTrue := func(name string, got bool) {
		t.Helper()
		if !got {
			t.Errorf("expected %s to be true", name)
		}
	}
	assertPositive := func(name string, got int) {
		t.Helper()
		if got <= 0 {
			t.Errorf("expected %s to be positive, got %d", name, got)
		}
	}
	assertPositiveDuration := func(name string, got time.Duration) {
		t.Helper()
		if got <= 0 {
			t.Errorf("expected %s to be positive, got %v", name, got)
		}
	}

	// LLM
	assertNotEmpty("llm.provider", cfg.LLM.Provider)
	assertNotEmpty("llm.api_key", cfg.LLM.APIKey)
	assertNotEmpty("llm.model", cfg.LLM.Model)
	assertNotEmpty("llm.base_url", cfg.LLM.BaseURL)
	assertPositiveDuration("llm.timeout", cfg.LLM.Timeout)

	// Fallback
	if cfg.LLM.Fallback == nil {
		t.Fatal("expected llm.fallback to be populated")
	}
	assertNotEmpty("llm.fallback.provider", cfg.LLM.Fallback.Provider)
	assertNotEmpty("llm.fallback.api_key", cfg.LLM.Fallback.APIKey)
	assertNotEmpty("llm.fallback.model", cfg.LLM.Fallback.Model)
	assertNotEmpty("llm.fallback.base_url", cfg.LLM.Fallback.BaseURL)
	assertPositiveDuration("llm.fallback.timeout", cfg.LLM.Fallback.Timeout)

	// Behavior
	if len(cfg.Behavior.AutoApprove) == 0 {
		t.Error("expected behavior.auto_approve to be non-empty")
	}
	assertPositiveDuration("behavior.shell_timeout", cfg.Behavior.ShellTimeout)
	assertPositive("behavior.max_turns", cfg.Behavior.MaxTurns)
	assertPositive("behavior.max_tool_rounds", cfg.Behavior.MaxToolRounds)
	assertTrue("behavior.allow_shell", cfg.Behavior.AllowShell)
	assertTrue("behavior.pass_env", cfg.Behavior.PassEnv)
	assertPositive("behavior.compact_keep_recent", cfg.Behavior.CompactKeepRecent)

	// Session
	assertNotEmpty("session.db_path", cfg.Session.DBPath)
	assertPositive("session.max_history", cfg.Session.MaxHistory)

	// MCP
	assertNotEmpty("mcp.guard_command", cfg.MCP.GuardCommand)
	assertNotEmpty("mcp.guard_config", cfg.MCP.GuardConfig)

	// WebSearch
	assertNotEmpty("web_search.endpoint", cfg.WebSearch.Endpoint)
	assertNotEmpty("web_search.api_key", cfg.WebSearch.APIKey)
	assertPositiveDuration("web_search.timeout", cfg.WebSearch.Timeout)

	// UI
	assertNotEmpty("ui.theme", cfg.UI.Theme)
	// ShowTokenCount is intentionally false in the TOML; assert it decoded correctly.
	if cfg.UI.ShowTokenCount {
		t.Error("expected ui.show_token_count to be false")
	}

	// MCPServers
	if len(cfg.MCPServers) == 0 {
		t.Fatal("expected mcp_servers to be populated")
	}
	srv, ok := cfg.MCPServers["test"]
	if !ok {
		t.Fatal("expected mcp_servers.test")
	}
	if srv.Transport != api.MCPTransportStdio {
		t.Errorf("expected mcp_servers.test.transport = stdio, got %q", srv.Transport)
	}
	if srv.Command != "npx" {
		t.Errorf("expected mcp_servers.test.command = npx, got %q", srv.Command)
	}
	if len(srv.Args) == 0 {
		t.Error("expected mcp_servers.test.args to be non-empty")
	}
	if !srv.Enabled {
		t.Error("expected mcp_servers.test.enabled to be true")
	}
	assertPositive("mcp_servers.test.startup_timeout_ms", srv.StartupTimeoutMs)
	assertPositive("mcp_servers.test.tool_timeout_ms", srv.ToolTimeoutMs)
	if len(srv.EnabledTools) == 0 {
		t.Error("expected mcp_servers.test.enabled_tools to be non-empty")
	}
	if len(srv.DisabledTools) == 0 {
		t.Error("expected mcp_servers.test.disabled_tools to be non-empty")
	}

	// Keybindings
	assertNotEmpty("keybindings.send", cfg.Keybindings.Send)
	assertNotEmpty("keybindings.newline", cfg.Keybindings.Newline)
	assertNotEmpty("keybindings.cancel", cfg.Keybindings.Cancel)
	assertNotEmpty("keybindings.quit", cfg.Keybindings.Quit)
	assertNotEmpty("keybindings.yolo", cfg.Keybindings.Yolo)
	assertNotEmpty("keybindings.toggle_sidebar", cfg.Keybindings.ToggleSidebar)
	assertNotEmpty("keybindings.focus_next", cfg.Keybindings.FocusNext)
	assertNotEmpty("keybindings.focus_prev", cfg.Keybindings.FocusPrev)
	assertNotEmpty("keybindings.approve_yes", cfg.Keybindings.ApproveYes)
	assertNotEmpty("keybindings.approve_no", cfg.Keybindings.ApproveNo)
	assertNotEmpty("keybindings.approve_always", cfg.Keybindings.ApproveAlways)
	assertNotEmpty("keybindings.approve_diff", cfg.Keybindings.ApproveDiff)

	// Spot-check exact values for fields that previously drifted out of the
	// RawConfig mirror.
	if cfg.Behavior.CompactKeepRecent != 7 {
		t.Errorf("expected behavior.compact_keep_recent = 7, got %d", cfg.Behavior.CompactKeepRecent)
	}
	if cfg.Keybindings.ToggleSidebar != "ctrl+b" {
		t.Errorf("expected keybindings.toggle_sidebar = ctrl+b, got %q", cfg.Keybindings.ToggleSidebar)
	}
	if cfg.Keybindings.ApproveYes != "y" {
		t.Errorf("expected keybindings.approve_yes = y, got %q", cfg.Keybindings.ApproveYes)
	}

	_ = api.Config{} // ensure the package compiles if api.Config ever changes
}
