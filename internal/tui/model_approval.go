package tui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ekhodzitsky/kimi-lite/internal/core"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// approvalStartRequest starts a new approval request under m.mu and clears any
// stale fullscreen diff state from a previous request.
func (m *Model) approvalStartRequest(calls []api.ToolCall, requestID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.approvalFullscreen = false
	m.approvalDiffContent = ""
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

func (m *Model) renderApprovalDialog(background string) string {
	m.mu.RLock()
	call, ok := m.approval.currentCall()
	session := m.session
	protectedPaths := append([]string(nil), m.protectedPaths...)
	m.mu.RUnlock()
	if !ok || session == nil {
		return background
	}
	var b strings.Builder
	b.WriteString("Tool call requires approval\n\n")
	fmt.Fprintf(&b, "Tool: %s\n", call.Name)
	diff, err := toolCallDiff(call, session.Path, protectedPaths)
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
	fmt.Fprintf(&b, "\n 1. yes  2. no  3. always  4. diff")
	fmt.Fprintf(&b, "\nkeys: 1/2/3/4 | y/n/a/d | ctrl+e fullscreen")

	dialog := m.styles.ApprovalDialog.Render(b.String())
	return overlayDialog(background, dialog, m.width, m.height)
}

// renderApprovalFullscreen renders a fullscreen diff preview overlay.
func (m *Model) renderApprovalFullscreen(background string) string {
	var b strings.Builder
	b.WriteString("Diff preview (Esc or Ctrl+E to close)\n\n")
	b.WriteString(m.approvalDiffContent)
	dialog := m.styles.ApprovalDialog.Render(b.String())
	return overlayDialog(background, dialog, m.width, m.height)
}

// toolCallDiff returns a diff preview for pending write_file or str_replace_file calls.
func toolCallDiff(call api.ToolCall, sandboxRoot string, protectedPaths []string) (string, error) {
	diff, err := core.ToolCallDiff(call, sandboxRoot, protectedPaths)
	if err != nil {
		return "", fmt.Errorf("tool call diff: %w", err)
	}
	return diff, nil
}
