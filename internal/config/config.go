// Package config provides configuration types and loading for kimi-lite.
package config

import (
	"fmt"
	"net/url"
	"time"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

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
				"list_directory",
				"grep",
				"glob",
				"fetch_url",
			},
			ShellTimeout: 30 * time.Second,
			MaxTurns:     50,
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
			Editor:         "vim",
		},
		Keybindings: api.KeybindingConfig{
			Send:     "enter",
			Newline:  "alt+enter",
			Cancel:   "esc",
			Quit:     "ctrl+c",
			PlanMode: "shift+tab",
			Yolo:     "ctrl+y",
		},
	}
}

// Validate checks that the configuration is valid.
func Validate(cfg *api.Config) error {
	if cfg.LLM.Timeout <= 0 {
		return fmt.Errorf("llm.timeout must be positive")
	}
	if cfg.Behavior.ShellTimeout <= 0 {
		return fmt.Errorf("behavior.shell_timeout must be positive")
	}
	if cfg.LLM.Model == "" {
		return fmt.Errorf("llm.model must not be empty")
	}
	if cfg.LLM.BaseURL != "" {
		u, err := url.Parse(cfg.LLM.BaseURL)
		if err != nil || u.Scheme == "" {
			return fmt.Errorf("llm.base_url must be a valid URL with scheme")
		}
	}
	if cfg.Session.DBPath == "" {
		return fmt.Errorf("session.db_path must not be empty")
	}
	if cfg.Behavior.MaxTurns < 0 {
		return fmt.Errorf("behavior.max_turns must be non-negative")
	}
	if cfg.Session.MaxHistory < 0 {
		return fmt.Errorf("session.max_history must be non-negative")
	}
	if cfg.UI.Theme == "" {
		return fmt.Errorf("ui.theme must not be empty")
	}
	return nil
}
