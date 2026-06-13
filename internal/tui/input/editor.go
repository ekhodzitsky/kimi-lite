package input

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// editorFinishedMsg is emitted when the external editor process exits.
type editorFinishedMsg struct {
	path string
	err  error
}

// resolveEditor returns the editor command to use. The configured value takes
// precedence, then $VISUAL, then $EDITOR, with a final fallback to vi.
func resolveEditor(configured string) string {
	if configured != "" {
		return configured
	}
	if visual := os.Getenv("VISUAL"); visual != "" {
		return visual
	}
	if editor := os.Getenv("EDITOR"); editor != "" {
		return editor
	}
	return "vi"
}

// parseEditor splits an editor command into name and arguments and appends the
// file path as the final argument. It validates that the editor executable can
// be located.
func parseEditor(editor, path string) (*exec.Cmd, error) {
	parts := strings.Fields(editor)
	if len(parts) == 0 {
		return nil, fmt.Errorf("editor command is empty")
	}
	name := parts[0]
	if _, err := exec.LookPath(name); err != nil {
		return nil, fmt.Errorf("editor not found: %q: %w", name, err)
	}
	args := append(parts[1:], path)
	return exec.Command(name, args...), nil
}

// writeTempFile writes content to a temporary file and returns its path.
func writeTempFile(content string) (string, error) {
	f, err := os.CreateTemp("", "kimi-lite-editor-*.txt")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString(content); err != nil {
		return f.Name(), fmt.Errorf("write temp file: %w", err)
	}
	return f.Name(), nil
}

// readTempFile reads the contents of path as a string.
func readTempFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read temp file: %w", err)
	}
	return string(data), nil
}

// openExternalEditor writes the current textarea value to a temp file and
// returns a tea.Cmd that launches the configured editor via tea.ExecProcess.
// The temp file is removed after the editor exits.
func (m *Model) openExternalEditor(editor string) tea.Cmd {
	content := m.textarea.Value()
	path, err := writeTempFile(content)
	if err != nil {
		return func() tea.Msg {
			return editorFinishedMsg{path: path, err: err}
		}
	}

	cmd, err := parseEditor(resolveEditor(editor), path)
	if err != nil {
		_ = os.Remove(path)
		return func() tea.Msg {
			return editorFinishedMsg{path: "", err: err}
		}
	}

	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorFinishedMsg{path: path, err: err}
	})
}

// handleEditorFinished reads the saved file back into the textarea and removes
// the temp file. Errors are ignored; the previous buffer is preserved.
func (m *Model) handleEditorFinished(msg editorFinishedMsg) {
	if msg.path == "" {
		return
	}
	defer func() { _ = os.Remove(msg.path) }()

	if msg.err != nil {
		return
	}

	content, err := readTempFile(msg.path)
	if err != nil {
		return
	}
	m.textarea.SetValue(content)
	m.textarea.CursorEnd()
}
