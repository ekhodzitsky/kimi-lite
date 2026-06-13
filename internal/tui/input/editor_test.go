package input

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ekhodzitsky/kimi-lite/internal/tui/styles"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func TestResolveEditor(t *testing.T) {
	tests := []struct {
		name       string
		configured string
		visual     string
		editor     string
		want       string
	}{
		{
			name:       "configured overrides env",
			configured: "nano",
			visual:     "vim",
			editor:     "emacs",
			want:       "nano",
		},
		{
			name:   "visual overrides editor",
			visual: "vim",
			editor: "emacs",
			want:   "vim",
		},
		{
			name:   "editor used when visual empty",
			visual: "",
			editor: "emacs",
			want:   "emacs",
		},
		{
			name: "fallback vi",
			want: "vi",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("VISUAL", tt.visual)
			t.Setenv("EDITOR", tt.editor)
			got := resolveEditor(tt.configured)
			if got != tt.want {
				t.Errorf("resolveEditor(%q) = %q, want %q", tt.configured, got, tt.want)
			}
		})
	}
}

func TestParseEditor(t *testing.T) {
	t.Parallel()

	t.Run("with arguments", func(t *testing.T) {
		t.Parallel()
		cmd, err := parseEditor("go version", "/tmp/file.txt")
		if err != nil {
			t.Fatalf("parseEditor error = %v", err)
		}
		want := []string{"go", "version", "/tmp/file.txt"}
		if strings.Join(cmd.Args, " ") != strings.Join(want, " ") {
			t.Errorf("cmd.Args = %v, want %v", cmd.Args, want)
		}
	})

	t.Run("empty editor", func(t *testing.T) {
		t.Parallel()
		_, err := parseEditor("", "/tmp/file.txt")
		if err == nil {
			t.Error("expected error for empty editor")
		}
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()
		_, err := parseEditor("definitely-not-an-editor-12345", "/tmp/file.txt")
		if err == nil {
			t.Error("expected error when editor executable not found")
		}
	})
}

func TestWriteReadTempFile(t *testing.T) {
	t.Parallel()

	content := "hello\nexternal editor\n"
	path, err := writeTempFile(content)
	if err != nil {
		t.Fatalf("writeTempFile error = %v", err)
	}
	if path == "" {
		t.Fatal("writeTempFile returned empty path")
	}
	defer func() { _ = os.Remove(path) }()

	if !strings.HasPrefix(filepath.Base(path), "kimi-lite-editor-") {
		t.Errorf("unexpected temp file name: %s", path)
	}

	got, err := readTempFile(path)
	if err != nil {
		t.Fatalf("readTempFile error = %v", err)
	}
	if got != content {
		t.Errorf("readTempFile = %q, want %q", got, content)
	}
}

func TestHandleEditorFinished(t *testing.T) {
	t.Parallel()

	m := New(styles.New("dark"), DefaultKeyMap(), 100)
	m.SetValue("before")

	content := "edited content"
	path, err := writeTempFile(content)
	if err != nil {
		t.Fatalf("writeTempFile error = %v", err)
	}
	defer func() { _ = os.Remove(path) }()

	m.handleEditorFinished(editorFinishedMsg{path: path, err: nil})

	if m.Value() != content {
		t.Errorf("Value = %q, want %q", m.Value(), content)
	}

	// Temp file should have been removed.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("temp file should be removed after handling editor finished")
	}
}

func TestHandleEditorFinished_KeepsBufferOnError(t *testing.T) {
	t.Parallel()

	m := New(styles.New("dark"), DefaultKeyMap(), 100)
	m.SetValue("before")

	m.handleEditorFinished(editorFinishedMsg{path: "/tmp/does-not-exist", err: os.ErrNotExist})

	if m.Value() != "before" {
		t.Errorf("Value = %q, want %q", m.Value(), "before")
	}
}

func TestExternalEditorKeyReturnsCommand(t *testing.T) {
	t.Parallel()

	m := New(styles.New("dark"), DefaultKeyMap(), 100)
	m.SetValue("draft")

	// Use a non-interactive editor so that the command can be constructed
	// successfully without requiring user input.
	editor := "cat"
	if runtime.GOOS == "windows" {
		editor = "type"
	}
	m.SetEditor(editor)

	cmd := m.UpdateMsg(tea.KeyMsg{Type: tea.KeyCtrlG})
	if cmd == nil {
		t.Fatal("expected a command for external editor key")
	}

	// The returned command is a tea.ExecProcess command; executing it directly
	// in tests yields the internal exec message without running the process.
	msg := cmd()
	if msg == nil {
		t.Fatal("expected non-nil message from external editor command")
	}
}

func TestConfigurableKeyMap_ExternalEditor(t *testing.T) {
	t.Parallel()

	cfg := api.KeybindingConfig{ExternalEditor: "ctrl+e"}
	km := ConfigurableKeyMap(cfg)
	if len(km.ExternalEditor.Keys()) != 1 || km.ExternalEditor.Keys()[0] != "ctrl+e" {
		t.Errorf("ExternalEditor keys = %v, want [ctrl+e]", km.ExternalEditor.Keys())
	}
}
