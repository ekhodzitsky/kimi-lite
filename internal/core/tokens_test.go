package core

import (
	"testing"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func TestHeuristicTokenEstimator_Empty(t *testing.T) {
	t.Parallel()
	e := NewHeuristicTokenEstimator()
	if got := e.Estimate(nil); got != 0 {
		t.Errorf("empty estimate = %d, want 0", got)
	}
}

func TestHeuristicTokenEstimator_ASCII(t *testing.T) {
	t.Parallel()
	e := NewHeuristicTokenEstimator()
	msgs := []api.Message{{Role: api.RoleUser, Content: "hello world"}}
	got := e.Estimate(msgs)
	want := 3 + 11/4 // per-message overhead + content
	if got != want {
		t.Errorf("ascii estimate = %d, want %d", got, want)
	}
}

func TestHeuristicTokenEstimator_CJK(t *testing.T) {
	t.Parallel()
	e := NewHeuristicTokenEstimator()
	msgs := []api.Message{{Role: api.RoleUser, Content: "你好世界"}}
	got := e.Estimate(msgs)
	want := 3 + 4 // overhead + one token per CJK rune
	if got != want {
		t.Errorf("cjk estimate = %d, want %d", got, want)
	}
}

func TestHeuristicTokenEstimator_ToolCall(t *testing.T) {
	t.Parallel()
	e := NewHeuristicTokenEstimator()
	msgs := []api.Message{{
		Role: api.RoleAssistant,
		ToolCalls: []api.ToolCall{
			{Name: "read_file", Arguments: `{"path":"main.go"}`},
		},
	}}
	got := e.Estimate(msgs)
	if got <= 0 {
		t.Fatalf("tool call estimate = %d, want > 0", got)
	}
	// 10 fixed overhead + name + arguments + per-message overhead.
	want := 3 + 10 + 9/4 + 17/4
	if got != want {
		t.Errorf("tool call estimate = %d, want %d", got, want)
	}
}

func BenchmarkHeuristicTokenEstimator(b *testing.B) {
	e := NewHeuristicTokenEstimator()
	msgs := []api.Message{{
		Role:    api.RoleUser,
		Content: "package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"hello\") }",
	}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = e.Estimate(msgs)
	}
}

func FuzzHeuristicTokenEstimator(f *testing.F) {
	f.Add("hello world")
	f.Add("你好世界")
	f.Add("package main\n\nfunc main() {}")
	f.Add("")

	f.Fuzz(func(t *testing.T, content string) {
		e := NewHeuristicTokenEstimator()
		base := e.Estimate([]api.Message{{Role: api.RoleUser, Content: content}})
		if base < 0 {
			t.Errorf("negative estimate for %q: %d", content, base)
		}
		double := e.Estimate([]api.Message{
			{Role: api.RoleUser, Content: content},
			{Role: api.RoleUser, Content: content},
		})
		if double < base {
			t.Errorf("estimate not monotonic: base=%d double=%d", base, double)
		}
	})
}
