// Package hooks implements lifecycle hook execution for the core agent.
package hooks

import (
	"os"
	"strings"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// allowedEnvKeys is the curated allowlist of environment variables passed to
// hook commands by default. Sensitive variables are excluded.
var allowedEnvKeys = map[string]bool{
	"PATH": true, "HOME": true, "USER": true, "LOGNAME": true,
	"SHELL": true, "LANG": true, "TERM": true, "TERM_PROGRAM": true,
	"TERM_PROGRAM_VERSION": true, "TMPDIR": true, "PWD": true, "OLDPWD": true,
	"XDG_CONFIG_HOME": true, "XDG_DATA_HOME": true, "XDG_CACHE_HOME": true,
	"XDG_SESSION_TYPE": true, "XDG_CURRENT_DESKTOP": true,
	"EDITOR": true, "VISUAL": true, "PAGER": true,
	"GOROOT": true, "GOPATH": true, "GOPROXY": true, "GOSUMDB": true,
	"GOVERSION": true, "CGO_ENABLED": true, "NO_COLOR": true,
}

// isAllowedEnvKey reports whether a variable is in the curated allowlist.
// Locale variables matching LC_* are also allowed.
func isAllowedEnvKey(key string) bool {
	upper := strings.ToUpper(key)
	if allowedEnvKeys[upper] {
		return true
	}
	return strings.HasPrefix(upper, "LC_")
}

// curatedEnv returns a copy of the current process environment containing only
// variables from the curated allowlist.
func curatedEnv() []string {
	env := os.Environ()
	filtered := make([]string, 0, len(env))
	for _, e := range env {
		key, _, _ := strings.Cut(e, "=")
		if isAllowedEnvKey(key) {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

// buildEnv constructs the process environment for a hook command.
// It starts with a curated subset of the current process environment, adds
// hook-specific variables prefixed with KIMI_HOOK_, and finally merges any
// extra variables from the hook configuration (which may override defaults).
func buildEnv(extra map[string]string, data api.HookData) []string {
	env := curatedEnv()
	env = append(env,
		"KIMI_HOOK_EVENT="+string(data.Event),
		"KIMI_HOOK_SESSION_ID="+data.SessionID,
		"KIMI_HOOK_TURN_ID="+data.TurnID,
		"KIMI_HOOK_TOOL_NAME="+data.ToolName,
		"KIMI_HOOK_TOOL_ARGS="+data.ToolArgs,
		"KIMI_HOOK_TOOL_RESULT="+data.ToolResult,
		"KIMI_HOOK_DECISION="+data.Decision,
		"KIMI_HOOK_ERROR="+data.Error,
	)
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}
