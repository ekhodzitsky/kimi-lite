package input

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ekhodzitsky/kimi-lite/internal/tui/styles"
)

func TestHeight(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st, DefaultKeyMap(), 100)
	m.SetWidth(80)

	h1 := m.Height()
	if h1 <= 0 {
		t.Errorf("Height() = %d, want >0", h1)
	}

	m.SetValue("@cmd")
	m.SetFileCandidates([]string{"cmd/main.go"})
	m.detectMention()
	h2 := m.Height()
	if h2 <= h1 {
		t.Errorf("Height() with completion = %d, want > %d", h2, h1)
	}
}

func TestSetContext(t *testing.T) {
	t.Parallel()

	m := New(styles.New("dark"), DefaultKeyMap(), 100)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m.SetContext(ctx)
	if m.ctx != ctx {
		t.Error("SetContext did not store context")
	}
}

func TestCloseCompletion(t *testing.T) {
	t.Parallel()

	m := New(styles.New("dark"), DefaultKeyMap(), 100)
	m.SetWidth(80)
	m.SetFileCandidates([]string{"main.go"})
	m.SetValue("@main")
	m.detectMention()

	if !m.Completing() {
		t.Fatal("expected completion to be active")
	}

	m.CloseCompletion()
	if m.Completing() {
		t.Error("expected completion to be closed")
	}
}

func TestUpdateMsgHistoryEmpty(t *testing.T) {
	t.Parallel()

	m := New(styles.New("dark"), DefaultKeyMap(), 100)
	m.SetWidth(80)

	if cmd := m.UpdateMsg(tea.KeyPressMsg{Code: tea.KeyUp}); cmd != nil {
		t.Error("up with empty history should not produce a command")
	}
	if cmd := m.UpdateMsg(tea.KeyPressMsg{Code: tea.KeyDown}); cmd != nil {
		t.Error("down with empty history should not produce a command")
	}
	if cmd := m.UpdateMsg(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl}); cmd != nil {
		t.Error("ctrl+p with empty history should not produce a command")
	}
	if cmd := m.UpdateMsg(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl}); cmd != nil {
		t.Error("ctrl+n with empty history should not produce a command")
	}
}

func TestUpdateMsgHistoryCtrlPAndN(t *testing.T) {
	t.Parallel()

	m := New(styles.New("dark"), DefaultKeyMap(), 100)
	m.SetWidth(80)

	m.SetValue("first")
	m.UpdateMsg(tea.KeyPressMsg{Code: tea.KeyEnter})

	m.SetValue("draft")
	m.UpdateMsg(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	if m.Value() != "first" {
		t.Errorf("ctrl+p value = %q, want %q", m.Value(), "first")
	}

	m.UpdateMsg(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	if m.Value() != "draft" {
		t.Errorf("ctrl+n value = %q, want %q", m.Value(), "draft")
	}
}

func TestUpdateMsgMentionNavigation(t *testing.T) {
	t.Parallel()

	m := New(styles.New("dark"), DefaultKeyMap(), 100)
	m.SetWidth(80)
	m.SetFileCandidates([]string{"a.go", "b.go", "c.go"})
	m.SetValue("@")
	m.detectMention()

	if !m.Completing() {
		t.Fatal("expected completion to be active")
	}
	if m.mention.selected != 0 {
		t.Fatalf("selected = %d, want 0", m.mention.selected)
	}

	m.UpdateMsg(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.mention.selected != 1 {
		t.Errorf("after tab: selected = %d, want 1", m.mention.selected)
	}

	m.UpdateMsg(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.mention.selected != 2 {
		t.Errorf("after down: selected = %d, want 2", m.mention.selected)
	}

	m.UpdateMsg(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.mention.selected != 0 {
		t.Errorf("after down wrap: selected = %d, want 0", m.mention.selected)
	}

	m.UpdateMsg(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.mention.selected != 2 {
		t.Errorf("after up wrap: selected = %d, want 2", m.mention.selected)
	}

	m.UpdateMsg(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.mention.selected != 1 {
		t.Errorf("after shift+tab: selected = %d, want 1", m.mention.selected)
	}

	m.UpdateMsg(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	m.UpdateMsg(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.mention.selected != 2 {
		t.Errorf("after shift+tab wrap: selected = %d, want 2", m.mention.selected)
	}
}

func TestUpdateMsgMentionEscAndCtrlC(t *testing.T) {
	t.Parallel()

	m := New(styles.New("dark"), DefaultKeyMap(), 100)
	m.SetWidth(80)
	m.SetFileCandidates([]string{"main.go"})

	m.SetValue("@main")
	m.detectMention()
	if !m.Completing() {
		t.Fatal("expected completion active")
	}
	m.UpdateMsg(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.Completing() {
		t.Error("expected completion to close on Esc")
	}

	m.SetValue("@main")
	m.detectMention()
	m.UpdateMsg(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if m.Completing() {
		t.Error("expected completion to close on ctrl+c")
	}
}

func TestUpdateMsgExternalEditorWithContext(t *testing.T) {
	t.Parallel()

	m := New(styles.New("dark"), DefaultKeyMap(), 100)
	m.SetWidth(80)
	m.SetEditor("cat")
	m.SetContext(context.Background())

	cmd := m.UpdateMsg(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("expected external editor command")
	}
}

func TestWordAtCursorEmpty(t *testing.T) {
	t.Parallel()

	m := New(styles.New("dark"), DefaultKeyMap(), 100)
	m.SetWidth(80)

	word, start, end := m.wordAtCursor()
	if word != "" || start != 0 || end != 0 {
		t.Errorf("wordAtCursor() = %q, %d, %d, want empty, 0, 0", word, start, end)
	}
}

func TestWordAtCursorMultiByte(t *testing.T) {
	t.Parallel()

	m := New(styles.New("dark"), DefaultKeyMap(), 100)
	m.SetWidth(80)

	m.SetValue("日本語")
	m.textarea.CursorEnd()

	word, start, end := m.wordAtCursor()
	if word != "日本語" {
		t.Errorf("word = %q, want 日本語", word)
	}
	if start != 0 || end != len("日本語") {
		t.Errorf("range = %d,%d, want 0,%d", start, end, len("日本語"))
	}
}

func TestWordAtCursorMultiLine(t *testing.T) {
	t.Parallel()

	m := New(styles.New("dark"), DefaultKeyMap(), 100)
	m.SetWidth(80)

	m.SetValue("abc\ndef")
	m.textarea.CursorDown()
	m.textarea.CursorEnd()

	if pos := m.cursorPosition(); pos != 7 {
		t.Errorf("cursorPosition() = %d, want 7", pos)
	}

	word, start, end := m.wordAtCursor()
	if word != "def" {
		t.Errorf("word = %q, want def", word)
	}
	if start != 4 || end != 7 {
		t.Errorf("range = %d,%d, want 4,7", start, end)
	}
}

func TestCompletionView(t *testing.T) {
	t.Parallel()

	m := New(styles.New("dark"), DefaultKeyMap(), 100)
	m.SetWidth(80)

	if got := m.completionView(); got != "" {
		t.Errorf("completionView() with no mention = %q, want empty", got)
	}

	m.mention = &mentionState{candidates: []string{}, selected: 0}
	if got := m.completionView(); got != "" {
		t.Errorf("completionView() with empty candidates = %q, want empty", got)
	}

	m.styles = nil
	m.mention = &mentionState{candidates: []string{"a.go"}, selected: 0}
	if got := m.completionView(); got != "" {
		t.Errorf("completionView() with nil styles = %q, want empty", got)
	}

	m.styles = styles.New("dark")
	candidates := make([]string, 10)
	for i := range candidates {
		candidates[i] = string(rune('a'+i)) + ".go"
	}
	m.mention = &mentionState{candidates: candidates, selected: 0}
	view := m.completionView()
	if !strings.Contains(view, "a.go") || strings.Contains(view, "i.go") {
		t.Errorf("completionView() should render 8 candidates, got %q", view)
	}
}

func TestInsertCandidate(t *testing.T) {
	t.Parallel()

	m := New(styles.New("dark"), DefaultKeyMap(), 100)
	m.SetWidth(80)

	m.insertCandidate()
	if m.Value() != "" {
		t.Errorf("insertCandidate with nil mention should leave value empty, got %q", m.Value())
	}

	m.SetValue("@cmd")
	m.SetFileCandidates([]string{"cmd/main.go"})
	m.detectMention()
	m.insertCandidate()
	if m.Value() != "@cmd/main.go" {
		t.Errorf("value = %q, want @cmd/main.go", m.Value())
	}

	m.SetValue("@cmd")
	m.detectMention()
	m.mention.end = 100
	m.insertCandidate()
	if m.Value() != "@cmd/main.go" {
		t.Errorf("value with end beyond length = %q, want @cmd/main.go", m.Value())
	}
}

func TestFilterCandidatesDuplicate(t *testing.T) {
	t.Parallel()

	m := New(styles.New("dark"), DefaultKeyMap(), 100)
	m.SetFileCandidates([]string{"cmd/main.go", "cmd/main.go", "cmd/other.go"})

	got := m.filterCandidates("cmd")
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[0] != "cmd/main.go" || got[1] != "cmd/other.go" {
		t.Errorf("candidates = %v, want [cmd/main.go cmd/other.go]", got)
	}
}

func TestUpdateStylesNil(t *testing.T) {
	t.Parallel()

	m := New(nil, DefaultKeyMap(), 100)
	if m.styles != nil {
		t.Error("styles should remain nil when nil is passed")
	}
	m.updateStyles() // should not panic
}

func TestMentionDetectNoCandidates(t *testing.T) {
	t.Parallel()

	m := New(styles.New("dark"), DefaultKeyMap(), 100)
	m.SetWidth(80)
	m.SetFileCandidates([]string{})
	m.SetValue("@cmd")
	m.detectMention()
	if m.Completing() {
		t.Error("expected no completion when no candidates match")
	}
}

func TestUpdateMsgEditorFinished(t *testing.T) {
	t.Parallel()

	m := New(styles.New("dark"), DefaultKeyMap(), 100)
	m.SetWidth(80)
	m.SetValue("before")

	content := "edited content"
	path, err := writeTempFile(content)
	if err != nil {
		t.Fatalf("writeTempFile: %v", err)
	}

	m.UpdateMsg(editorFinishedMsg{path: path, err: nil})
	if m.Value() != content {
		t.Errorf("Value = %q, want %q", m.Value(), content)
	}
}

func TestUpdateMsgPassesToTextarea(t *testing.T) {
	t.Parallel()

	m := New(styles.New("dark"), DefaultKeyMap(), 100)
	m.SetWidth(80)
	m.SetValue("hello")

	// A non-key, non-editor-finished message should be forwarded to the
	// underlying textarea without panicking.
	_ = m.UpdateMsg(tea.WindowSizeMsg{Width: 100, Height: 30})
	if m.Value() != "hello" {
		t.Errorf("Value = %q, want %q", m.Value(), "hello")
	}
}
