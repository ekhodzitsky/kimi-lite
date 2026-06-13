package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// makeBackground returns a width x height string of repeated filler lines.
func makeBackground(width, height int, filler string) string {
	line := strings.Repeat(filler, width)
	return strings.Repeat(line+"\n", height)[:len(line)*height+max(0, height-1)]
}

// assertDimensions checks that every line of s has exactly the expected cell
// dimensions.
func assertDimensions(t *testing.T, name, s string, width, height int) {
	t.Helper()
	lines := strings.Split(s, "\n")
	if len(lines) != height {
		t.Errorf("%s: height = %d, want %d", name, len(lines), height)
	}
	for i, line := range lines {
		w := ansi.StringWidth(line)
		if w != width {
			t.Errorf("%s: line %d width = %d, want %d", name, i, w, width)
		}
	}
}

func TestOverlayDialog_CentersDialog(t *testing.T) {
	t.Parallel()

	const width, height = 30, 12
	bg := makeBackground(width, height, "·")
	dialog := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Render("Approve?")

	out := overlayDialog(bg, dialog, width, height)
	assertDimensions(t, "centers dialog", out, width, height)

	if !strings.Contains(out, "Approve?") {
		t.Error("expected rendered dialog to contain 'Approve?'")
	}

	// The dialog box is centered; at least one line should have content on
	// both sides of the visible dialog.
	lines := strings.Split(out, "\n")
	foundCentered := false
	for _, line := range lines {
		if strings.Contains(line, "Approve?") {
			if strings.Contains(line, "·") {
				foundCentered = true
				break
			}
		}
	}
	if !foundCentered {
		t.Error("expected dialog to be centered over the background")
	}
}

func TestOverlayDialog_WideRunes(t *testing.T) {
	t.Parallel()

	const width, height = 20, 8
	bg := makeBackground(width, height, "·")
	// "日本語" is three CJK runes, each two cells wide.
	dialog := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Render("日本語")

	out := overlayDialog(bg, dialog, width, height)
	assertDimensions(t, "wide runes", out, width, height)

	if !strings.Contains(out, "日本語") {
		t.Error("expected rendered dialog to contain the CJK text")
	}
}

func TestOverlayDialog_NarrowTerminalLocksWidth(t *testing.T) {
	t.Parallel()

	const width, height = 8, 6
	bg := makeBackground(width, height, "·")
	dialog := "This is a very wide dialog line"

	out := overlayDialog(bg, dialog, width, height)
	assertDimensions(t, "narrow terminal", out, width, height)

	// The output must be exactly the requested width even though the dialog
	// is much wider.
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if ansi.StringWidth(line) > width {
			t.Errorf("line %d exceeds terminal width: %q", i, line)
		}
	}
}

func TestOverlayDialog_DialogTallerThanBackground(t *testing.T) {
	t.Parallel()

	const width, height = 20, 3
	bg := makeBackground(width, height, "·")
	dialog := "line1\nline2\nline3\nline4\nline5"

	out := overlayDialog(bg, dialog, width, height)
	assertDimensions(t, "tall dialog", out, width, height)

	if !strings.Contains(out, "line1") {
		t.Error("expected first dialog line to be visible")
	}
}

func TestOverlayDialog_BackgroundPreservedAtEdges(t *testing.T) {
	t.Parallel()

	const width, height = 25, 9
	bg := makeBackground(width, height, "·")
	dialog := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Render("OK")

	out := overlayDialog(bg, dialog, width, height)

	lines := strings.Split(out, "\n")
	// The top-left and bottom-right corners should still be background filler.
	if !strings.Contains(lines[0], "·") {
		t.Error("expected top-left background to be preserved")
	}
	if !strings.Contains(lines[height-1], "·") {
		t.Error("expected bottom-right background to be preserved")
	}
}
