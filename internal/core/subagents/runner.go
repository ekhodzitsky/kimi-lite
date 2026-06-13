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
//
// The Runner's sandbox root is established at construction time. If the
// request supplies a non-empty SandboxRoot it must match the runner's root;
// otherwise an error is returned.
func (r *Runner) Run(ctx context.Context, req api.SubagentRequest) (*api.SubagentResult, error) {
	if req.Type == "" {
		return nil, fmt.Errorf("subagent type is required")
	}
	if req.Prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}
	if r.llmClient == nil {
		return nil, fmt.Errorf("llm client is nil")
	}
	if r.toolExecutor == nil {
		return nil, fmt.Errorf("tool executor is nil")
	}
	if req.SandboxRoot != "" && req.SandboxRoot != r.sandboxRoot {
		return nil, fmt.Errorf("mismatched sandbox root: request %q, runner %q", req.SandboxRoot, r.sandboxRoot)
	}
	cfg, err := agentConfigFor(req.Type)
	if err != nil {
		return nil, fmt.Errorf("subagent config: %w", err)
	}
	return runLoop(ctx, r, cfg, req)
}

// now is a test seam for time-sensitive assertions.
var now = time.Now
