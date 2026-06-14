package input

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ekhodzitsky/kimi-lite/internal/tui/styles"
)

func TestWriteTempFileCreateTempError(t *testing.T) {
	t.Parallel()

	path, err := writeTempFile("content", writeTempFileHooks{
		create: func(string, string) (*os.File, error) { return nil, errors.New("create failed") },
		write:  defaultWriteTempFileHooks().write,
		close:  defaultWriteTempFileHooks().close,
	})
	if err == nil {
		t.Error("expected error when create fails")
	}
	if path != "" {
		t.Errorf("path = %q, want empty", path)
	}
}

func TestWriteTempFileWriteError(t *testing.T) {
	t.Parallel()

	path, err := writeTempFile("content", writeTempFileHooks{
		create: defaultWriteTempFileHooks().create,
		write:  func(f *os.File, s string) (int, error) { return 0, errors.New("write failed") },
		close:  defaultWriteTempFileHooks().close,
	})
	if err == nil {
		t.Error("expected error when write fails")
	}
	if path != "" {
		t.Errorf("path = %q, want empty", path)
	}
}

func TestWriteTempFileCloseError(t *testing.T) {
	t.Parallel()

	path, err := writeTempFile("content", writeTempFileHooks{
		create: defaultWriteTempFileHooks().create,
		write:  defaultWriteTempFileHooks().write,
		close:  func(f *os.File) error { return errors.New("close failed") },
	})
	if err == nil {
		t.Error("expected error when close fails")
	}
	if path != "" {
		t.Errorf("path = %q, want empty", path)
	}
}

func TestReadTempFileError(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "missing-file")
	if _, err := readTempFile(path); err == nil {
		t.Error("expected error for missing file")
	}
}

func TestHandleEditorFinishedEmptyPath(t *testing.T) {
	t.Parallel()

	m := New(styles.New("dark"), DefaultKeyMap(), 100)
	m.SetValue("before")

	m.handleEditorFinished(editorFinishedMsg{path: "", err: nil})
	if m.Value() != "before" {
		t.Errorf("Value = %q, want %q", m.Value(), "before")
	}
}

func TestHandleEditorFinishedReadError(t *testing.T) {
	t.Parallel()

	m := New(styles.New("dark"), DefaultKeyMap(), 100)
	m.SetValue("before")

	// Reading a directory returns an error, exercising the readTempFile error path.
	m.handleEditorFinished(editorFinishedMsg{path: t.TempDir(), err: nil})
	if m.Value() != "before" {
		t.Errorf("Value = %q, want %q", m.Value(), "before")
	}
}

func TestOpenExternalEditorWriteTempError(t *testing.T) {
	dir := t.TempDir()
	badDir := filepath.Join(dir, "bad")
	if err := os.Mkdir(badDir, 0o000); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	defer func() { _ = os.Chmod(badDir, 0o755) }()

	t.Setenv("TMPDIR", badDir)

	m := New(styles.New("dark"), DefaultKeyMap(), 100)
	m.SetValue("draft")
	cmd := m.openExternalEditor(context.Background(), "cat")
	if cmd == nil {
		t.Fatal("expected a command even when temp file creation fails")
	}

	msg, ok := cmd().(editorFinishedMsg)
	if !ok {
		t.Fatalf("expected editorFinishedMsg, got %T", cmd())
	}
	if msg.err == nil {
		t.Error("expected error in editorFinishedMsg")
	}
}

func TestOpenExternalEditorParseEditorError(t *testing.T) {
	t.Parallel()

	m := New(styles.New("dark"), DefaultKeyMap(), 100)
	m.SetValue("draft")
	cmd := m.openExternalEditor(context.Background(), "definitely-not-an-editor-12345")
	if cmd == nil {
		t.Fatal("expected a command even when editor is not found")
	}

	msg, ok := cmd().(editorFinishedMsg)
	if !ok {
		t.Fatalf("expected editorFinishedMsg, got %T", cmd())
	}
	if msg.err == nil {
		t.Error("expected error in editorFinishedMsg")
	}
	if msg.path != "" {
		t.Errorf("path = %q, want empty", msg.path)
	}
}

func TestOpenExternalEditorNilContext(t *testing.T) {
	t.Parallel()

	m := New(styles.New("dark"), DefaultKeyMap(), 100)
	m.SetValue("draft")
	m.SetEditor("cat")

	cmd := m.openExternalEditor(nil, "cat")
	if cmd == nil {
		t.Fatal("expected a command with nil context")
	}
}

func TestExternalEditorKeyViaUpdateReturnsCommand(t *testing.T) {
	t.Parallel()

	m := New(styles.New("dark"), DefaultKeyMap(), 100)
	m.SetValue("draft")
	m.SetEditor("cat")

	cmd := m.UpdateMsg(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("expected external editor command")
	}
}
