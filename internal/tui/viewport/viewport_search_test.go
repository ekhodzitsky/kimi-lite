package viewport

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/ekhodzitsky/kimi-lite/internal/tui/styles"
)

func TestSetSearchFindsMatches(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st)
	m.SetSize(80, 10)
	m.SetContent("hello world\nthis is a test\nhello again")

	count, idx := m.SetSearch("hello", false)
	if count != 2 {
		t.Errorf("match count = %d, want 2", count)
	}
	if idx != 0 {
		t.Errorf("current index = %d, want 0", idx)
	}
}

func TestSetSearchCaseInsensitive(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st)
	m.SetSize(80, 10)
	m.SetContent("Hello World\nHELLO again")

	count, _ := m.SetSearch("hello", false)
	if count != 2 {
		t.Errorf("case-insensitive count = %d, want 2", count)
	}

	count, _ = m.SetSearch("Hello", true)
	if count != 1 {
		t.Errorf("case-sensitive count = %d, want 1", count)
	}
}

func TestSetSearchNoMatches(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st)
	m.SetSize(80, 10)
	m.SetContent("hello world")

	count, idx := m.SetSearch("xyz", false)
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
	if idx != -1 {
		t.Errorf("index = %d, want -1", idx)
	}
}

func TestSearchNextPrevious(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st)
	m.SetSize(80, 10)
	m.SetContent("one\ntwo\nthree\ntwo\nfour")
	m.SetSearch("two", false)

	idx, count, ok := m.SearchNext()
	if !ok || idx != 1 || count != 2 {
		t.Fatalf("SearchNext() = (%d, %d, %v), want (1, 2, true)", idx, count, ok)
	}

	idx, count, ok = m.SearchNext()
	if !ok || idx != 0 || count != 2 {
		t.Fatalf("SearchNext() wrap = (%d, %d, %v), want (0, 2, true)", idx, count, ok)
	}

	idx, count, ok = m.SearchPrevious()
	if !ok || idx != 1 || count != 2 {
		t.Fatalf("SearchPrevious() = (%d, %d, %v), want (1, 2, true)", idx, count, ok)
	}
}

func TestSearchNextNoMatches(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st)
	m.SetSize(80, 10)
	m.SetContent("hello world")
	m.SetSearch("xyz", false)

	idx, count, ok := m.SearchNext()
	if ok {
		t.Errorf("SearchNext() ok = true, want false")
	}
	if idx != -1 || count != 0 {
		t.Errorf("SearchNext() = (%d, %d), want (-1, 0)", idx, count)
	}
}

func TestClearSearch(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st)
	m.SetSize(80, 10)
	m.SetContent("hello world")
	m.SetSearch("hello", false)

	m.ClearSearch()
	if m.SearchMatchCount() != 0 {
		t.Errorf("matches = %d after ClearSearch, want 0", m.SearchMatchCount())
	}
	if m.searchQuery != "" {
		t.Errorf("query = %q after ClearSearch, want empty", m.searchQuery)
	}
}

func TestScrollToLine(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st)
	m.SetSize(80, 5)
	m.SetContent("line0\nline1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9")

	m.ScrollToLine(7)
	start, _ := m.VisibleLineRange()
	// With height 5, visible height is 4 (indicator visible). Centering line 7
	// gives offset 7 - 4/2 = 5.
	if start != 5 {
		t.Errorf("VisibleLineRange start = %d, want 5", start)
	}
}

func TestSearchHighlightInView(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st)
	m.SetSize(80, 10)
	m.SetContent("hello world\nfoo bar\nhello world")
	m.SetSearch("hello", false)

	view := m.View().Content
	plain := ansi.Strip(view)
	if strings.Count(plain, "hello") != 2 {
		t.Errorf("expected 2 occurrences of hello in view, got %d", strings.Count(plain, "hello"))
	}
	// Highlighted current match should wrap the text in additional ANSI codes.
	if !strings.Contains(view, "\x1b[") {
		t.Error("view should contain ANSI escape sequences from highlighting")
	}
}

func TestSearchIgnoresANSI(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st)
	m.SetSize(80, 10)
	styled := st.UserMessage.Render("hello world")
	m.SetContent(styled)

	count, _ := m.SetSearch("hello", false)
	if count != 1 {
		t.Errorf("match count = %d, want 1 (search should ignore ANSI)", count)
	}
}

func TestRefreshSearchPreservesPosition(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st)
	m.SetSize(80, 10)
	m.SetContent("alpha\nbeta\ngamma\nbeta\ndelta")
	m.SetSearch("beta", false)
	m.SearchNext() // move to second beta (index 1, line 3)

	if m.searchCurrentIdx != 1 {
		t.Fatalf("setup: current index = %d, want 1", m.searchCurrentIdx)
	}

	// Simulate content change that keeps the second beta on line 3.
	m.SetContent("alpha\nbeta\ngamma\nbeta\ndelta")
	count, idx := m.RefreshSearch()
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
	if idx != 1 {
		t.Errorf("index = %d, want 1 (position should be preserved)", idx)
	}
}
