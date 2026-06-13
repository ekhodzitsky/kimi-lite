package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestOverlayDialog_BasicCentering(t *testing.T) {
	t.Parallel()

	background := strings.Repeat(".", 20) + "\n" +
		strings.Repeat(".", 20) + "\n" +
		strings.Repeat(".", 20)
	dialog := "hello\nworld"

	const width, height = 20, 10
	got := overlayDialog(background, dialog, width, height)
	lines := strings.Split(got, "\n")

	if len(lines) != height {
		t.Fatalf("expected %d lines, got %d", height, len(lines))
	}

	dialogWidth := ansi.StringWidth("hello")
	startX := (width - dialogWidth) / 2
	startY := (height - 2) / 2

	for i, line := range lines {
		lineWidth := ansi.StringWidth(line)
		if lineWidth != width {
			t.Errorf("line %d width = %d, want %d: %q", i, lineWidth, width, line)
		}
	}

	if !strings.Contains(lines[startY], "hello") {
		t.Errorf("expected dialog line 0 at y=%d, got %q", startY, lines[startY])
	}
	if !strings.Contains(lines[startY+1], "world") {
		t.Errorf("expected dialog line 1 at y=%d, got %q", startY+1, lines[startY+1])
	}

	left := ansi.Cut(lines[startY], 0, startX)
	if strings.Contains(left, "hello") {
		t.Errorf("left segment should not contain dialog: %q", left)
	}
}

func TestOverlayDialog_WideRunesPreserved(t *testing.T) {
	t.Parallel()

	// Background is a single wide-rune repeated across the width.
	const width, height = 12, 5
	bgLine := strings.Repeat("你好", width/2)
	background := strings.Repeat(bgLine+"\n", height)
	dialog := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		Padding(0, 1).
		Render("OK")

	got := overlayDialog(background, dialog, width, height)
	lines := strings.Split(got, "\n")
	if len(lines) != height {
		t.Fatalf("expected %d lines, got %d", height, len(lines))
	}

	// Lines that are not covered by the dialog should still contain the
	// original wide runes and have the correct width.
	dialogHeight := lipgloss.Height(dialog)
	startY := (height - dialogHeight) / 2
	for i, line := range lines {
		lineWidth := ansi.StringWidth(line)
		if lineWidth != width {
			t.Errorf("line %d width = %d, want %d", i, lineWidth, width)
		}
		if i < startY || i >= startY+dialogHeight {
			if !strings.Contains(line, "你") {
				t.Errorf("line %d should preserve background wide runes, got %q", i, line)
			}
		}
	}
}

func TestOverlayDialog_EmojiBackground(t *testing.T) {
	t.Parallel()

	const width, height = 10, 5
	bgLine := strings.Repeat("🙂", width/2)
	background := strings.Repeat(bgLine+"\n", height)
	dialog := "hi"

	got := overlayDialog(background, dialog, width, height)
	lines := strings.Split(got, "\n")

	dialogWidth := ansi.StringWidth(dialog)
	startX := (width - dialogWidth) / 2
	startY := (height - 1) / 2

	// Dialog line should contain the dialog text.
	if !strings.Contains(lines[startY], "hi") {
		t.Errorf("expected dialog at y=%d, got %q", startY, lines[startY])
	}

	// Non-dialog lines should still contain the emoji background.
	for i, line := range lines {
		if i == startY {
			continue
		}
		if !strings.Contains(line, "🙂") {
			t.Errorf("line %d should preserve emoji background, got %q", i, line)
		}
	}

	// The cells around the dialog should still be emoji.
	left := ansi.Cut(lines[startY], 0, startX)
	if !strings.Contains(left, "🙂") {
		t.Errorf("left segment should preserve emoji background, got %q", left)
	}
}

func TestOverlayDialog_NarrowTerminal(t *testing.T) {
	t.Parallel()

	const width, height = 5, 5
	background := strings.Repeat("x", width) + "\n" + strings.Repeat("x", width)
	dialog := "hello world" // wider than the terminal

	got := overlayDialog(background, dialog, width, height)
	lines := strings.Split(got, "\n")

	if len(lines) != height {
		t.Fatalf("expected %d lines, got %d", height, len(lines))
	}

	// The dialog should be left-aligned because it does not fit.
	for i, line := range lines {
		lineWidth := ansi.StringWidth(line)
		if lineWidth != width {
			t.Errorf("line %d width = %d, want %d", i, lineWidth, width)
		}
	}

	if !strings.HasPrefix(lines[2], "hello") {
		t.Errorf("expected dialog left-aligned in narrow terminal, got %q", lines[2])
	}
}

func TestOverlayDialog_FullWidthDialogBoundary(t *testing.T) {
	t.Parallel()

	const width, height = 10, 3
	background := strings.Repeat("x", width) + "\n" + strings.Repeat("x", width) + "\n" + strings.Repeat("x", width)
	dialog := strings.Repeat("o", width)

	got := overlayDialog(background, dialog, width, height)
	lines := strings.Split(got, "\n")

	startY := height / 2
	if lines[startY] != dialog {
		t.Errorf("expected full-width dialog to replace entire line, got %q", lines[startY])
	}

	// Other lines should remain background.
	for i, line := range lines {
		if i == startY {
			continue
		}
		if line != strings.Repeat("x", width) {
			t.Errorf("line %d should remain background, got %q", i, line)
		}
	}
}

func TestOverlayDialog_ShortHeightClipsDialog(t *testing.T) {
	t.Parallel()

	const width, height = 20, 1
	background := strings.Repeat(".", width)
	dialog := "line1\nline2\nline3"

	got := overlayDialog(background, dialog, width, height)
	lines := strings.Split(got, "\n")

	if len(lines) != height {
		t.Fatalf("expected %d lines, got %d", height, len(lines))
	}

	// Only the first dialog line should be visible, left/center aligned.
	if !strings.Contains(lines[0], "line1") {
		t.Errorf("expected first dialog line visible, got %q", lines[0])
	}
	if strings.Contains(got, "line2") || strings.Contains(got, "line3") {
		t.Errorf("dialog lines beyond height should be clipped")
	}
}
