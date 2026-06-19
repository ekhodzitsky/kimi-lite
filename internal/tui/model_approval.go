package tui

import (
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ekhodzitsky/kimi-lite/internal/core"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// approvalDiffComputedMsg carries the asynchronously computed inline diff for
// the current pending approval call.
type approvalDiffComputedMsg struct {
	CallID string
	Diff   string
	Err    error
}

// approvalStartRequest starts a new approval request under m.mu and clears any
// stale fullscreen diff state from a previous request.
func (m *Model) approvalStartRequest(calls []api.ToolCall, requestID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.approvalFullscreen = false
	m.approvalDiffContent = ""
	m.approvalDiffCallID = ""
	m.approvalDiffErr = nil
	m.approval.startRequest(calls, requestID)
}

// approvalHandleResponse records one approval decision under m.mu.
func (m *Model) approvalHandleResponse(resp ApprovalResponseMsg) (done bool, approvals map[string]api.ApprovalDecision, alwaysAll bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.approval.handleResponse(resp)
}

// approvalRequestID returns the active approval request ID under m.mu.
func (m *Model) approvalRequestID() int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.approval.requestID()
}

// approvalPending returns the current pending calls under m.mu.
func (m *Model) approvalPending() []api.ToolCall {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.approval.pending()
}

// approvalClear resets the approval controller under m.mu.
func (m *Model) approvalClear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.approval.clear()
}

// approvalIsActive reports whether there is an active approval request under m.mu.
func (m *Model) approvalIsActive() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.approval.isActive()
}

// approvalApproveCurrent produces a response for the current call under m.mu.
func (m *Model) approvalApproveCurrent(decision api.ApprovalDecision) (ApprovalResponseMsg, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.approval.approveCurrent(decision)
}

// approvalComputeDiffCmd returns a command that computes the diff preview for
// the current pending call. The result is delivered asynchronously via
// approvalDiffComputedMsg so the View() path never does file IO.
func (m *Model) approvalComputeDiffCmd() tea.Cmd {
	m.mu.RLock()
	call, ok := m.approval.currentCall()
	session := m.session
	protectedPaths := append([]string(nil), m.protectedPaths...)
	m.mu.RUnlock()

	if !ok || session == nil {
		return nil
	}

	return func() tea.Msg {
		diff, err := toolCallDiff(call, session.Path, protectedPaths)
		return approvalDiffComputedMsg{CallID: call.ID, Diff: diff, Err: err}
	}
}

func (m *Model) renderApprovalDialog(background string) string {
	m.mu.RLock()
	call, ok := m.approval.currentCall()
	session := m.session
	cachedCallID := m.approvalDiffCallID
	cachedDiff := m.approvalDiffContent
	cachedErr := m.approvalDiffErr
	m.mu.RUnlock()
	if !ok || session == nil {
		return background
	}
	var b strings.Builder
	b.WriteString("Tool call requires approval\n\n")

	total := len(m.approval.pending())
	if total > 1 {
		fmt.Fprintf(&b, "Call %d of %d\n\n", m.approval.currentIndex()+1, total)
	}

	fmt.Fprintf(&b, "Tool: %s\n", call.Name)

	var diff string
	var err error
	if cachedCallID == call.ID {
		diff = cachedDiff
		err = cachedErr
	}

	switch {
	case err == nil && diff != "":
		fmt.Fprintf(&b, "\n%s\n", diff)
	case err == nil:
		fmt.Fprintf(&b, "Arguments: %s\n", call.Arguments)
	case errors.Is(err, core.ErrDiffFileTooLarge):
		fmt.Fprintf(&b, "Arguments: %s\n(diff preview disabled: file too large)\n", call.Arguments)
	case errors.Is(err, core.ErrDiffPathBlocked):
		fmt.Fprintf(&b, "Arguments: %s\n(diff preview blocked)\n", call.Arguments)
	default:
		fmt.Fprintf(&b, "Arguments: %s\n(diff preview unavailable)\n", call.Arguments)
	}

	allowAlways := !core.IsNeverAutoApprove(call.Name)
	var opts []string
	opts = append(opts, "1. Allow")
	opts = append(opts, "2. Reject")
	if allowAlways {
		opts = append(opts, "3. Allow for this session")
	}
	opts = append(opts, "4. Diff")
	fmt.Fprintf(&b, "\n %s", strings.Join(opts, "   "))

	var keyOpts []string
	keyOpts = append(keyOpts, "keys: 1/2")
	if allowAlways {
		keyOpts = append(keyOpts, "3")
	} else {
		keyOpts = append(keyOpts, "-")
	}
	keyOpts = append(keyOpts, "4 | y/n")
	if allowAlways {
		keyOpts = append(keyOpts, "a")
	} else {
		keyOpts = append(keyOpts, "-")
	}
	keyOpts = append(keyOpts, "d | ctrl+e fullscreen")
	fmt.Fprintf(&b, "\n%s", strings.Join(keyOpts, "/"))

	dialog := m.styles.ApprovalDialog.Render(b.String())
	return overlayDialog(background, dialog, m.width, m.height)
}

// renderApprovalFullscreen renders a fullscreen diff preview overlay that fills
// the entire terminal area without a dialog border.
func (m *Model) renderApprovalFullscreen(background string) string {
	_ = background

	width := m.width
	if width < minContentWidth {
		width = minContentWidth
	}
	height := m.height
	if height < 3 {
		height = 3
	}

	header := "Diff preview"
	footer := "Esc or Ctrl+E to close"
	contentHeight := height - 2

	lines := strings.Split(m.approvalDiffContent, "\n")
	var contentLines []string
	for _, line := range lines {
		for ansi.StringWidth(line) > width {
			contentLines = append(contentLines, padOrTruncate(ansi.Cut(line, 0, width), width))
			line = ansi.Cut(line, width, ansi.StringWidth(line))
		}
		contentLines = append(contentLines, padOrTruncate(line, width))
	}

	if len(contentLines) > contentHeight {
		contentLines = contentLines[:contentHeight]
	}

	out := make([]string, 0, height)
	out = append(out, padOrTruncate(header, width))
	out = append(out, contentLines...)
	for len(out) < height-1 {
		out = append(out, strings.Repeat(" ", width))
	}
	out = append(out, padOrTruncate(footer, width))

	return strings.Join(out, "\n")
}

// padOrTruncate returns s padded or truncated to exactly width cells.
func padOrTruncate(s string, width int) string {
	w := ansi.StringWidth(s)
	switch {
	case w > width:
		return ansi.Cut(s, 0, width)
	case w < width:
		return s + strings.Repeat(" ", width-w)
	default:
		return s
	}
}

// toolCallDiff returns a diff preview for pending write_file or str_replace_file calls.
func toolCallDiff(call api.ToolCall, sandboxRoot string, protectedPaths []string) (string, error) {
	diff, err := core.ToolCallDiff(call, sandboxRoot, protectedPaths)
	if err != nil {
		return "", fmt.Errorf("tool call diff: %w", err)
	}
	return diff, nil
}
