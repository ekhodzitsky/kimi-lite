package hooks

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func TestRunner_Creates(t *testing.T) {
	t.Parallel()
	r := NewRunner(nil)
	if r == nil {
		t.Fatal("NewRunner returned nil")
	}
}

func TestRunner_RunsMatchingHook(t *testing.T) {
	t.Parallel()
	calls := 0
	r := NewRunner([]api.HookConfig{{
		Event:   api.HookToolCall,
		Command: "sh",
		Args:    []string{"-c", "exit 0"},
	}})
	r.execCommand = func(ctx context.Context, cfg api.HookConfig, data api.HookData) error {
		calls++
		return nil
	}
	if err := r.Run(context.Background(), api.HookData{Event: api.HookToolCall}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestRunner_SkipsNonMatchingHook(t *testing.T) {
	t.Parallel()
	calls := 0
	r := NewRunner([]api.HookConfig{{
		Event:   api.HookToolCall,
		Command: "sh",
		Args:    []string{"-c", "exit 0"},
	}})
	r.execCommand = func(ctx context.Context, cfg api.HookConfig, data api.HookData) error {
		calls++
		return nil
	}
	if err := r.Run(context.Background(), api.HookData{Event: api.HookTurnStart}); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("calls = %d, want 0", calls)
	}
}

func TestRunner_TemplatedArgsReceiveData(t *testing.T) {
	t.Parallel()
	var gotCfg api.HookConfig
	var gotData api.HookData
	r := NewRunner([]api.HookConfig{{
		Event:   api.HookToolCall,
		Command: "sh",
		Args:    []string{"-c", "echo {{.ToolName}} {{.SessionID}}"},
	}})
	r.execCommand = func(ctx context.Context, cfg api.HookConfig, data api.HookData) error {
		gotCfg = cfg
		gotData = data
		return nil
	}
	data := api.HookData{
		Event:     api.HookToolCall,
		SessionID: "sess-1",
		ToolName:  "read_file",
	}
	if err := r.Run(context.Background(), data); err != nil {
		t.Fatal(err)
	}
	if len(gotCfg.Args) != 2 {
		t.Fatalf("len(args) = %d, want 2", len(gotCfg.Args))
	}
	want := "echo read_file sess-1"
	if gotCfg.Args[1] != want {
		t.Fatalf("rendered arg = %q, want %q", gotCfg.Args[1], want)
	}
	if gotData.ToolName != "read_file" {
		t.Fatalf("data.ToolName = %q, want %q", gotData.ToolName, "read_file")
	}
}

func TestRunner_PropagatesArgsTemplateError(t *testing.T) {
	t.Parallel()
	r := NewRunner([]api.HookConfig{{
		Event:   api.HookToolCall,
		Command: "sh",
		Args:    []string{"{{.Bad"},
	}})
	if err := r.Run(context.Background(), api.HookData{Event: api.HookToolCall}); err == nil {
		t.Fatal("expected template error")
	}
}

func TestRunner_PropagatesExecError(t *testing.T) {
	t.Parallel()
	r := NewRunner([]api.HookConfig{{
		Event:   api.HookToolCall,
		Command: "sh",
		Args:    []string{"-c", "exit 0"},
	}})
	r.execCommand = func(ctx context.Context, cfg api.HookConfig, data api.HookData) error {
		return errors.New("boom")
	}
	err := r.Run(context.Background(), api.HookData{Event: api.HookToolCall})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected error to contain 'boom', got: %v", err)
	}
}

func TestRunner_ContinueOnErrorAggregatesErrors(t *testing.T) {
	t.Parallel()
	calls := 0
	r := NewRunner([]api.HookConfig{
		{
			Event:           api.HookToolCall,
			Command:         "sh",
			Args:            []string{"-c", "exit 0"},
			ContinueOnError: true,
		},
		{
			Event:           api.HookToolCall,
			Command:         "sh",
			Args:            []string{"-c", "exit 0"},
			ContinueOnError: true,
		},
	})
	r.execCommand = func(ctx context.Context, cfg api.HookConfig, data api.HookData) error {
		calls++
		return fmt.Errorf("boom %d", calls)
	}
	err := r.Run(context.Background(), api.HookData{Event: api.HookToolCall})
	if err == nil {
		t.Fatal("expected aggregated error")
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if !strings.Contains(err.Error(), "boom 1") || !strings.Contains(err.Error(), "boom 2") {
		t.Fatalf("expected aggregated errors, got: %v", err)
	}
}

func TestRunner_HaltsOnFirstError(t *testing.T) {
	t.Parallel()
	calls := 0
	r := NewRunner([]api.HookConfig{
		{
			Event:   api.HookToolCall,
			Command: "sh",
			Args:    []string{"-c", "exit 0"},
		},
		{
			Event:   api.HookToolCall,
			Command: "sh",
			Args:    []string{"-c", "exit 0"},
		},
	})
	r.execCommand = func(ctx context.Context, cfg api.HookConfig, data api.HookData) error {
		calls++
		return errors.New("boom")
	}
	if err := r.Run(context.Background(), api.HookData{Event: api.HookToolCall}); err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestExecHook_RealEnvVars(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}
	cfg := api.HookConfig{
		Event:   api.HookToolCall,
		Command: "sh",
		Args:    []string{"-c", "echo $KIMI_HOOK_EVENT $KIMI_HOOK_TOOL_NAME $KIMI_HOOK_SESSION_ID $EXTRA"},
		Env:     map[string]string{"EXTRA": "custom"},
	}
	data := api.HookData{
		Event:     api.HookToolCall,
		SessionID: "sess-42",
		ToolName:  "read_file",
	}
	out, err := captureExec(cfg, data)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(out)
	want := "tool_call read_file sess-42 custom"
	if got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestExecHook_RealNonZeroExit(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}
	cfg := api.HookConfig{
		Event:   api.HookToolCall,
		Command: "sh",
		Args:    []string{"-c", "echo oops >&2; exit 7"},
	}
	err := execHook(context.Background(), cfg, api.HookData{Event: api.HookToolCall})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "oops") {
		t.Fatalf("error should contain output, got: %v", err)
	}
}

func TestExecHook_Timeout(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}
	cfg := api.HookConfig{
		Event:   api.HookToolCall,
		Command: "sh",
		Args:    []string{"-c", "sleep 10"},
		Timeout: 50 * time.Millisecond,
	}
	start := time.Now()
	err := execHook(context.Background(), cfg, api.HookData{Event: api.HookToolCall})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("error should mention timeout, got: %v", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("timeout took too long: %v", elapsed)
	}
}

func TestExecHook_DefaultTimeout(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}
	cfg := api.HookConfig{
		Event:   api.HookToolCall,
		Command: "sh",
		Args:    []string{"-c", "echo ok"},
	}
	err := execHook(context.Background(), cfg, api.HookData{Event: api.HookToolCall})
	if err != nil {
		t.Fatal(err)
	}
}

func TestExecHook_Cancellation(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}
	cfg := api.HookConfig{
		Event:   api.HookToolCall,
		Command: "sh",
		Args:    []string{"-c", "sleep 10"},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var runErr error
	go func() {
		defer close(done)
		runErr = execHook(ctx, cfg, api.HookData{Event: api.HookToolCall})
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation took too long")
	}
	if runErr == nil {
		t.Fatal("expected cancellation error")
	}
}

func TestBuildEnv_ExtraOverridesHookVars(t *testing.T) {
	t.Parallel()
	extra := map[string]string{"KIMI_HOOK_TOOL_NAME": "override"}
	data := api.HookData{Event: api.HookToolCall, ToolName: "original"}
	env := buildEnv(extra, data)
	m := envMap(env)
	if m["KIMI_HOOK_TOOL_NAME"] != "override" {
		t.Fatalf("extra did not override hook var: got %q", m["KIMI_HOOK_TOOL_NAME"])
	}
}

func TestBuildEnv_IncludesAllFields(t *testing.T) {
	t.Parallel()
	data := api.HookData{
		Event:      api.HookToolResult,
		SessionID:  "s1",
		TurnID:     "t1",
		ToolName:   "tn",
		ToolArgs:   "ta",
		ToolResult: "tr",
		Decision:   "d",
		Error:      "e",
	}
	env := buildEnv(nil, data)
	m := envMap(env)
	checks := map[string]string{
		"KIMI_HOOK_EVENT":       "tool_result",
		"KIMI_HOOK_SESSION_ID":  "s1",
		"KIMI_HOOK_TURN_ID":     "t1",
		"KIMI_HOOK_TOOL_NAME":   "tn",
		"KIMI_HOOK_TOOL_ARGS":   "ta",
		"KIMI_HOOK_TOOL_RESULT": "tr",
		"KIMI_HOOK_DECISION":    "d",
		"KIMI_HOOK_ERROR":       "e",
	}
	for k, want := range checks {
		if m[k] != want {
			t.Fatalf("%s = %q, want %q", k, m[k], want)
		}
	}
}

func captureExec(cfg api.HookConfig, data api.HookData) (string, error) {
	cmd := exec.CommandContext(context.Background(), cfg.Command, cfg.Args...)
	cmd.Env = buildEnv(cfg.Env, data)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestRunner_MissingTemplateKeyReturnsError(t *testing.T) {
	t.Parallel()
	r := NewRunner([]api.HookConfig{{
		Event:   api.HookToolCall,
		Command: "sh",
		Args:    []string{"-c", "echo {{.MissingField}}"},
	}})
	if err := r.Run(context.Background(), api.HookData{Event: api.HookToolCall}); err == nil {
		t.Fatal("expected missing template key error")
	}
}

func TestRunner_DeepCopiesConfig(t *testing.T) {
	t.Parallel()
	cfg := []api.HookConfig{{
		Event:   api.HookToolCall,
		Command: "sh",
		Args:    []string{"-c", "echo {{.ToolName}}"},
		Env:     map[string]string{"KEEP": "original"},
	}}
	r := NewRunner(cfg)
	// Mutate the original slice and map after construction.
	cfg[0].Args[0] = "echo changed"
	cfg[0].Env["KEEP"] = "mutated"
	cfg[0].Env["NEW"] = "new"

	var got api.HookConfig
	r.execCommand = func(ctx context.Context, cfg api.HookConfig, data api.HookData) error {
		got = cfg
		return nil
	}
	if err := r.Run(context.Background(), api.HookData{Event: api.HookToolCall, ToolName: "x"}); err != nil {
		t.Fatal(err)
	}
	if got.Args[0] != "-c" {
		t.Fatalf("args mutated: %v", got.Args)
	}
	if got.Env["KEEP"] != "original" {
		t.Fatalf("env mutated: %v", got.Env)
	}
	if _, ok := got.Env["NEW"]; ok {
		t.Fatal("env should not contain NEW")
	}
}

func TestExecHook_TruncatesLargeOutput(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}
	cfg := api.HookConfig{
		Event:   api.HookToolCall,
		Command: "sh",
		Args:    []string{"-c", `head -c 2097152 /dev/zero | tr '\0' 'x'; exit 1`},
	}
	err := execHook(context.Background(), cfg, api.HookData{Event: api.HookToolCall})
	if err == nil {
		t.Fatal("expected error for non-zero exit")
	}
	if !strings.Contains(err.Error(), "[truncated]") {
		t.Fatalf("expected truncation marker, got: %v", err)
	}
}

func TestExecHook_ParentDeadlineDistinguishesFromHookTimeout(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}
	cfg := api.HookConfig{
		Event:   api.HookToolCall,
		Command: "sh",
		Args:    []string{"-c", "sleep 10"},
		Timeout: time.Minute,
	}
	parentCtx, cancel := context.WithDeadline(context.Background(), time.Now().Add(50*time.Millisecond))
	defer cancel()
	start := time.Now()
	err := execHook(parentCtx, cfg, api.HookData{Event: api.HookToolCall})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected parent deadline error")
	}
	if !strings.Contains(err.Error(), "parent context") {
		t.Fatalf("expected parent context error, got: %v", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("parent deadline took too long to propagate: %v", elapsed)
	}
}

func envMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, e := range env {
		k, v, _ := strings.Cut(e, "=")
		m[k] = v
	}
	return m
}
