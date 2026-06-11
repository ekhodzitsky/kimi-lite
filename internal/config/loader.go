// Package config provides configuration loading from files, environment, and flags.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// Loader handles configuration loading from multiple sources.
type Loader struct {
	v *viper.Viper
}

// NewLoader creates a new configuration loader.
func NewLoader() *Loader {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("toml")

	// Set default paths
	configDir, err := os.UserConfigDir()
	if err == nil {
		v.AddConfigPath(filepath.Join(configDir, "kimi-lite"))
	}
	v.AddConfigPath(".")

	// Set defaults
	defaults := DefaultConfig()
	v.SetDefault("llm.provider", defaults.LLM.Provider)
	v.SetDefault("llm.api_key", defaults.LLM.APIKey)
	v.SetDefault("llm.model", defaults.LLM.Model)
	v.SetDefault("llm.base_url", defaults.LLM.BaseURL)
	v.SetDefault("llm.timeout", defaults.LLM.Timeout)
	v.SetDefault("behavior.auto_approve", defaults.Behavior.AutoApprove)
	v.SetDefault("behavior.shell_timeout", defaults.Behavior.ShellTimeout)
	v.SetDefault("behavior.max_turns", defaults.Behavior.MaxTurns)
	v.SetDefault("session.db_path", defaults.Session.DBPath)
	v.SetDefault("session.max_history", defaults.Session.MaxHistory)
	v.SetDefault("mcp.guard_command", defaults.MCP.GuardCommand)
	v.SetDefault("mcp.guard_config", defaults.MCP.GuardConfig)
	v.SetDefault("ui.theme", defaults.UI.Theme)
	v.SetDefault("ui.show_token_count", defaults.UI.ShowTokenCount)
	v.SetDefault("ui.editor", defaults.UI.Editor)
	v.SetDefault("keybindings.send", defaults.Keybindings.Send)
	v.SetDefault("keybindings.newline", defaults.Keybindings.Newline)
	v.SetDefault("keybindings.cancel", defaults.Keybindings.Cancel)
	v.SetDefault("keybindings.quit", defaults.Keybindings.Quit)
	v.SetDefault("keybindings.plan_mode", defaults.Keybindings.PlanMode)
	v.SetDefault("keybindings.yolo", defaults.Keybindings.Yolo)

	return &Loader{v: v}
}

// Load reads configuration from file, environment, and flags.
func (l *Loader) Load() (*api.Config, error) {
	if err := l.v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("read config: %w", err)
		}
		// Config file not found is okay, use defaults + env
	}

	// Bind environment variables
	l.v.SetEnvPrefix("KIMI")
	l.v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	l.v.AutomaticEnv()

	var raw RawConfig
	if err := l.v.Unmarshal(&raw); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	cfg := mapRawToAPI(raw)

	// Resolve environment variables in API keys
	cfg.LLM.APIKey = resolveEnvVar(cfg.LLM.APIKey)
	if cfg.LLM.Fallback != nil {
		cfg.LLM.Fallback.APIKey = resolveEnvVar(cfg.LLM.Fallback.APIKey)
	}

	// Expand paths
	cfg.Session.DBPath = expandPath(cfg.Session.DBPath)
	cfg.MCP.GuardConfig = expandPath(cfg.MCP.GuardConfig)

	if err := Validate(&cfg); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return &cfg, nil
}

// SetFlag allows CLI flags to override config values.
func (l *Loader) SetFlag(key string, value interface{}) {
	l.v.Set(key, value)
}

// SetConfigFile sets an explicit config file path.
func (l *Loader) SetConfigFile(path string) {
	l.v.SetConfigFile(path)
}

// resolveEnvVar expands $VAR or ${VAR} patterns.
func resolveEnvVar(s string) string {
	if strings.HasPrefix(s, "$") {
		name := strings.TrimPrefix(s, "$")
		name = strings.Trim(name, "{}")
		if val, ok := os.LookupEnv(name); ok {
			return val
		}
	}
	return s
}

// expandPath expands ~ to home directory.
func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// EnsureConfigDir creates the config directory if it doesn't exist.
func EnsureConfigDir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("get config dir: %w", err)
	}
	dir := filepath.Join(configDir, "kimi-lite")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create config dir: %w", err)
	}
	return dir, nil
}

// WriteDefaultConfig writes the default config to the config directory.
func WriteDefaultConfig() error {
	dir, err := EnsureConfigDir()
	if err != nil {
		return err
	}

	path := filepath.Join(dir, "config.toml")
	if _, err := os.Stat(path); err == nil {
		return nil // Already exists
	}

	content := `# kimi-lite configuration
[llm]
provider = "moonshot"
api_key = "$MOONSHOT_API_KEY"
model = "kimi-k2.5"
base_url = "https://api.moonshot.cn/v1"
timeout = "60s"

[behavior]
auto_approve = ["read_file", "list_directory", "grep", "glob", "fetch_url"]
shell_timeout = "30s"
max_turns = 50

[session]
db_path = "~/.local/share/kimi-lite/sessions.db"
max_history = 100

[mcp]
guard_command = "mcp-guard"
guard_config = "~/.config/mcp-guard/mcp-guard.toml"

[ui]
theme = "dark"
show_token_count = true
editor = "vim"

[keybindings]
send = "enter"
newline = "alt+enter"
cancel = "esc"
quit = "ctrl+c"
plan_mode = "shift+tab"
yolo = "ctrl+y"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("write default config: %w", err)
	}
	return nil
}
