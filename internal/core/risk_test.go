package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func TestRiskEvaluator_Baseline(t *testing.T) {
	t.Parallel()

	sandbox := t.TempDir()
	e := NewRiskEvaluator(nil, sandbox)

	cases := []struct {
		name string
		call api.ToolCall
		want api.RiskLevel
	}{
		{"read_file", api.ToolCall{Name: "read_file", Arguments: `{"path":"main.go"}`}, api.RiskLevelLow},
		{"glob", api.ToolCall{Name: "glob", Arguments: `{"pattern":"*.go"}`}, api.RiskLevelLow},
		{"write_file", api.ToolCall{Name: "write_file", Arguments: `{"path":"main.go"}`}, api.RiskLevelMedium},
		{"str_replace_file", api.ToolCall{Name: "str_replace_file", Arguments: `{"path":"main.go"}`}, api.RiskLevelMedium},
		{"shell", api.ToolCall{Name: "shell", Arguments: `{"command":"ls"}`}, api.RiskLevelHigh},
		{"unknown", api.ToolCall{Name: "custom_tool", Arguments: `{}`}, api.RiskLevelMedium},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			level, _ := e.Evaluate(tc.call)
			if level != tc.want {
				t.Errorf("Evaluate() = %q, want %q", level, tc.want)
			}
		})
	}
}

func TestRiskEvaluator_PathEscape(t *testing.T) {
	t.Parallel()

	sandbox := t.TempDir()
	e := NewRiskEvaluator(nil, sandbox)

	outside := filepath.Join(sandbox, "..", "outside.txt")
	call := api.ToolCall{Name: "write_file", Arguments: `{"path":"` + outside + `"}`}
	level, reason := e.Evaluate(call)
	if level != api.RiskLevelHigh {
		t.Errorf("path escape risk = %q, want high (%s)", level, reason)
	}
}

func TestRiskEvaluator_SymlinkEscape(t *testing.T) {
	t.Parallel()

	sandbox := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	linkPath := filepath.Join(sandbox, "link.txt")
	if err := os.Symlink(outsideFile, linkPath); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	e := NewRiskEvaluator(nil, sandbox)
	call := api.ToolCall{Name: "write_file", Arguments: `{"path":"` + linkPath + `"}`}
	level, reason := e.Evaluate(call)
	if level != api.RiskLevelHigh {
		t.Errorf("symlink escape risk = %q, want high (%s)", level, reason)
	}
}

func TestRiskEvaluator_TildeEscape(t *testing.T) {
	t.Parallel()

	sandbox := t.TempDir()
	e := NewRiskEvaluator(nil, sandbox)

	call := api.ToolCall{Name: "write_file", Arguments: `{"path":"~/.ssh/id_rsa"}`}
	level, reason := e.Evaluate(call)
	if level != api.RiskLevelHigh {
		t.Errorf("tilde escape risk = %q, want high (%s)", level, reason)
	}
}

func TestRiskEvaluator_CustomRule(t *testing.T) {
	t.Parallel()

	sandbox := t.TempDir()
	e := NewRiskEvaluator([]api.RiskRule{
		{Tool: "shell", Path: "", Level: api.RiskLevelMedium, Message: "allowed shell"},
	}, sandbox)

	level, _ := e.Evaluate(api.ToolCall{Name: "shell", Arguments: `{"command":"git status"}`})
	if level != api.RiskLevelMedium {
		t.Errorf("custom rule risk = %q, want medium", level)
	}
}

func TestRiskEvaluator_CustomRuleGlob(t *testing.T) {
	t.Parallel()

	sandbox := t.TempDir()
	e := NewRiskEvaluator([]api.RiskRule{
		{Tool: "write_file", Path: "*.md", Level: api.RiskLevelLow, Message: "docs are safe"},
	}, sandbox)

	level, _ := e.Evaluate(api.ToolCall{Name: "write_file", Arguments: `{"path":"README.md"}`})
	if level != api.RiskLevelLow {
		t.Errorf("glob rule risk = %q, want low", level)
	}
}

func BenchmarkEvaluate(b *testing.B) {
	sandbox := b.TempDir()
	e := NewRiskEvaluator([]api.RiskRule{
		{Tool: "shell", Path: "", Level: api.RiskLevelMedium, Message: "shell is medium"},
	}, sandbox)
	call := api.ToolCall{Name: "write_file", Arguments: `{"path":"README.md"}`}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = e.Evaluate(call)
	}
}

func FuzzRiskEvaluator(f *testing.F) {
	f.Add("read_file", `{"path":"main.go"}`)
	f.Add("shell", `{"command":"rm -rf /"}`)
	f.Add("write_file", `{"path":"../../../etc/passwd"}`)
	f.Add("unknown_tool", `{"x":"`+string(make([]byte, 100))+`"}`)

	f.Fuzz(func(t *testing.T, name string, args string) {
		sandbox := t.TempDir()
		e := NewRiskEvaluator(nil, sandbox)
		level, _ := e.Evaluate(api.ToolCall{Name: name, Arguments: args})
		if !level.Valid() {
			t.Errorf("invalid level %q for %s %s", level, name, args)
		}
	})
}
