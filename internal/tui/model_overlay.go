package tui

import (
	"strings"
)

// renderPlanPanel renders the plan approval overlay.
func (m *Model) renderPlanPanel(background string) string {
	var b strings.Builder
	b.WriteString("Plan requires approval\n\n")
	innerW := m.width - 8
	if innerW < minContentWidth {
		innerW = minContentWidth
	}
	b.WriteString(wordWrap(m.planRequest, innerW))
	b.WriteString("\n\n[y] yes  [n] no")
	dialog := m.styles.PlanPanel.Render(b.String())
	return overlayDialog(background, dialog, m.width, m.height)
}

func (m *Model) renderSteerOverlay(background string) string {
	var b strings.Builder
	b.WriteString("Steer the response\n\n")
	innerW := m.width - 8
	if innerW < minContentWidth {
		innerW = minContentWidth
	}
	if m.steerInput == "" {
		b.WriteString(wordWrap("(type a follow-up instruction)", innerW))
	} else {
		b.WriteString(wordWrap(m.steerInput, innerW))
	}
	b.WriteString("\n\n[Enter] send  [Esc] cancel")
	dialog := m.styles.SteerOverlay.Render(b.String())
	return overlayDialog(background, dialog, m.width, m.height)
}
