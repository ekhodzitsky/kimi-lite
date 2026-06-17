package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// welcomeHeight returns the rendered height of the welcome panel, or 0 when
// the transcript already contains messages.
func (m *Model) welcomeHeight() int {
	if len(m.messages) > 0 {
		return 0
	}
	return lipgloss.Height(m.welcome.View())
}

// activityHeight returns the rendered height of the activity panel, or 0 when
// the turn is not actively thinking, streaming, or running tools.
func (m *Model) activityHeight() int {
	return m.activity.Height()
}

// layoutRect holds computed geometry for a single frame.
type layoutRect struct {
	contentWidth int
	vpWidth      int
	vpHeight     int
	inputHeight  int
	statusY      int
}

// eq reports whether two layouts have identical geometry.
func (r layoutRect) eq(o layoutRect) bool {
	return r.contentWidth == o.contentWidth &&
		r.vpWidth == o.vpWidth &&
		r.vpHeight == o.vpHeight &&
		r.inputHeight == o.inputHeight &&
		r.statusY == o.statusY
}

// layout computes all geometry once, folding in min-content/min-viewport clamps.
func (m *Model) layout() layoutRect {
	contentWidth := m.width
	if contentWidth < minContentWidth {
		contentWidth = minContentWidth
	}
	m.welcome.SetSize(contentWidth)
	m.updateWelcomeData()
	m.activity.SetSize(contentWidth)
	m.updateActivity()
	welcomeHeight := m.welcomeHeight()
	inputHeight := m.inputHeight()
	activityHeight := m.activityHeight()
	vpHeight := m.height - statusHeight - inputHeight - welcomeHeight - activityHeight
	if vpHeight < minViewportHeight {
		vpHeight = minViewportHeight
	}
	statusY := welcomeHeight + vpHeight + inputHeight + activityHeight
	if statusY > m.height {
		statusY = m.height
	}
	return layoutRect{
		contentWidth: contentWidth,
		vpWidth:      contentWidth - viewportWidthPadding,
		vpHeight:     vpHeight,
		inputHeight:  inputHeight,
		statusY:      statusY,
	}
}

// applyLayoutSizes updates child component sizes without rebuilding the transcript.
func (m *Model) applyLayoutSizes(l layoutRect) {
	m.vp.SetSize(l.contentWidth, l.vpHeight)
	m.input.SetWidth(l.contentWidth)
	m.footer.SetSize(l.contentWidth)
	m.welcome.SetSize(l.contentWidth)
	m.activity.SetSize(l.contentWidth)
}

func (m *Model) contentWidth() int {
	return m.layout().contentWidth
}

func (m *Model) updateLayout() {
	l := m.layout()
	m.applyLayoutSizes(l)

	// If the layout geometry has not changed, the transcript is still valid.
	if l.eq(m.lastLayout) {
		return
	}

	m.mu.Lock()
	for _, msg := range m.messages {
		msg.SetWidth(l.vpWidth)
	}
	m.mu.Unlock()
	m.rebuildRenderedContent()
	m.lastLayout = l
}

// normalizeRect pads or truncates a rendered string so that every line has
// exactly width cells and the output contains exactly height lines.
func normalizeRect(s string, width, height int) string {
	lines := strings.Split(s, "\n")
	if len(lines) < height {
		lines = append(lines, make([]string, height-len(lines))...)
	}
	lines = lines[:height]
	for i, line := range lines {
		lineWidth := ansi.StringWidth(line)
		switch {
		case lineWidth > width:
			lines[i] = ansi.Cut(line, 0, width)
		case lineWidth < width:
			lines[i] = line + strings.Repeat(" ", width-lineWidth)
		}
	}
	return strings.Join(lines, "\n")
}

// wordWrap wraps s to the given display width, preserving existing newlines.
// It is copied from internal/tui/messages/messages.go for use by the plan
// panel so long plan lines are not truncated by the dialog overlay.
func wordWrap(s string, width int) string {
	if width <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	var out []string
	for _, line := range lines {
		for ansi.StringWidth(line) > width {
			out = append(out, ansi.Cut(line, 0, width))
			line = ansi.Cut(line, width, ansi.StringWidth(line))
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// overlayDialog composites a dialog box over a background, centering it both
// horizontally and vertically. The background is normalized to exactly width x
// height cells before the dialog is painted on top, so the rendered output is
// stable even on narrow terminals or when the dialog is larger than the
// background. Wide runes (CJK/emoji) are handled via ansi.Cut.
//
// The overlay is implemented with lipgloss v2's Canvas and Layer compositor
// instead of hand-rolled ANSI string splicing.
func overlayDialog(background string, dialog string, width int, height int) string {
	bgLines := strings.Split(background, "\n")

	// Normalize the background to exactly height lines.
	if len(bgLines) < height {
		bgLines = append(bgLines, make([]string, height-len(bgLines))...)
	}
	bgLines = bgLines[:height]

	// Normalize each line to exactly width cells.
	for i, line := range bgLines {
		lineWidth := ansi.StringWidth(line)
		switch {
		case lineWidth > width:
			bgLines[i] = ansi.Cut(line, 0, width)
		case lineWidth < width:
			bgLines[i] = line + strings.Repeat(" ", width-lineWidth)
		}
	}

	dialogHeight := lipgloss.Height(dialog)
	dialogWidth := lipgloss.Width(dialog)

	startY := (height - dialogHeight) / 2
	if startY < 0 {
		startY = 0
	}
	startX := (width - dialogWidth) / 2
	if startX < 0 {
		startX = 0
	}

	// Clamp the dialog line so the rendered output never exceeds the
	// requested width, even on very narrow terminals.
	maxDialogWidth := width - startX
	dialogLines := strings.Split(dialog, "\n")
	for i, dLine := range dialogLines {
		if ansi.StringWidth(dLine) > maxDialogWidth {
			dialogLines[i] = ansi.Cut(dLine, 0, maxDialogWidth)
		}
	}
	dialog = strings.Join(dialogLines, "\n")

	comp := lipgloss.NewCompositor(
		lipgloss.NewLayer(strings.Join(bgLines, "\n")),
		lipgloss.NewLayer(dialog).X(startX).Y(startY).Z(1),
	)
	rendered := lipgloss.NewCanvas(width, height).Compose(comp).Render()

	// Canvas.Render trims trailing whitespace. Re-normalize each line to the
	// requested width x height so callers get a stable, predictable rectangle.
	return normalizeRect(rendered, width, height)
}
