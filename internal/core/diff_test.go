package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func TestComputeFileDiff_RespectsProtectedPaths(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	blocked := filepath.Join(tmp, "blocked")
	if err := os.Mkdir(blocked, 0755); err != nil {
		t.Fatalf("mkdir blocked: %v", err)
	}
	allowedFile := filepath.Join(tmp, "allowed.txt")
	blockedFile := filepath.Join(blocked, "secret.txt")
	for _, p := range []string{allowedFile, blockedFile} {
		if err := os.WriteFile(p, []byte("old"), 0644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	protected := []string{blocked}

	if diff := ComputeFileDiff(allowedFile, []byte("new"), tmp, protected); diff == "" {
		t.Error("expected non-empty diff for allowed file")
	}
	if diff := ComputeFileDiff(blockedFile, []byte("new"), tmp, protected); diff != "" {
		t.Errorf("expected empty diff for protected path, got %q", diff)
	}
}

func TestToolCallDiff_StrReplaceFile_SingleReplacementByDefault(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	target := filepath.Join(tmp, "file.txt")
	if err := os.WriteFile(target, []byte("alpha beta alpha"), 0644); err != nil {
		t.Fatalf("write target: %v", err)
	}

	call := api.ToolCall{
		ID:        "call_1",
		Name:      "str_replace_file",
		Arguments: `{"path":"` + target + `","old_string":"alpha","new_string":"gamma"}`,
	}
	diff := ToolCallDiff(call, tmp, nil)
	if diff == "" {
		t.Fatal("expected diff")
	}
	if strings.Count(diff, "-alpha") != 1 {
		t.Errorf("expected exactly one removed alpha line, diff:\n%s", diff)
	}
	if strings.Count(diff, "+gamma") != 1 {
		t.Errorf("expected exactly one added gamma line, diff:\n%s", diff)
	}
}

func TestToolCallDiff_StrReplaceFile_ReplaceAll(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	target := filepath.Join(tmp, "file.txt")
	if err := os.WriteFile(target, []byte("alpha\nbeta\nalpha"), 0644); err != nil {
		t.Fatalf("write target: %v", err)
	}

	call := api.ToolCall{
		ID:        "call_1",
		Name:      "str_replace_file",
		Arguments: `{"path":"` + target + `","old_string":"alpha","new_string":"gamma","replace_all":true}`,
	}
	diff := ToolCallDiff(call, tmp, nil)
	if diff == "" {
		t.Fatal("expected diff")
	}
	if strings.Count(diff, "-alpha") != 2 {
		t.Errorf("expected two removed alpha lines, diff:\n%s", diff)
	}
	if strings.Count(diff, "+gamma") != 2 {
		t.Errorf("expected two added gamma lines, diff:\n%s", diff)
	}
}

func TestToolCallDiff_WriteFile_RespectsProtectedPaths(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	blocked := filepath.Join(tmp, "blocked")
	if err := os.Mkdir(blocked, 0755); err != nil {
		t.Fatalf("mkdir blocked: %v", err)
	}
	blockedFile := filepath.Join(blocked, "secret.txt")
	if err := os.WriteFile(blockedFile, []byte("old"), 0644); err != nil {
		t.Fatalf("write blocked file: %v", err)
	}

	call := api.ToolCall{
		ID:        "call_1",
		Name:      "write_file",
		Arguments: `{"path":"` + blockedFile + `","content":"new"}`,
	}
	if diff := ToolCallDiff(call, tmp, []string{blocked}); diff != "" {
		t.Errorf("expected empty diff for protected path, got %q", diff)
	}
}

func TestToolCallDiff_StrReplaceFile_SymlinkEscape(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret old"), 0644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	linkPath := filepath.Join(tmp, "link.txt")
	if err := os.Symlink(outsideFile, linkPath); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	call := api.ToolCall{
		ID:        "call_1",
		Name:      "str_replace_file",
		Arguments: `{"path":"` + linkPath + `","old_string":"old","new_string":"new"}`,
	}
	if diff := ToolCallDiff(call, tmp, nil); diff != "" {
		t.Errorf("expected empty diff for symlink escape, got %q", diff)
	}
}
