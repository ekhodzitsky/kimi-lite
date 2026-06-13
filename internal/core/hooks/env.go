// Package hooks implements lifecycle hook execution for the core agent.
package hooks

import (
	"os"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// buildEnv constructs the process environment for a hook command.
// It starts with the current process environment, adds hook-specific
// variables prefixed with KIMI_HOOK_, and finally merges any extra
// variables from the hook configuration (which may override defaults).
func buildEnv(extra map[string]string, data api.HookData) []string {
	env := os.Environ()
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
