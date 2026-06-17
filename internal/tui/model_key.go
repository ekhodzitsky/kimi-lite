package tui

import (
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/ekhodzitsky/kimi-lite/internal/tui/help"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func (m *Model) handleKeyMsg(msg tea.KeyPressMsg) []tea.Cmd {
	var cmds []tea.Cmd

	// Help overlay takes precedence while it is open.
	if m.showHelp {
		if help.CloseKeys(msg.String()) {
			m.showHelp = false
			return nil
		}
		cmd := m.helpPanel.UpdateMsg(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		return cmds
	}

	// Fullscreen diff preview closes on Esc or Ctrl+E.
	if m.approvalFullscreen {
		if msg.String() == "esc" || msg.String() == "ctrl+e" {
			m.approvalFullscreen = false
			m.approvalDiffContent = ""
		}
		return cmds
	}

	// Steering overlay takes precedence while it is open.
	if m.steerOpen {
		return m.handleSteerKeyMsg(msg)
	}

	// Shift+Tab toggles input plan mode when the input is focused.
	if msg.String() == "shift+tab" && m.focused == focusInput {
		m.input.TogglePlanMode()
		return cmds
	}

	// Plan approval panel takes precedence while it is open.
	if m.planPending {
		switch msg.String() {
		case "y":
			return append(cmds, func() tea.Msg { return PlanApprovalMsg{Approved: true} })
		case "n":
			return append(cmds, func() tea.Msg { return PlanApprovalMsg{Approved: false} })
		}
		return cmds
	}

	// Approval dialog takes precedence when waiting for approval.
	if m.state == api.TurnWaitingApproval {
		switch msg.String() {
		case "1":
			if resp, ok := m.approvalApproveCurrent(api.ApprovalYes); ok {
				cmds = append(cmds, func() tea.Msg { return resp })
			}
			return cmds
		case "2":
			if resp, ok := m.approvalApproveCurrent(api.ApprovalNo); ok {
				cmds = append(cmds, func() tea.Msg { return resp })
			}
			return cmds
		case "3":
			if resp, ok := m.approvalApproveCurrent(api.ApprovalAlways); ok {
				cmds = append(cmds, func() tea.Msg { return resp })
			}
			return cmds
		case "4", m.config.Keybindings.ApproveDiff:
			if resp, ok := m.approvalApproveCurrent(api.ApprovalDiff); ok {
				cmds = append(cmds, func() tea.Msg { return resp })
			}
			return cmds
		case m.config.Keybindings.ApproveYes:
			if resp, ok := m.approvalApproveCurrent(api.ApprovalYes); ok {
				cmds = append(cmds, func() tea.Msg { return resp })
			}
			return cmds
		case m.config.Keybindings.ApproveNo:
			if resp, ok := m.approvalApproveCurrent(api.ApprovalNo); ok {
				cmds = append(cmds, func() tea.Msg { return resp })
			}
			return cmds
		case m.config.Keybindings.ApproveAlways:
			if resp, ok := m.approvalApproveCurrent(api.ApprovalAlways); ok {
				cmds = append(cmds, func() tea.Msg { return resp })
			}
			return cmds
		case "ctrl+e":
			m.mu.RLock()
			call, ok := m.approval.currentCall()
			session := m.session
			protectedPaths := append([]string(nil), m.protectedPaths...)
			m.mu.RUnlock()
			if !ok || session == nil {
				return cmds
			}
			diff, err := toolCallDiff(call, session.Path, protectedPaths)
			if err == nil && diff != "" {
				m.approvalFullscreen = true
				m.approvalDiffContent = diff
			}
			return cmds
		}
	}

	switch msg.String() {
	case m.config.Keybindings.Quit:
		cmds = append(cmds, tea.Quit)
	case m.config.Keybindings.Cancel:
		// If the user has typed a draft, clear it first instead of cancelling
		// the active stream. A second Cancel then stops the stream.
		if m.input.Value() != "" {
			m.input.SetValue("")
			break
		}
		m.mu.Lock()
		state := m.state
		cancel := m.streamCancel
		m.mu.Unlock()
		if state == api.TurnThinking || state == api.TurnStreaming {
			if cancel != nil {
				cancel()
			}
			m.mu.Lock()
			m.streamCh = nil
			m.streamCancel = nil
			m.streamCanceled = true
			m.mu.Unlock()
			m.setState(api.TurnIdle)
		}
	case m.config.Keybindings.FocusNext:
		if cmd := m.cycleFocus(1); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case m.config.Keybindings.FocusPrev:
		if cmd := m.cycleFocus(-1); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case m.config.Keybindings.Yolo:
		if m.approvalModeSetter != nil {
			m.mu.Lock()
			if m.approvalMode == approvalModeAuto {
				m.approvalMode = approvalModeYolo
			} else {
				m.approvalMode = approvalModeAuto
			}
			mode := m.approvalMode
			m.mu.Unlock()
			m.approvalModeSetter(mode)
		}
	}

	if steerKey(msg, m.config.Keybindings.Steer) {
		if m.state == api.TurnStreaming || m.state == api.TurnThinking {
			m.steerOpen = true
		}
	}

	return cmds
}

// steerKey reports whether msg is the configured steering key. An empty config
// value defaults to ctrl+s for backward compatibility.
func steerKey(msg tea.KeyPressMsg, configured string) bool {
	key := configured
	if key == "" {
		key = "ctrl+s"
	}
	return msg.String() == key
}

func (m *Model) handleSteerKeyMsg(msg tea.KeyPressMsg) []tea.Cmd {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.steerOpen = false
		m.steerInput = ""
		return nil
	case "enter":
		content := strings.TrimSpace(m.steerInput)
		if content == "" {
			return nil
		}
		m.steerOpen = false
		m.steerInput = ""
		return []tea.Cmd{func() tea.Msg { return SteerMsg{Content: content} }}
	case "backspace", "ctrl+h":
		if m.steerInput != "" {
			runes := []rune(m.steerInput)
			m.steerInput = string(runes[:len(runes)-1])
		}
		return nil
	}

	if text := appendableKeyText(msg); text != "" {
		m.steerInput += text
	}
	return nil
}

func appendableKeyText(msg tea.KeyPressMsg) string {
	if msg.Text == "" {
		return ""
	}
	for _, r := range msg.Text {
		if unicode.IsControl(r) {
			return ""
		}
	}
	return msg.Text
}

func (m *Model) handleMouseMsg(msg tea.MouseReleaseMsg) {
	if msg.Button != tea.MouseLeft {
		return
	}

	l := m.layout()
	welcomeHeight := m.welcomeHeight()
	vpEnd := welcomeHeight + l.vpHeight

	if msg.Y >= vpEnd && msg.Y < l.statusY {
		m.focused = focusInput
	} else if msg.Y >= welcomeHeight && msg.Y < vpEnd {
		m.focused = focusViewport
	}
}
