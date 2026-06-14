package subagents

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func TestRunner_Creates(t *testing.T) {
	t.Parallel()
	r := NewRunner(nil, nil, "")
	if r == nil {
		t.Fatal("NewRunner returned nil")
	}
}

func TestRunner_ExploreReturnsAnswer(t *testing.T) {
	t.Parallel()
	llm := &fakeLLM{responses: []string{"The answer is 42."}}
	tools := &fakeToolExecutor{}
	r := NewRunner(llm, tools, t.TempDir())
	ctx := context.Background()
	res, err := r.Run(ctx, api.SubagentRequest{Type: api.SubagentExplore, Prompt: "what?"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Output != "The answer is 42." {
		t.Fatalf("output = %q, want %q", res.Output, "The answer is 42.")
	}
}

func TestAgentConfig_Coder(t *testing.T) {
	t.Parallel()
	cfg, err := agentConfigFor(api.SubagentCoder)
	if err != nil {
		t.Fatalf("agentConfigFor: %v", err)
	}
	if cfg.systemPrompt == "" {
		t.Fatal("empty system prompt")
	}
	if !strContains(cfg.systemPrompt, "coding") && !strContains(cfg.systemPrompt, "write") {
		t.Fatalf("coder prompt missing expected directive: %q", cfg.systemPrompt)
	}
	if !contains(cfg.tools, "write_file") || !contains(cfg.tools, "shell") {
		t.Fatalf("coder tools missing write_file or shell: %v", cfg.tools)
	}
}

func TestAgentConfig_Explore(t *testing.T) {
	t.Parallel()
	cfg, err := agentConfigFor(api.SubagentExplore)
	if err != nil {
		t.Fatalf("agentConfigFor: %v", err)
	}
	if cfg.systemPrompt == "" {
		t.Fatal("empty system prompt")
	}
	if !strContains(cfg.systemPrompt, "read-only") && !strContains(cfg.systemPrompt, "investigate") {
		t.Fatalf("explore prompt missing expected directive: %q", cfg.systemPrompt)
	}
	for _, forbidden := range []string{"write_file", "str_replace_file", "edit", "shell"} {
		if contains(cfg.tools, forbidden) {
			t.Fatalf("explore tools must not include %q", forbidden)
		}
	}
}

func TestAgentConfig_Plan(t *testing.T) {
	t.Parallel()
	cfg, err := agentConfigFor(api.SubagentPlan)
	if err != nil {
		t.Fatalf("agentConfigFor: %v", err)
	}
	if cfg.systemPrompt == "" {
		t.Fatal("empty system prompt")
	}
	if !strContains(cfg.systemPrompt, "plan") && !strContains(cfg.systemPrompt, "step-by-step") {
		t.Fatalf("plan prompt missing expected directive: %q", cfg.systemPrompt)
	}
	for _, forbidden := range []string{"write_file", "str_replace_file", "edit", "shell"} {
		if contains(cfg.tools, forbidden) {
			t.Fatalf("plan tools must not include %q", forbidden)
		}
	}
}

func TestRunner_ToolCallsReturnAnswer(t *testing.T) {
	t.Parallel()
	llm := &fakeLLM{
		responses: []string{"", "done"},
		toolCalls: [][]api.ToolCall{
			{{ID: "call-1", Name: "read_file", Arguments: `{"path":"x.go"}`}},
		},
	}
	tools := &fakeToolExecutor{
		defs: []api.ToolDefinition{
			{Name: "read_file"},
			{Name: "write_file"},
		},
		outputs: map[string]string{"read_file": "package x"},
	}
	r := NewRunner(llm, tools, t.TempDir())
	res, err := r.Run(context.Background(), api.SubagentRequest{Type: api.SubagentCoder, Prompt: "read x"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Output != "done" {
		t.Fatalf("output = %q, want %q", res.Output, "done")
	}
	if res.Rounds != 2 {
		t.Fatalf("rounds = %d, want 2", res.Rounds)
	}
	if len(tools.calls) != 1 || tools.calls[0].Name != "read_file" {
		t.Fatalf("unexpected tool calls: %+v", tools.calls)
	}
}

func TestRunner_MaxRoundsExceeded(t *testing.T) {
	t.Parallel()
	llm := &fakeLLM{
		responses: []string{"", "", ""},
		toolCalls: [][]api.ToolCall{
			{{ID: "1", Name: "read_file"}},
			{{ID: "2", Name: "read_file"}},
			{{ID: "3", Name: "read_file"}},
		},
	}
	tools := &fakeToolExecutor{
		defs:    []api.ToolDefinition{{Name: "read_file"}},
		outputs: map[string]string{"read_file": "x"},
	}
	r := NewRunner(llm, tools, t.TempDir())
	_, err := r.Run(context.Background(), api.SubagentRequest{Type: api.SubagentExplore, Prompt: "go", MaxRounds: 2})
	if err == nil {
		t.Fatal("expected error for max rounds")
	}
	if !strContains(err.Error(), "max rounds") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunner_Cancellation(t *testing.T) {
	t.Parallel()
	llm := &fakeLLM{delay: 5 * time.Second}
	r := NewRunner(llm, &fakeToolExecutor{}, t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := r.Run(ctx, api.SubagentRequest{Type: api.SubagentExplore, Prompt: "hi"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
}

func TestRunner_RequiredFields(t *testing.T) {
	t.Parallel()
	r := NewRunner(&fakeLLM{}, &fakeToolExecutor{}, t.TempDir())
	if _, err := r.Run(context.Background(), api.SubagentRequest{Type: "", Prompt: "x"}); err == nil {
		t.Fatal("expected error for missing type")
	}
	if _, err := r.Run(context.Background(), api.SubagentRequest{Type: api.SubagentCoder, Prompt: ""}); err == nil {
		t.Fatal("expected error for missing prompt")
	}
}

func TestFilterDefinitions(t *testing.T) {
	t.Parallel()
	all := []api.ToolDefinition{
		{Name: "read_file"},
		{Name: "write_file"},
		{Name: "shell"},
	}
	got := filterDefinitions(all, []string{"read_file", "shell"})
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Name != "read_file" || got[1].Name != "shell" {
		t.Fatalf("unexpected order: %v", got)
	}
	if len(filterDefinitions(all, nil)) != len(all) {
		t.Fatal("nil allowlist should return all definitions")
	}
	// Mutating the returned slice must not affect the original.
	allCopy := filterDefinitions(all, nil)
	allCopy[0].Name = "mutated"
	if all[0].Name != "read_file" {
		t.Fatal("filterDefinitions returned a mutable alias of the input")
	}

	empty := filterDefinitions(all, []string{})
	if len(empty) != 0 {
		t.Fatalf("explicit empty allowlist should return no definitions, got %d", len(empty))
	}
}

func TestRunner_EmptyAllowlistBlocksAllTools(t *testing.T) {
	t.Parallel()
	llm := &fakeLLM{
		responses: []string{"The answer is 42."},
	}
	tools := &fakeToolExecutor{
		defs: []api.ToolDefinition{
			{Name: "read_file"},
			{Name: "write_file"},
		},
	}
	r := NewRunner(llm, tools, t.TempDir())
	res, err := r.Run(context.Background(), api.SubagentRequest{
		Type:         api.SubagentExplore,
		Prompt:       "what?",
		AllowedTools: []string{},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Output != "The answer is 42." {
		t.Fatalf("output = %q, want %q", res.Output, "The answer is 42.")
	}

	llm.mu.Lock()
	defer llm.mu.Unlock()
	if len(llm.calls) != 1 {
		t.Fatalf("expected 1 LLM call, got %d", len(llm.calls))
	}
	if len(llm.calls[0].tools) != 0 {
		t.Fatalf("expected 0 tool definitions for explicit empty allowlist, got %v", llm.calls[0].tools)
	}
}

func TestRunner_NilAllowlistUsesDefaultTools(t *testing.T) {
	t.Parallel()
	llm := &fakeLLM{
		responses: []string{"The answer is 42."},
	}
	tools := &fakeToolExecutor{
		defs: []api.ToolDefinition{
			{Name: "read_file"},
			{Name: "glob"},
			{Name: "grep"},
			{Name: "list_directory"},
			{Name: "write_file"},
			{Name: "shell"},
		},
	}
	r := NewRunner(llm, tools, t.TempDir())
	_, err := r.Run(context.Background(), api.SubagentRequest{Type: api.SubagentExplore, Prompt: "what?"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	llm.mu.Lock()
	defer llm.mu.Unlock()
	if len(llm.calls) != 1 {
		t.Fatalf("expected 1 LLM call, got %d", len(llm.calls))
	}
	// explore default tools are read_file, glob, grep, list_directory.
	if len(llm.calls[0].tools) != 4 {
		t.Fatalf("expected 4 tool definitions for default allowlist, got %d", len(llm.calls[0].tools))
	}
	for _, d := range llm.calls[0].tools {
		if d.Name == "write_file" || d.Name == "shell" {
			t.Fatalf("unexpected tool in default allowlist: %q", d.Name)
		}
	}
}

func TestRunner_MismatchedSandboxRoot(t *testing.T) {
	t.Parallel()
	r := NewRunner(&fakeLLM{}, &fakeToolExecutor{}, "/tmp/runner-root")
	_, err := r.Run(context.Background(), api.SubagentRequest{
		Type:        api.SubagentExplore,
		Prompt:      "hi",
		SandboxRoot: "/tmp/other-root",
	})
	if err == nil {
		t.Fatal("expected error for mismatched sandbox root")
	}
	if !strContains(err.Error(), "mismatched sandbox root") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunner_NilLLMClient(t *testing.T) {
	t.Parallel()
	r := NewRunner(nil, &fakeToolExecutor{}, t.TempDir())
	_, err := r.Run(context.Background(), api.SubagentRequest{Type: api.SubagentExplore, Prompt: "hi"})
	if err == nil {
		t.Fatal("expected error for nil llm client")
	}
	if !strContains(err.Error(), "llm client is nil") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunner_NilToolExecutor(t *testing.T) {
	t.Parallel()
	r := NewRunner(&fakeLLM{}, nil, t.TempDir())
	_, err := r.Run(context.Background(), api.SubagentRequest{Type: api.SubagentExplore, Prompt: "hi"})
	if err == nil {
		t.Fatal("expected error for nil tool executor")
	}
	if !strContains(err.Error(), "tool executor is nil") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunner_NilLLMResponse(t *testing.T) {
	t.Parallel()
	r := NewRunner(&fakeLLM{}, &fakeToolExecutor{}, t.TempDir())
	_, err := r.Run(context.Background(), api.SubagentRequest{Type: api.SubagentExplore, Prompt: "hi"})
	if err == nil {
		t.Fatal("expected error for nil llm response")
	}
	if !strContains(err.Error(), "nil message") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunner_PreservesPartialOutputOnError(t *testing.T) {
	t.Parallel()
	llm := &fakeLLM{
		responses: []string{"", "done"},
		toolCalls: [][]api.ToolCall{
			{{ID: "call-1", Name: "read_file", Arguments: `{"path":"x.go"}`}},
		},
	}
	tools := &fakeToolExecutor{
		defs: []api.ToolDefinition{
			{Name: "read_file"},
		},
		outputs: map[string]string{"read_file": "partial content"},
		errs:    map[string]error{"read_file": errors.New("permission denied")},
	}
	r := NewRunner(llm, tools, t.TempDir())
	res, err := r.Run(context.Background(), api.SubagentRequest{Type: api.SubagentExplore, Prompt: "read x"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Output != "done" {
		t.Fatalf("output = %q, want %q", res.Output, "done")
	}

	llm.mu.Lock()
	defer llm.mu.Unlock()
	if len(llm.calls) < 2 {
		t.Fatalf("expected at least 2 LLM calls, got %d", len(llm.calls))
	}
	msgs := llm.calls[1].messages
	last := msgs[len(msgs)-1]
	if last.Role != api.RoleTool {
		t.Fatalf("expected last message to be a tool message, got %s", last.Role)
	}
	want := "partial content\nError: permission denied"
	if last.Content != want {
		t.Fatalf("tool message content = %q, want %q", last.Content, want)
	}
}

func TestRunner_MaxRoundsCapped(t *testing.T) {
	t.Parallel()
	llm := &fakeLLM{
		responses:  []string{""},
		toolCalls:  [][]api.ToolCall{{{ID: "1", Name: "read_file"}}},
		repeatLast: true,
	}
	tools := &fakeToolExecutor{
		defs:    []api.ToolDefinition{{Name: "read_file"}},
		outputs: map[string]string{"read_file": "x"},
	}
	r := NewRunner(llm, tools, t.TempDir())
	_, err := r.Run(context.Background(), api.SubagentRequest{Type: api.SubagentExplore, Prompt: "go", MaxRounds: 100})
	if err == nil {
		t.Fatal("expected error for max rounds")
	}
	if !strContains(err.Error(), "max rounds (50)") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunner_IntersectDedupes(t *testing.T) {
	t.Parallel()
	all := []api.ToolDefinition{
		{Name: "read_file"},
		{Name: "write_file"},
	}
	dups := intersect([]string{"read_file", "read_file", "write_file"}, []string{"read_file", "write_file"})
	if len(dups) != 2 {
		t.Fatalf("expected 2 deduplicated entries, got %d", len(dups))
	}
	if dups[0] != "read_file" || dups[1] != "write_file" {
		t.Fatalf("unexpected order: %v", dups)
	}
	_ = filterDefinitions(all, dups)
}

func TestAgentConfig_Unknown(t *testing.T) {
	t.Parallel()
	_, err := agentConfigFor(api.SubagentType("unknown"))
	if err == nil {
		t.Fatal("expected error for unknown agent type")
	}
	if !strContains(err.Error(), "unknown subagent type") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestToolResultContent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		result api.ToolResult
		want   string
	}{
		{"output only", api.ToolResult{Output: "hello"}, "hello"},
		{"error only", api.ToolResult{Error: "fail"}, "Error: fail"},
		{"both", api.ToolResult{Output: "hello", Error: "fail"}, "hello\nError: fail"},
		{"empty", api.ToolResult{}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := toolResultContent(tc.result)
			if got != tc.want {
				t.Fatalf("toolResultContent = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRunner_UnknownType(t *testing.T) {
	t.Parallel()
	r := NewRunner(&fakeLLM{}, &fakeToolExecutor{}, t.TempDir())
	_, err := r.Run(context.Background(), api.SubagentRequest{Type: "unknown", Prompt: "hi"})
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
	if !strContains(err.Error(), "subagent config") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunner_LLMError(t *testing.T) {
	t.Parallel()
	llm := &fakeLLM{
		responses: []string{""},
		toolCalls: [][]api.ToolCall{
			{{ID: "1", Name: "read_file"}},
		},
	}
	tools := &fakeToolExecutor{
		defs:    []api.ToolDefinition{{Name: "read_file"}},
		outputs: map[string]string{"read_file": "x"},
	}
	r := NewRunner(llm, tools, t.TempDir())
	_, err := r.Run(context.Background(), api.SubagentRequest{Type: api.SubagentExplore, Prompt: "go"})
	if err == nil {
		t.Fatal("expected error for LLM failure")
	}
	if !strContains(err.Error(), "subagent llm chat") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunner_ContextCancelledBeforeStart(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := NewRunner(&fakeLLM{}, &fakeToolExecutor{}, t.TempDir())
	_, err := r.Run(ctx, api.SubagentRequest{Type: api.SubagentExplore, Prompt: "hi"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

func TestRunner_ToolErrorFillsMissingFields(t *testing.T) {
	t.Parallel()
	llm := &fakeLLM{
		responses: []string{"", "done"},
		toolCalls: [][]api.ToolCall{
			{{ID: "call-1", Name: "read_file", Arguments: `{"path":"x.go"}`}},
		},
	}
	tools := &fakeToolExecutor{
		defs:          []api.ToolDefinition{{Name: "read_file"}},
		outputs:       map[string]string{"read_file": "partial content"},
		errs:          map[string]error{"read_file": errors.New("permission denied")},
		emptyErrorFor: map[string]bool{"read_file": true},
	}
	r := NewRunner(llm, tools, t.TempDir())
	res, err := r.Run(context.Background(), api.SubagentRequest{Type: api.SubagentExplore, Prompt: "read x"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Output != "done" {
		t.Fatalf("output = %q, want %q", res.Output, "done")
	}

	llm.mu.Lock()
	defer llm.mu.Unlock()
	if len(llm.calls) < 2 {
		t.Fatalf("expected at least 2 LLM calls, got %d", len(llm.calls))
	}
	msgs := llm.calls[1].messages
	last := msgs[len(msgs)-1]
	if last.Role != api.RoleTool {
		t.Fatalf("expected last message to be a tool message, got %s", last.Role)
	}
	want := "partial content\nError: permission denied"
	if last.Content != want {
		t.Fatalf("tool message content = %q, want %q", last.Content, want)
	}
	if last.ToolCallID != "call-1" {
		t.Fatalf("tool call ID = %q, want %q", last.ToolCallID, "call-1")
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func strContains(s, substr string) bool {
	return strings.Contains(s, substr)
}

// fakeLLM is a test double for api.LLMClient.
type fakeLLM struct {
	responses  []string
	toolCalls  [][]api.ToolCall
	delay      time.Duration
	repeatLast bool // repeat the last response indefinitely once idx passes it

	mu    sync.Mutex
	idx   int
	calls []chatCall
}

type chatCall struct {
	messages []api.Message
	tools    []api.ToolDefinition
}

func (f *fakeLLM) Chat(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (*api.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.delay > 0 {
		timer := time.NewTimer(f.delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, chatCall{messages: messages, tools: tools})
	if f.idx >= len(f.responses) {
		if f.repeatLast && len(f.responses) > 0 {
			return f.messageAt(len(f.responses) - 1), nil
		}
		if len(f.responses) == 0 {
			return nil, nil
		}
		return nil, errors.New("fakeLLM: no more responses")
	}
	msg := f.messageAt(f.idx)
	f.idx++
	return msg, nil
}

func (f *fakeLLM) messageAt(idx int) *api.Message {
	var tcs []api.ToolCall
	if idx < len(f.toolCalls) {
		tcs = f.toolCalls[idx]
	}
	content := ""
	if idx < len(f.responses) {
		content = f.responses[idx]
	}
	return &api.Message{
		Role:         api.RoleAssistant,
		Content:      content,
		ToolCalls:    tcs,
		CreatedAt:    time.Now(),
		FinishReason: "stop",
	}
}

func (f *fakeLLM) ChatStream(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
	return nil, errors.New("fakeLLM: streaming not implemented")
}

func (f *fakeLLM) Models() []api.ModelInfo { return nil }

// fakeToolExecutor is a test double for api.ToolExecutor.
type fakeToolExecutor struct {
	defs          []api.ToolDefinition
	outputs       map[string]string
	errs          map[string]error
	emptyErrorFor map[string]bool

	mu    sync.Mutex
	calls []api.ToolCall
}

func (f *fakeToolExecutor) Execute(ctx context.Context, call api.ToolCall) (api.ToolResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, call)
	result := api.ToolResult{
		CallID: call.ID,
		Name:   call.Name,
		Output: f.outputs[call.Name],
	}
	if err := f.errs[call.Name]; err != nil {
		result.Error = err.Error()
		if f.emptyErrorFor[call.Name] {
			result.CallID = ""
			result.Name = ""
		}
		return result, err
	}
	return result, nil
}

func (f *fakeToolExecutor) Definitions(_ context.Context) []api.ToolDefinition {
	return f.defs
}

func (f *fakeToolExecutor) IsReadOnly(name string) bool {
	return false
}
