// Package subagents implements ephemeral, non-persistent subagent runners.
package subagents

import (
	"fmt"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// agentConfig describes the system prompt and allowed tools for a subagent.
type agentConfig struct {
	systemPrompt string
	tools        []string
}

// agentConfigFor returns the configuration for a built-in subagent type.
func agentConfigFor(t api.SubagentType) (agentConfig, error) {
	switch t {
	case api.SubagentCoder:
		return agentConfig{systemPrompt: coderPrompt, tools: coderTools}, nil
	case api.SubagentExplore:
		return agentConfig{systemPrompt: explorePrompt, tools: exploreTools}, nil
	case api.SubagentPlan:
		return agentConfig{systemPrompt: planPrompt, tools: planTools}, nil
	default:
		return agentConfig{}, fmt.Errorf("unknown subagent type %q", t)
	}
}

var coderTools = []string{
	"read_file",
	"write_file",
	"str_replace_file",
	"edit",
	"shell",
	"glob",
	"grep",
	"list_directory",
}

var exploreTools = []string{
	"read_file",
	"glob",
	"grep",
	"list_directory",
}

var planTools = []string{
	"read_file",
	"glob",
	"grep",
	"list_directory",
}

const (
	coderPrompt = `You are a focused coding subagent. You have access to file and shell tools.
Work in the provided directory. Make minimal, correct changes. Return a concise summary of what you changed.`

	explorePrompt = `You are a read-only exploration subagent. Investigate the codebase and answer the question.
Do not modify files or run shell commands.`

	planPrompt = `You are a planning subagent. Read the relevant code and produce a step-by-step implementation plan.
Do not write code; only produce the plan.`
)
