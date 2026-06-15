package input

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// editorFinishedMsg is emitted when the external editor process exits.
type editorFinishedMsg struct {
	path string
	err  error
}

// writeTempFileHooks allows tests to inject failures for individual file-system
// operations performed by writeTempFile without using mutable global state.
type writeTempFileHooks struct {
	create func(string, string) (*os.File, error)
	write  func(*os.File, string) (int, error)
	close  func(*os.File) error
}

func defaultWriteTempFileHooks() writeTempFileHooks {
	return writeTempFileHooks{
		create: os.CreateTemp,
		write:  func(f *os.File, s string) (int, error) { return f.WriteString(s) },
		close:  func(f *os.File) error { return f.Close() },
	}
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

// parseShellArgs splits a string by whitespace while respecting single and
// double quotes. It returns an error for unterminated quotes.
func parseShellArgs(s string) ([]string, error) {
	var args []string
	var current strings.Builder
	var inSingle, inDouble bool
	var escaped bool

	flush := func() {
		if current.Len() > 0 {
			args = append(args, current.String())
			current.Reset()
		}
	}

	for _, r := range s {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if inSingle {
			if r == '\'' {
				inSingle = false
			} else {
				current.WriteRune(r)
			}
			continue
		}
		if inDouble {
			if r == '"' {
				inDouble = false
			} else {
				current.WriteRune(r)
			}
			continue
		}
		switch r {
		case '"':
			inDouble = true
		case '\'':
			inSingle = true
		case ' ', '\t', '\n', '\r':
			flush()
		default:
			current.WriteRune(r)
		}
	}

	if inSingle || inDouble {
		return nil, fmt.Errorf("unterminated quote in editor command")
	}
	if escaped {
		current.WriteRune('\\')
	}
	flush()
	return args, nil
}

// parseEditor splits an editor command into name and arguments and appends the
// file path as the final argument. It validates that the editor executable can
// be located.
func parseEditor(ctx context.Context, editor, path string) (*exec.Cmd, error) {
	parts, err := parseShellArgs(editor)
	if err != nil {
		return nil, fmt.Errorf("parse editor command: %w", err)
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("editor command is empty")
	}
	name := parts[0]
	if _, err := exec.LookPath(name); err != nil {
		return nil, fmt.Errorf("editor not found: %q: %w", name, err)
	}
	args := append(parts[1:], path)
	// #nosec G204 — the editor is resolved from user configuration or trusted
	// environment variables ($VISUAL, $EDITOR) and is a deliberate user-facing
	// subprocess launch.
	return exec.CommandContext(ctx, name, args...), nil
}

// writeTempFile writes content to a temporary file and returns its path. The
// optional hooks argument is used by tests to simulate file-system failures.
func writeTempFile(content string, hooks ...writeTempFileHooks) (string, error) {
	h := defaultWriteTempFileHooks()
	if len(hooks) > 0 {
		h = hooks[0]
	}

	f, err := h.create("", "kimi-lite-editor-*.txt")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	closeAndRemove := func(name string) {
		_ = f.Close()
		_ = os.Remove(name)
	}

	if _, err := h.write(f, content); err != nil {
		closeAndRemove(f.Name())
		return "", fmt.Errorf("write temp file: %w", err)
	}
	if err := h.close(f); err != nil {
		closeAndRemove(f.Name())
		return "", fmt.Errorf("close temp file: %w", err)
	}
	return f.Name(), nil
}

// readTempFile reads the contents of path as a string.
func readTempFile(path string) (string, error) {
	// #nosec G304 — path is a temporary file created and passed by the TUI.
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read temp file: %w", err)
	}
	return string(data), nil
}

// openExternalEditor writes the provided content to a temp file and returns a
// tea.Cmd that launches the configured editor via tea.ExecProcess. The temp
// file is removed after the editor exits. The supplied context controls the
// editor subprocess; when it is cancelled the editor is killed.
func (m *Model) openExternalEditor(ctx context.Context, editor, content string) tea.Cmd {
	if ctx == nil {
		ctx = context.Background()
	}

	path, err := writeTempFile(content)
	if err != nil {
		return func() tea.Msg {
			return editorFinishedMsg{path: path, err: err}
		}
	}

	cmd, err := parseEditor(ctx, resolveEditor(editor), path)
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
		slog.Warn("external editor: failed to read temp file", "path", msg.path, "error", err)
		return
	}
	m.textarea.SetValue(content)
	m.textarea.CursorEnd()
}
