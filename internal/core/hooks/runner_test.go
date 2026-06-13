package hooks

import (
	"context"
	"errors"
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
	if err := r.Run(context.Background(), api.HookData{Event: api.HookToolCall}); err == nil || err.Error() != "boom" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunner_ContinueOnErrorSkipsError(t *testing.T) {
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
			Event:   api.HookToolCall,
			Command: "sh",
			Args:    []string{"-c", "exit 0"},
		},
	})
	r.execCommand = func(ctx context.Context, cfg api.HookConfig, data api.HookData) error {
		calls++
		if calls == 1 {
			return errors.New("boom")
		}
		return nil
	}
	if err := r.Run(context.Background(), api.HookData{Event: api.HookToolCall}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
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

func envMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, e := range env {
		k, v, _ := strings.Cut(e, "=")
		m[k] = v
	}
	return m
}
