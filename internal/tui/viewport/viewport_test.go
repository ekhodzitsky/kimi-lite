package viewport

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ekhodzitsky/kimi-lite/internal/tui/styles"
)

func TestNew(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st)

	if m == nil {
		t.Fatal("New() returned nil")
	}
	if !m.autoScroll {
		t.Error("autoScroll should be true by default")
	}
	if m.vp.MouseWheelEnabled != false {
		t.Error("MouseWheelEnabled should be false so the wrapper handles wheel events")
	}
}

func TestInit(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st)
	cmd := m.Init()
	if cmd != nil {
		t.Error("Init() should return nil")
	}
}

func TestSetSize(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st)
	m.SetSize(100, 30)

	if m.width != 100 {
		t.Errorf("width = %d, want 100", m.width)
	}
	if m.height != 30 {
		t.Errorf("height = %d, want 30", m.height)
	}
	if m.vp.Width() != 100 {
		t.Errorf("vp.Width = %d, want 100", m.vp.Width())
	}
	if m.vp.Height() != 30 {
		t.Errorf("vp.Height = %d, want 30", m.vp.Height())
	}
}

func TestSetContent(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st)
	m.SetSize(80, 20)

	m.SetContent("hello world")
	if !m.AtBottom() {
		t.Error("should be at bottom after SetContent with auto-scroll")
	}
}

func TestScrollToTop(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st)
	m.SetSize(80, 5)

	m.SetContent("line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10")
	m.GotoTop()
	if m.AtBottom() {
		t.Error("should not be at bottom after GotoTop")
	}
	if m.autoScroll {
		t.Error("autoScroll should be false after GotoTop")
	}
}

func TestScrollToBottom(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st)
	m.SetSize(80, 5)

	m.SetContent("line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10")
	m.GotoTop()
	m.GotoBottom()
	if !m.AtBottom() {
		t.Error("should be at bottom after GotoBottom")
	}
	if !m.autoScroll {
		t.Error("autoScroll should be true after GotoBottom")
	}
}

func TestScrollPercent(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st)
	m.SetSize(80, 5)

	m.SetContent("line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10")
	m.GotoBottom()
	pct := m.ScrollPercent()
	if pct != 1.0 {
		t.Errorf("ScrollPercent() = %f, want 1.0", pct)
	}

	m.GotoTop()
	pct = m.ScrollPercent()
	if pct != 0.0 {
		t.Errorf("ScrollPercent() after top = %f, want 0.0", pct)
	}
}

func TestKeyNavigation(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st)
	m.SetSize(80, 5)
	m.SetContent("line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10")
	m.GotoBottom()

	// Page up
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	vm := updated.(*Model)
	if vm.autoScroll {
		t.Error("autoScroll should be false after page up")
	}

	// Page down to bottom
	updated, _ = vm.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	vm = updated.(*Model)
	if !vm.AtBottom() {
		t.Error("should be at bottom after page down from near bottom")
	}

	// Home
	updated, _ = vm.Update(tea.KeyPressMsg{Code: tea.KeyHome})
	vm = updated.(*Model)
	if vm.autoScroll {
		t.Error("autoScroll should be false after home")
	}

	// End
	updated, _ = vm.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	vm = updated.(*Model)
	if !vm.autoScroll {
		t.Error("autoScroll should be true after end")
	}
}

func TestMouseScroll(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st)
	m.SetSize(80, 5)
	m.SetContent("line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10")
	m.GotoBottom()

	// Scroll up
	updated, _ := m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	vm := updated.(*Model)
	if vm.autoScroll {
		t.Error("autoScroll should be false after scroll up")
	}

	// Scroll down to bottom
	updated, _ = vm.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	vm = updated.(*Model)
	if !vm.AtBottom() {
		t.Error("should be at bottom after scroll down")
	}
}

func TestScrollIndicatorVisible(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st)
	m.SetSize(80, 5)
	m.SetContent("line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10")
	m.GotoTop()

	view := m.View()
	if !strings.Contains(view.Content, "▼") {
		t.Error("View should contain scroll indicator when not at bottom")
	}

	m.GotoBottom()
	view = m.View()
	// When at bottom, the indicator logic may still show it depending on viewport internals
	// We just verify View() doesn't panic
	_ = view.Content
}

func TestScrollIndicatorPreservesLastLine(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st)
	m.SetSize(80, 5)
	// With height 5, GotoTop shows the first 5 lines; "VISIBLE_BOTTOM" is the
	// last rendered line and must not be overwritten by the scroll indicator.
	m.SetContent("line1\nline2\nline3\nline4\nVISIBLE_BOTTOM\nline6\nline7\nline8")
	m.GotoTop()

	rendered := ansi.Strip(m.View().Content)
	if !strings.Contains(rendered, "VISIBLE_BOTTOM") {
		t.Error("View should preserve the last rendered content line alongside the indicator")
	}
	if !strings.Contains(rendered, "▼") {
		t.Error("View should contain scroll indicator when not at bottom")
	}
}

func TestMouseWheelSingleScrollDistance(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st)
	m.SetSize(80, 5)
	m.SetContent("1\n2\n3\n4\n5\n6\n7\n8\n9\n10")
	m.GotoBottom()

	before := m.vp.YOffset()
	updated, _ := m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	vm := updated.(*Model)
	after := vm.vp.YOffset()

	want := before - mouseWheelScrollLines
	if after != want {
		t.Errorf("YOffset = %d, want %d (scrolled %d lines)", after, want, before-after)
	}
}
