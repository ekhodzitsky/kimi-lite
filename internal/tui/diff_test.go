package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func TestIsPreviewPathAllowed(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()

	insideFile := filepath.Join(tmp, "foo.txt")
	if err := os.WriteFile(insideFile, []byte("old"), 0644); err != nil {
		t.Fatalf("write inside file: %v", err)
	}

	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	symlinkPath := filepath.Join(tmp, "link.txt")
	if err := os.Symlink(outsideFile, symlinkPath); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	tests := []struct {
		name        string
		path        string
		sandboxRoot string
		wantEmpty   bool
	}{
		{"inside sandbox", "foo.txt", tmp, false},
		{"outside sandbox", outsideFile, tmp, true},
		{"sensitive etc", "/etc/passwd", tmp, true},
		{"empty", "", tmp, true},
		{"symlink escape", symlinkPath, tmp, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			diff := computeFileDiff(tt.path, []byte("new content"), tt.sandboxRoot)
			if gotEmpty := diff == ""; gotEmpty != tt.wantEmpty {
				t.Errorf("computeFileDiff(%q, ..., %q) empty=%v, want %v", tt.path, tt.sandboxRoot, gotEmpty, tt.wantEmpty)
			}
		})
	}
}

func TestToolCallDiff_StrReplaceFile_RespectsSandbox(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()

	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret old"), 0644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	symlinkPath := filepath.Join(tmp, "link.txt")
	if err := os.Symlink(outsideFile, symlinkPath); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	call := apiCallStrReplace(symlinkPath, "old", "new")
	if diff := toolCallDiff(call, tmp); diff != "" {
		t.Errorf("expected empty diff for symlink escape, got: %s", diff)
	}
}

func apiCallStrReplace(path, oldStr, newStr string) api.ToolCall {
	return api.ToolCall{
		ID:        "call_1",
		Name:      "str_replace_file",
		Arguments: `{"path":"` + path + `","old_string":"` + oldStr + `","new_string":"` + newStr + `"}`,
	}
}

func TestUnifiedDiff_Basic(t *testing.T) {
	t.Parallel()

	old := "line1\nline2\nline3\n"
	new := "line1\nline2 modified\nline3\n"
	diff := unifiedDiff("test.txt", old, new)

	if !strings.Contains(diff, "--- test.txt") {
		t.Error("expected --- header")
	}
	if !strings.Contains(diff, "+++ test.txt") {
		t.Error("expected +++ header")
	}
	if !strings.Contains(diff, "-line2") {
		t.Error("expected removed line2")
	}
	if !strings.Contains(diff, "+line2 modified") {
		t.Error("expected added line2 modified")
	}
}
