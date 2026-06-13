// Package config provides configuration types and loading for kimi-lite.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func validateURL(prefix, value string, allowEmpty bool) error {
	if value == "" {
		if allowEmpty {
			return nil
		}
		return fmt.Errorf("%s must not be empty", prefix)
	}
	u, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("%s must be a valid URL, got %q: %w", prefix, value, err)
	}
	if u.Host == "" {
		return fmt.Errorf("%s must be a valid URL with a host, got %q", prefix, value)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%s must be an http(s) URL, got %q", prefix, value)
	}
	if u.Scheme == "http" {
		host := u.Hostname()
		if host != "localhost" && host != "127.0.0.1" && host != "::1" {
			return fmt.Errorf("%s must use https (or explicit localhost opt-in), got %q", prefix, value)
		}
	}
	return nil
}

func validateLLM(prefix string, c api.LLMConfig) error {
	if c.Timeout <= 0 {
		return fmt.Errorf("%s.timeout must be positive", prefix)
	}
	if c.Model == "" {
		return fmt.Errorf("%s.model must not be empty", prefix)
	}
	if c.BaseURL != "" {
		if err := validateURL(prefix+".base_url", c.BaseURL, false); err != nil {
			return err
		}
	}
	return nil
}

func validateMCPServer(name string, c api.MCPServerConfig) error {
	if !c.Enabled {
		return nil
	}
	switch c.Transport {
	case api.MCPTransportStdio, api.MCPTransportHTTP:
		// ok
	default:
		return fmt.Errorf("mcp_servers.%s.transport must be %q or %q, got %q", name, api.MCPTransportStdio, api.MCPTransportHTTP, c.Transport)
	}
	if c.Transport == api.MCPTransportStdio && c.Command == "" {
		return fmt.Errorf("mcp_servers.%s.command must not be empty for stdio transport", name)
	}
	if c.Transport == api.MCPTransportHTTP {
		if c.URL == "" {
			return fmt.Errorf("mcp_servers.%s.url must not be empty for http transport", name)
		}
		if err := validateURL(fmt.Sprintf("mcp_servers.%s.url", name), c.URL, false); err != nil {
			return err
		}
	}
	if c.StartupTimeoutMs < 0 {
		return fmt.Errorf("mcp_servers.%s.startup_timeout_ms must not be negative", name)
	}
	if c.ToolTimeoutMs < 0 {
		return fmt.Errorf("mcp_servers.%s.tool_timeout_ms must not be negative", name)
	}
	return nil
}

// DefaultConfig returns the default configuration.
func DefaultConfig() *api.Config {
	return &api.Config{
		LLM: api.LLMConfig{
			Provider: "moonshot",
			APIKey:   "$MOONSHOT_API_KEY",
			Model:    "kimi-k2.5",
			BaseURL:  "https://api.moonshot.cn/v1",
			Timeout:  60 * time.Second,
		},
		Behavior: api.BehaviorConfig{
			AutoApprove: []string{
				"read_file",
				"grep",
				"glob",
				"fetch_url",
				"list_directory",
				"web_search",
			},
			ShellTimeout:      30 * time.Second,
			MaxTurns:          50,
			MaxToolRounds:     10,
			AllowShell:        true,
			CompactKeepRecent: 2,
			PassEnv:           false,
		},
		Permission: api.PermissionConfig{
			Rules: []api.PermissionRule{},
		},
		Session: api.SessionConfig{
			DBPath:     "~/.local/share/kimi-lite/sessions.db",
			MaxHistory: 100,
		},
		MCP: api.MCPConfig{
			GuardCommand: "mcp-guard",
			GuardConfig:  "~/.config/mcp-guard/mcp-guard.toml",
		},
		MCPServers: map[string]api.MCPServerConfig{},
		WebSearch: api.WebSearchConfig{
			Timeout: 30 * time.Second,
		},
		UI: api.UIConfig{
			Theme:          "dark",
			ShowTokenCount: true,
			Editor:         "",
		},
		Keybindings: api.KeybindingConfig{
			Send:           "enter",
			Newline:        "alt+enter",
			Cancel:         "esc",
			Quit:           "ctrl+c",
			Yolo:           "ctrl+y",
			ToggleSidebar:  "ctrl+b",
			FocusNext:      "tab",
			FocusPrev:      "shift+tab",
			ApproveYes:     "y",
			ApproveNo:      "n",
			ApproveAlways:  "a",
			ExternalEditor: "ctrl+g",
		},
	}
}

// Validate checks that the configuration is valid.
func Validate(cfg *api.Config) error {
	var errs []error
	if err := validateLLM("llm", cfg.LLM); err != nil {
		errs = append(errs, err)
	}
	if cfg.LLM.Fallback != nil {
		if err := validateLLM("llm.fallback", *cfg.LLM.Fallback); err != nil {
			errs = append(errs, err)
		}
	}
	if cfg.Behavior.ShellTimeout <= 0 {
		errs = append(errs, fmt.Errorf("behavior.shell_timeout must be positive"))
	}
	if cfg.Session.DBPath == "" {
		errs = append(errs, fmt.Errorf("session.db_path must not be empty"))
	}
	if cfg.UI.Theme == "" {
		errs = append(errs, fmt.Errorf("ui.theme must not be empty"))
	}
	if cfg.WebSearch.Endpoint != "" {
		if err := validateURL("web_search.endpoint", cfg.WebSearch.Endpoint, false); err != nil {
			errs = append(errs, err)
		}
		if cfg.WebSearch.Timeout <= 0 {
			errs = append(errs, fmt.Errorf("web_search.timeout must be positive"))
		}
	}
	for i, r := range cfg.Permission.Rules {
		prefix := fmt.Sprintf("permission.rules[%d]", i)
		if r.Tool == "" {
			errs = append(errs, fmt.Errorf("%s.tool must not be empty", prefix))
		}
		switch r.Decision {
		case api.PermissionAllow, api.PermissionDeny, api.PermissionAsk:
			// ok
		default:
			errs = append(errs, fmt.Errorf("%s.decision must be one of allow, deny, ask, got %q", prefix, r.Decision))
		}
		switch r.Scope {
		case api.PermissionScopeUser, api.PermissionScopeSession, api.PermissionScopeTurn:
			// ok
		default:
			errs = append(errs, fmt.Errorf("%s.scope must be one of user, session, turn, got %q", prefix, r.Scope))
		}
	}
	for name, srv := range cfg.MCPServers {
		if err := validateMCPServer(name, srv); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
