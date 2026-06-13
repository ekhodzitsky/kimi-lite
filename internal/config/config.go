// Package config provides configuration types and loading for kimi-lite.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func validateLLM(prefix string, c api.LLMConfig) error {
	if c.Timeout <= 0 {
		return fmt.Errorf("%s.timeout must be positive", prefix)
	}
	if c.Model == "" {
		return fmt.Errorf("%s.model must not be empty", prefix)
	}
	if c.BaseURL != "" {
		u, err := url.Parse(c.BaseURL)
		if err != nil {
			return fmt.Errorf("%s.base_url must be a valid URL, got %q: %w", prefix, c.BaseURL, err)
		}
		if u.Host == "" {
			return fmt.Errorf("%s.base_url must be a valid URL with a host, got %q", prefix, c.BaseURL)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf("%s.base_url must be an http(s) URL with a host, got %q", prefix, c.BaseURL)
		}
		if u.Scheme == "http" {
			host := u.Hostname()
			if host != "localhost" && host != "127.0.0.1" && host != "::1" {
				return fmt.Errorf("%s.base_url must use https (or explicit localhost opt-in), got %q", prefix, c.BaseURL)
			}
		}
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
		UI: api.UIConfig{
			Theme:          "dark",
			ShowTokenCount: true,
		},
		Keybindings: api.KeybindingConfig{
			Send:          "enter",
			Newline:       "alt+enter",
			Cancel:        "esc",
			Quit:          "ctrl+c",
			Yolo:          "ctrl+y",
			ToggleSidebar: "ctrl+b",
			FocusNext:     "tab",
			FocusPrev:     "shift+tab",
			ApproveYes:    "y",
			ApproveNo:     "n",
			ApproveAlways: "a",
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
	return errors.Join(errs...)
}
