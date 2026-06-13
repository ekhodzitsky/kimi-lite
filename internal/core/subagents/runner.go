package subagents

import (
	"context"
	"fmt"
	"time"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// Runner implements api.SubagentRunner with an ephemeral LLM↔tool loop.
type Runner struct {
	llmClient    api.LLMClient
	toolExecutor api.ToolExecutor
	sandboxRoot  string
}

// NewRunner creates a new subagent runner.
// If sandboxRoot is empty it defaults to the current working directory.
func NewRunner(llmClient api.LLMClient, toolExecutor api.ToolExecutor, sandboxRoot string) *Runner {
	if sandboxRoot == "" {
		sandboxRoot = "."
	}
	return &Runner{
		llmClient:    llmClient,
		toolExecutor: toolExecutor,
		sandboxRoot:  sandboxRoot,
	}
}

// Run executes a subagent request.
func (r *Runner) Run(ctx context.Context, req api.SubagentRequest) (*api.SubagentResult, error) {
	if req.Type == "" {
		return nil, fmt.Errorf("subagent type is required")
	}
	if req.Prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}
	cfg, err := agentConfigFor(req.Type)
	if err != nil {
		return nil, fmt.Errorf("subagent config: %w", err)
	}
	return runLoop(ctx, r, cfg, req)
}

// now is a test seam for time-sensitive assertions.
var now = time.Now
