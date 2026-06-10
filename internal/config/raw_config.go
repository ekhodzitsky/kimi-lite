// Package config provides configuration loading from files, environment, and flags.
package config

import (
	"time"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// RawConfig holds the raw configuration with mapstructure tags for viper unmarshaling.
type RawConfig struct {
	LLM         RawLLMConfig        `mapstructure:"llm"`
	Behavior    RawBehaviorConfig   `mapstructure:"behavior"`
	Session     RawSessionConfig    `mapstructure:"session"`
	MCP         RawMCPConfig        `mapstructure:"mcp"`
	UI          RawUIConfig         `mapstructure:"ui"`
	Keybindings RawKeybindingConfig `mapstructure:"keybindings"`
}

// RawLLMConfig holds raw LLM provider configuration.
type RawLLMConfig struct {
	Provider string        `mapstructure:"provider"`
	APIKey   string        `mapstructure:"api_key"`
	Model    string        `mapstructure:"model"`
	BaseURL  string        `mapstructure:"base_url"`
	Timeout  time.Duration `mapstructure:"timeout"`
	Fallback *RawLLMConfig `mapstructure:"fallback"`
}

// RawBehaviorConfig holds raw behavior settings.
type RawBehaviorConfig struct {
	AutoApprove  []string      `mapstructure:"auto_approve"`
	ShellTimeout time.Duration `mapstructure:"shell_timeout"`
	MaxTurns     int           `mapstructure:"max_turns"`
}

// RawSessionConfig holds raw session persistence settings.
type RawSessionConfig struct {
	DBPath     string `mapstructure:"db_path"`
	MaxHistory int    `mapstructure:"max_history"`
}

// RawMCPConfig holds raw MCP integration settings.
type RawMCPConfig struct {
	GuardCommand string `mapstructure:"guard_command"`
	GuardConfig  string `mapstructure:"guard_config"`
}

// RawUIConfig holds raw UI settings.
type RawUIConfig struct {
	Theme          string `mapstructure:"theme"`
	ShowTokenCount bool   `mapstructure:"show_token_count"`
	Editor         string `mapstructure:"editor"`
}

// RawKeybindingConfig holds raw keybinding settings.
type RawKeybindingConfig struct {
	Send     string `mapstructure:"send"`
	Newline  string `mapstructure:"newline"`
	Cancel   string `mapstructure:"cancel"`
	Quit     string `mapstructure:"quit"`
	PlanMode string `mapstructure:"plan_mode"`
	Yolo     string `mapstructure:"yolo"`
}

// mapRawToAPI converts a RawConfig to an api.Config.
func mapRawToAPI(raw RawConfig) api.Config {
	return api.Config{
		LLM:         mapRawLLM(raw.LLM),
		Behavior:    mapRawBehavior(raw.Behavior),
		Session:     mapRawSession(raw.Session),
		MCP:         mapRawMCP(raw.MCP),
		UI:          mapRawUI(raw.UI),
		Keybindings: mapRawKeybindings(raw.Keybindings),
	}
}

func mapRawLLM(raw RawLLMConfig) api.LLMConfig {
	cfg := api.LLMConfig{
		Provider: raw.Provider,
		APIKey:   raw.APIKey,
		Model:    raw.Model,
		BaseURL:  raw.BaseURL,
		Timeout:  raw.Timeout,
	}
	if raw.Fallback != nil {
		fallback := mapRawLLM(*raw.Fallback)
		cfg.Fallback = &fallback
	}
	return cfg
}

func mapRawBehavior(raw RawBehaviorConfig) api.BehaviorConfig {
	return api.BehaviorConfig{
		AutoApprove:  raw.AutoApprove,
		ShellTimeout: raw.ShellTimeout,
		MaxTurns:     raw.MaxTurns,
	}
}

func mapRawSession(raw RawSessionConfig) api.SessionConfig {
	return api.SessionConfig{
		DBPath:     raw.DBPath,
		MaxHistory: raw.MaxHistory,
	}
}

func mapRawMCP(raw RawMCPConfig) api.MCPConfig {
	return api.MCPConfig{
		GuardCommand: raw.GuardCommand,
		GuardConfig:  raw.GuardConfig,
	}
}

func mapRawUI(raw RawUIConfig) api.UIConfig {
	return api.UIConfig{
		Theme:          raw.Theme,
		ShowTokenCount: raw.ShowTokenCount,
		Editor:         raw.Editor,
	}
}

func mapRawKeybindings(raw RawKeybindingConfig) api.KeybindingConfig {
	return api.KeybindingConfig{
		Send:     raw.Send,
		Newline:  raw.Newline,
		Cancel:   raw.Cancel,
		Quit:     raw.Quit,
		PlanMode: raw.PlanMode,
		Yolo:     raw.Yolo,
	}
}
