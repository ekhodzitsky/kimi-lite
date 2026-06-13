// Package config provides configuration loading from files, environment, and flags.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/viper"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

var envVarRegex = regexp.MustCompile(`^\$\{([A-Za-z_][A-Za-z0-9_]*)\}$|^\$([A-Za-z_][A-Za-z0-9_]*)$`)

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
	v.SetDefault("behavior.max_tool_rounds", defaults.Behavior.MaxToolRounds)
	v.SetDefault("behavior.compact_keep_recent", defaults.Behavior.CompactKeepRecent)
	v.SetDefault("session.db_path", defaults.Session.DBPath)
	v.SetDefault("session.max_history", defaults.Session.MaxHistory)
	v.SetDefault("mcp.guard_command", defaults.MCP.GuardCommand)
	v.SetDefault("mcp.guard_config", defaults.MCP.GuardConfig)
	v.SetDefault("ui.theme", defaults.UI.Theme)
	v.SetDefault("ui.show_token_count", defaults.UI.ShowTokenCount)
	v.SetDefault("keybindings.send", defaults.Keybindings.Send)
	v.SetDefault("keybindings.newline", defaults.Keybindings.Newline)
	v.SetDefault("keybindings.cancel", defaults.Keybindings.Cancel)
	v.SetDefault("keybindings.quit", defaults.Keybindings.Quit)
	v.SetDefault("keybindings.yolo", defaults.Keybindings.Yolo)
	v.SetDefault("keybindings.toggle_sidebar", defaults.Keybindings.ToggleSidebar)
	v.SetDefault("keybindings.focus_next", defaults.Keybindings.FocusNext)
	v.SetDefault("keybindings.focus_prev", defaults.Keybindings.FocusPrev)
	v.SetDefault("keybindings.approve_yes", defaults.Keybindings.ApproveYes)
	v.SetDefault("keybindings.approve_no", defaults.Keybindings.ApproveNo)
	v.SetDefault("keybindings.approve_always", defaults.Keybindings.ApproveAlways)
	v.SetDefault("permission.rules", []api.PermissionRule{})

	return &Loader{v: v}
}

// Load reads configuration from file, environment, and flags.
func (l *Loader) Load() (*api.Config, error) {
	if err := l.v.ReadInConfig(); err != nil {
		var nf viper.ConfigFileNotFoundError
		if !errors.As(err, &nf) {
			return nil, fmt.Errorf("read config: %w", err)
		}
		// Config file not found is okay, use defaults + env
	}

	// Bind environment variables
	l.v.SetEnvPrefix("KIMI")
	l.v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	l.v.AutomaticEnv()

	var cfg api.Config
	if err := l.v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

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
func (l *Loader) SetFlag(key string, value any) {
	l.v.Set(key, value)
}

// SetConfigFile sets an explicit config file path.
func (l *Loader) SetConfigFile(path string) {
	l.v.SetConfigFile(path)
}

// resolveEnvVar expands $VAR or ${VAR} patterns using strict matching.
// Strings that do not match exactly are returned unchanged.
func resolveEnvVar(s string) string {
	matches := envVarRegex.FindStringSubmatch(s)
	if matches == nil {
		return s
	}
	name := matches[1]
	if name == "" {
		name = matches[2]
	}
	if val, ok := os.LookupEnv(name); ok {
		return val
	}
	return s
}

// expandPath expands ~ to home directory.
func expandPath(path string) string {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			return home
		}
		return path
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// Default returns the default configuration with environment variables
// and paths resolved.
func Default() (*api.Config, error) {
	cfg := DefaultConfig()
	cfg.LLM.APIKey = resolveEnvVar(cfg.LLM.APIKey)
	if cfg.LLM.Fallback != nil {
		cfg.LLM.Fallback.APIKey = resolveEnvVar(cfg.LLM.Fallback.APIKey)
	}
	cfg.Session.DBPath = expandPath(cfg.Session.DBPath)
	cfg.MCP.GuardConfig = expandPath(cfg.MCP.GuardConfig)
	return cfg, nil
}

// EnsureConfigDir creates the config directory if it doesn't exist.
func EnsureConfigDir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("get config dir: %w", err)
	}
	dir := filepath.Join(configDir, "kimi-lite")
	if err := os.MkdirAll(dir, 0700); err != nil {
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
auto_approve = ["read_file", "grep", "glob", "fetch_url", "list_directory"]
shell_timeout = "30s"
allow_shell = true
max_turns = 50
max_tool_rounds = 10
compact_keep_recent = 2

[permission]
# Permission rules override the default auto-approve behavior for read-only tools.
# decision can be "allow", "deny", or "ask". scope can be "user", "session", or "turn".
# [[permission.rules]]
# tool = "read_file"
# decision = "ask"
# scope = "user"

[session]
db_path = "~/.local/share/kimi-lite/sessions.db"
max_history = 100

[mcp]
guard_command = "mcp-guard"
guard_config = "~/.config/mcp-guard/mcp-guard.toml"

[ui]
theme = "dark"
show_token_count = true

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
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		return fmt.Errorf("write default config: %w", err)
	}
	return nil
}
