package tui

import (
	"fmt"
	"strings"
)

// planPanelMaxHeight returns the maximum number of wrapped content lines the
// plan panel can display inside the terminal while leaving room for its title,
// border, and choice footer.
func (m *Model) planPanelMaxHeight() int {
	h := m.height - 6
	if h < 3 {
		return 3
	}
	return h
}

// renderPlanPanel renders the plan approval overlay with a capped height and an
// internal scroll offset.
func (m *Model) renderPlanPanel(background string) string {
	var b strings.Builder
	b.WriteString("Plan requires approval\n\n")

	innerW := m.width - 8
	if innerW < minContentWidth {
		innerW = minContentWidth
	}

	wrapped := wordWrap(m.planRequest, innerW)
	lines := strings.Split(wrapped, "\n")
	maxHeight := m.planPanelMaxHeight()
	offset := m.planScrollOffset
	if offset < 0 {
		offset = 0
	}
	if offset > len(lines) {
		offset = len(lines)
	}
	end := offset + maxHeight
	if end > len(lines) {
		end = len(lines)
	}

	for _, line := range lines[offset:end] {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if len(lines) > maxHeight {
		fmt.Fprintf(&b, "\n(%d more lines)\n", len(lines)-maxHeight)
	}

	b.WriteString("\n[Enter] approve  [Esc] reject  [y] yes  [n] no")
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

	runes := []rune(m.steerInput)
	cursor := m.steerCursor
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(runes) {
		cursor = len(runes)
	}

	if m.steerInput == "" {
		placeholder := "(type a follow-up instruction)"
		b.WriteString(wordWrap(placeholder, innerW))
		b.WriteString("▌")
	} else {
		before := string(runes[:cursor])
		after := string(runes[cursor:])
		line := before + "▌" + after
		b.WriteString(wordWrap(line, innerW))
	}

	b.WriteString("\n\n[Enter] send  [Esc] cancel")
	dialog := m.styles.SteerOverlay.Render(b.String())
	return overlayDialog(background, dialog, m.width, m.height)
}
