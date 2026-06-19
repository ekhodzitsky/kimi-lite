package input

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ekhodzitsky/kimi-lite/internal/tui/styles"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func TestNew(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	km := DefaultKeyMap()
	m := New(st, km, 100)

	if m == nil {
		t.Fatal("New() returned nil")
	}
	if m.styles == nil {
		t.Error("styles not set")
	}
	if m.Value() != "" {
		t.Errorf("initial value = %q, want empty", m.Value())
	}
}

func TestInit(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	km := DefaultKeyMap()
	m := New(st, km, 100)
	cmd := m.Init()
	if cmd == nil {
		t.Error("Init() should return a non-nil command")
	}
}

func TestSendMessage(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	km := DefaultKeyMap()
	m := New(st, km, 100)
	m.SetWidth(80)

	m.SetValue("hello world")
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if cmd == nil {
		t.Fatal("expected a command after sending")
	}

	msg, ok := cmd().(SendMsg)
	if !ok {
		t.Fatalf("expected SendMsg, got %T", cmd())
	}
	if msg.Content != "hello world" {
		t.Errorf("SendMsg.Content = %q, want %q", msg.Content, "hello world")
	}

	inp := updated.(*Model)
	if inp.Value() != "" {
		t.Error("input should be cleared after send")
	}
}

func TestSendEmptyMessage(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	km := DefaultKeyMap()
	m := New(st, km, 100)
	m.SetWidth(80)

	m.SetValue("   ")
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if cmd != nil {
		t.Error("sending empty message should not produce a command")
	}

	inp := updated.(*Model)
	if inp.Value() != "   " {
		t.Error("input should not be cleared for empty send")
	}
}

func TestNewline(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	km := DefaultKeyMap()
	m := New(st, km, 100)
	m.SetWidth(80)

	m.SetValue("line1")
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt})

	if cmd != nil {
		t.Error("newline should not produce a command")
	}

	inp := updated.(*Model)
	if !strings.Contains(inp.Value(), "\n") {
		t.Error("newline should insert a newline character")
	}
}

func TestHistoryNavigationUpDown(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	km := DefaultKeyMap()
	m := New(st, km, 100)
	m.SetWidth(80)

	// Send three messages
	for _, content := range []string{"first", "second", "third"} {
		m.SetValue(content)
		updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = updated.(*Model)
	}

	if len(m.history) != 3 {
		t.Fatalf("history length = %d, want 3", len(m.history))
	}

	// Press up - should show "third"
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m = updated.(*Model)
	if m.Value() != "third" {
		t.Errorf("after up: value = %q, want %q", m.Value(), "third")
	}

	// Press up again - should show "second"
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m = updated.(*Model)
	if m.Value() != "second" {
		t.Errorf("after up: value = %q, want %q", m.Value(), "second")
	}

	// Press down - should show "third"
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = updated.(*Model)
	if m.Value() != "third" {
		t.Errorf("after down: value = %q, want %q", m.Value(), "third")
	}

	// Press down again - should show draft (empty)
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = updated.(*Model)
	if m.Value() != "" {
		t.Errorf("after down to draft: value = %q, want empty", m.Value())
	}
}

func TestHistoryPreservesDraft(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	km := DefaultKeyMap()
	m := New(st, km, 100)
	m.SetWidth(80)

	m.SetValue("sent")
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(*Model)

	m.SetValue("draft")
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m = updated.(*Model)
	if m.Value() != "sent" {
		t.Errorf("history up: value = %q, want %q", m.Value(), "sent")
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = updated.(*Model)
	if m.Value() != "draft" {
		t.Errorf("history down should restore draft, got %q", m.Value())
	}
}

func TestHistoryCap(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	km := DefaultKeyMap()
	m := New(st, km, 3)
	m.SetWidth(80)

	for _, content := range []string{"a", "b", "c", "d"} {
		m.SetValue(content)
		updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = updated.(*Model)
	}

	if len(m.history) != 3 {
		t.Fatalf("history length = %d, want 3", len(m.history))
	}
	if m.history[0] != "b" || m.history[1] != "c" || m.history[2] != "d" {
		t.Errorf("history should keep newest entries, got %v", m.history)
	}
}

func TestHistoryDedupConsecutive(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	km := DefaultKeyMap()
	m := New(st, km, 100)
	m.SetWidth(80)

	for _, content := range []string{"hello", "hello", "world", "world", "world"} {
		m.SetValue(content)
		updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = updated.(*Model)
	}

	if len(m.history) != 2 {
		t.Fatalf("history length = %d, want 2", len(m.history))
	}
	if m.history[0] != "hello" || m.history[1] != "world" {
		t.Errorf("history should de-duplicate consecutive sends, got %v", m.history)
	}
}

func TestHistoryNoDedupNonConsecutive(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	km := DefaultKeyMap()
	m := New(st, km, 100)
	m.SetWidth(80)

	for _, content := range []string{"hello", "world", "hello"} {
		m.SetValue(content)
		updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = updated.(*Model)
	}

	if len(m.history) != 3 {
		t.Fatalf("history length = %d, want 3", len(m.history))
	}
}

func TestHistoryCapZeroUnbounded(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	km := DefaultKeyMap()
	m := New(st, km, 0)
	m.SetWidth(80)

	for i := 0; i < 5; i++ {
		m.SetValue("msg")
		updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = updated.(*Model)
	}

	if len(m.history) != 1 {
		t.Fatalf("history length = %d, want 1 (consecutive dedup)", len(m.history))
	}

	for i := 0; i < 5; i++ {
		m.SetValue(string(rune('a' + i)))
		updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		m = updated.(*Model)
	}

	if len(m.history) != 6 {
		t.Errorf("history length with cap=0 = %d, want 6", len(m.history))
	}
}

func TestFocusBlur(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	km := DefaultKeyMap()
	m := New(st, km, 100)

	cmd := m.Focus()
	if cmd == nil {
		t.Error("Focus() should return a command")
	}

	m.Blur()
	// Blur doesn't panic - that's the main test
}

func TestReset(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	km := DefaultKeyMap()
	m := New(st, km, 100)
	m.SetWidth(80)

	m.SetValue("something")
	m.Reset()
	if m.Value() != "" {
		t.Errorf("after Reset(): value = %q, want empty", m.Value())
	}
}

func TestConfigurableKeyMap(t *testing.T) {
	t.Parallel()

	cfg := api.KeybindingConfig{
		Send:    "ctrl+s",
		Newline: "ctrl+j",
	}
	km := ConfigurableKeyMap(cfg)

	if len(km.Send.Keys()) != 1 || km.Send.Keys()[0] != "ctrl+s" {
		t.Errorf("Send keys = %v, want [ctrl+s]", km.Send.Keys())
	}
	if len(km.Newline.Keys()) != 1 || km.Newline.Keys()[0] != "ctrl+j" {
		t.Errorf("Newline keys = %v, want [ctrl+j]", km.Newline.Keys())
	}
}

func TestConfigurableKeyMapHelp(t *testing.T) {
	t.Parallel()

	cfg := api.KeybindingConfig{
		Send:    "ctrl+s",
		Newline: "ctrl+j",
	}
	km := ConfigurableKeyMap(cfg)

	if km.Send.Help().Desc == "" {
		t.Error("Send binding should have non-empty help description")
	}
	if km.Newline.Help().Desc == "" {
		t.Error("Newline binding should have non-empty help description")
	}
}

func TestSetWidth(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	km := DefaultKeyMap()
	m := New(st, km, 100)
	m.SetWidth(100)
	view := m.View()
	if view.Content == "" {
		t.Error("View() should not be empty after setting width")
	}
}

func TestMentionDetection(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	km := DefaultKeyMap()
	m := New(st, km, 100)
	m.SetWidth(80)
	m.SetFileCandidates([]string{
		"cmd/kimi-lite/main.go",
		"cmd/kimi-lite/root.go",
		"internal/app/app.go",
	})

	m.SetValue("@cmd")
	m.detectMention()

	if !m.Completing() {
		t.Fatal("expected completion to be active for @cmd")
	}
	// Only the two paths prefixed with "cmd/" match; "internal/app/app.go"
	// matches by basename only when the query targets "app".
	if len(m.mention.candidates) != 2 {
		t.Errorf("candidates = %d, want 2", len(m.mention.candidates))
	}
}

func TestMentionNoAtSymbol(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	km := DefaultKeyMap()
	m := New(st, km, 100)
	m.SetWidth(80)
	m.SetFileCandidates([]string{"cmd/kimi-lite/main.go"})

	m.SetValue("cmd")
	m.detectMention()

	if m.Completing() {
		t.Error("expected no completion without @ prefix")
	}
}

func TestMentionPrefixMatching(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	km := DefaultKeyMap()
	m := New(st, km, 100)
	m.SetWidth(80)
	m.SetFileCandidates([]string{
		"cmd/kimi-lite/main.go",
		"cmd/kimi-lite/root.go",
		"internal/app/app.go",
	})

	m.SetValue("@main")
	m.detectMention()

	if !m.Completing() {
		t.Fatal("expected completion for basename prefix @main")
	}
	if len(m.mention.candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(m.mention.candidates))
	}
	if m.mention.candidates[0] != "cmd/kimi-lite/main.go" {
		t.Errorf("candidate = %q, want cmd/kimi-lite/main.go", m.mention.candidates[0])
	}
}

func TestMentionCaseInsensitive(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	km := DefaultKeyMap()
	m := New(st, km, 100)
	m.SetWidth(80)
	m.SetFileCandidates([]string{"CMD/Kimi-Lite/Main.go"})

	m.SetValue("@cmd")
	m.detectMention()

	if !m.Completing() {
		t.Fatal("expected case-insensitive completion")
	}
}

func TestMentionInsertCandidate(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	km := DefaultKeyMap()
	m := New(st, km, 100)
	m.SetWidth(80)
	m.SetFileCandidates([]string{
		"cmd/kimi-lite/main.go",
		"cmd/kimi-lite/root.go",
	})

	m.SetValue("@cmd")
	m.detectMention()
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	inp := updated.(*Model)

	want := "@cmd/kimi-lite/main.go "
	if inp.Value() != want {
		t.Errorf("value = %q, want %q", inp.Value(), want)
	}
	if inp.Completing() {
		t.Error("completion should close after insertion")
	}
}

func TestMentionNavigateAndInsert(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	km := DefaultKeyMap()
	m := New(st, km, 100)
	m.SetWidth(80)
	m.SetFileCandidates([]string{
		"cmd/kimi-lite/main.go",
		"cmd/kimi-lite/root.go",
	})

	m.SetValue("@cmd")
	m.detectMention()

	m.UpdateMsg(tea.KeyPressMsg{Code: tea.KeyTab})
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	inp := updated.(*Model)

	want := "@cmd/kimi-lite/root.go "
	if inp.Value() != want {
		t.Errorf("value = %q, want %q", inp.Value(), want)
	}
}

func TestMentionCloseWithEsc(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	km := DefaultKeyMap()
	m := New(st, km, 100)
	m.SetWidth(80)
	m.SetFileCandidates([]string{"cmd/kimi-lite/main.go"})

	m.SetValue("@cmd")
	m.detectMention()
	if !m.Completing() {
		t.Fatal("expected completion active")
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	inp := updated.(*Model)

	if inp.Completing() {
		t.Error("expected completion to close on Esc")
	}
}

func TestMentionSetFileCandidatesRefresh(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	km := DefaultKeyMap()
	m := New(st, km, 100)
	m.SetWidth(80)
	m.SetFileCandidates([]string{"cmd/kimi-lite/main.go"})

	m.SetValue("@cmd")
	m.detectMention()
	if !m.Completing() {
		t.Fatal("expected completion active")
	}

	m.SetFileCandidates([]string{})
	if m.Completing() {
		t.Error("expected completion to close when candidates are cleared")
	}
}

func TestMentionViewContainsPopup(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	km := DefaultKeyMap()
	m := New(st, km, 100)
	m.SetWidth(100)
	m.SetFileCandidates([]string{"cmd/kimi-lite/main.go"})

	m.SetValue("@cmd")
	m.detectMention()

	view := m.View()
	if !strings.Contains(view.Content, "cmd/kimi-lite/main.go") {
		t.Error("expected View() to contain the completion candidate")
	}
}

func TestMentionMultiByte(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	km := DefaultKeyMap()
	m := New(st, km, 100)
	m.SetWidth(80)
	m.SetFileCandidates([]string{"docs/日本語.go"})

	m.SetValue("prefix @日本語")
	m.detectMention()

	if !m.Completing() {
		t.Fatal("expected completion for multi-byte @ mention")
	}
	if len(m.mention.candidates) != 1 || m.mention.candidates[0] != "docs/日本語.go" {
		t.Errorf("candidates = %v, want [docs/日本語.go]", m.mention.candidates)
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	inp := updated.(*Model)
	want := "prefix @docs/日本語.go "
	if inp.Value() != want {
		t.Errorf("value = %q, want %q", inp.Value(), want)
	}
}

func TestSlashCompletion(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st, DefaultKeyMap(), 10)
	m.SetWidth(80)
	m.SetSlashCommands(DefaultSlashCommands)
	m.SetValue("/com")
	m.detectSlash()

	if m.slash == nil {
		t.Fatal("expected slash completion state")
	}
	if !strings.Contains(m.slashCompletionView(), "/compact") {
		t.Errorf("missing /compact in popup: %q", m.slashCompletionView())
	}
}

func TestSlashCompletionNoSlash(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st, DefaultKeyMap(), 10)
	m.SetWidth(80)
	m.SetSlashCommands(DefaultSlashCommands)
	m.SetValue("com")
	m.detectSlash()

	if m.Completing() {
		t.Error("expected no slash completion without / prefix")
	}
}

func TestSlashInsertCandidate(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st, DefaultKeyMap(), 10)
	m.SetWidth(80)
	m.SetSlashCommands(DefaultSlashCommands)
	m.SetValue("/com")
	m.detectSlash()

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	inp := updated.(*Model)

	if cmd == nil {
		t.Fatal("expected a send command after slash selection")
	}
	msg, ok := cmd().(SendMsg)
	if !ok {
		t.Fatalf("expected SendMsg, got %T", cmd())
	}
	if msg.Content != "/compact" {
		t.Errorf("SendMsg.Content = %q, want %q", msg.Content, "/compact")
	}
	if inp.Value() != "" {
		t.Errorf("value = %q, want empty after auto-submit", inp.Value())
	}
	if inp.Completing() {
		t.Error("completion should close after insertion")
	}
}

func TestSlashNavigateAndInsert(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st, DefaultKeyMap(), 10)
	m.SetWidth(80)
	m.SetSlashCommands(DefaultSlashCommands)
	m.SetValue("/c")
	m.detectSlash()

	m.UpdateMsg(tea.KeyPressMsg{Code: tea.KeyTab})
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	inp := updated.(*Model)

	if cmd == nil {
		t.Fatal("expected a send command after slash selection")
	}
	msg, ok := cmd().(SendMsg)
	if !ok {
		t.Fatalf("expected SendMsg, got %T", cmd())
	}
	if msg.Content != "/clear" {
		t.Errorf("SendMsg.Content = %q, want %q", msg.Content, "/clear")
	}
	if inp.Value() != "" {
		t.Errorf("value = %q, want empty after auto-submit", inp.Value())
	}
}

func TestSlashCloseWithEsc(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st, DefaultKeyMap(), 10)
	m.SetWidth(80)
	m.SetSlashCommands(DefaultSlashCommands)
	m.SetValue("/com")
	m.detectSlash()
	if !m.Completing() {
		t.Fatal("expected slash completion active")
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	inp := updated.(*Model)

	if inp.Completing() {
		t.Error("expected slash completion to close on Esc")
	}
}

func TestSlashNoAutoSubmitLeavesInputForArgs(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st, DefaultKeyMap(), 10)
	m.SetWidth(80)
	m.SetSlashCommands(DefaultSlashCommands)
	m.SetValue("/tit")
	m.detectSlash()

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	inp := updated.(*Model)

	if cmd != nil {
		t.Error("selecting a NoAutoSubmit slash command should not send immediately")
	}
	if inp.Value() != "/title " {
		t.Errorf("value = %q, want %q", inp.Value(), "/title ")
	}
	if inp.Completing() {
		t.Error("completion should close after insertion")
	}
}

func TestSlashViewContainsPopup(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st, DefaultKeyMap(), 10)
	m.SetWidth(100)
	m.SetSlashCommands(DefaultSlashCommands)
	m.SetValue("/com")
	m.detectSlash()

	view := m.View()
	if !strings.Contains(view.Content, "/compact") {
		t.Error("expected View() to contain the slash command candidate")
	}
}

func TestSlashClearsMention(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st, DefaultKeyMap(), 10)
	m.SetWidth(80)
	m.SetFileCandidates([]string{"cmd/kimi-lite/main.go"})
	m.SetSlashCommands(DefaultSlashCommands)

	m.SetValue("@cmd")
	m.detectMention()
	if !m.mentionActive() {
		t.Fatal("expected mention completion active")
	}

	m.SetValue("/com")
	m.detectMention()
	m.detectSlash()
	if m.mentionActive() {
		t.Error("expected mention completion to clear when switching to slash")
	}
	if m.slash == nil {
		t.Fatal("expected slash completion active after switching from mention")
	}
}

func (m *Model) mentionActive() bool {
	return m.mention != nil
}

func TestPlanModeToggle(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st, DefaultKeyMap(), 10)
	if m.PlanMode() {
		t.Error("plan mode should start disabled")
	}

	m.TogglePlanMode()
	if !m.PlanMode() {
		t.Error("plan mode should be enabled after toggle")
	}

	view := m.View()
	if !strings.Contains(view.Content, "[PLAN]") {
		t.Errorf("missing plan indicator: %q", view.Content)
	}

	m.SetPlanMode(false)
	if m.PlanMode() {
		t.Error("SetPlanMode(false) should disable plan mode")
	}
}

func TestPlanModeShiftTab(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st, DefaultKeyMap(), 10)
	m.SetWidth(80)

	// The input component no longer owns Shift+Tab plan-mode toggling; that is
	// handled by the root model. The input should ignore Shift+Tab so it is not
	// double-toggled.
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if cmd != nil {
		t.Error("shift+tab should not produce a command")
	}

	inp := updated.(*Model)
	if inp.PlanMode() {
		t.Error("input should not toggle plan mode on shift+tab")
	}
}

func TestPlanModeHeightAccountsForIndicator(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st, DefaultKeyMap(), 10)
	m.SetWidth(80)

	heightWithout := m.Height()
	m.TogglePlanMode()
	heightWith := m.Height()

	if heightWith <= heightWithout {
		t.Errorf("plan mode height = %d should be greater than non-plan height = %d", heightWith, heightWithout)
	}
}

func TestRefreshCandidatesCmd_PopulatesCandidates(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st, DefaultKeyMap(), 10)

	called := false
	m.SetCandidateFunc(func() []string {
		called = true
		return []string{"cmd/main.go", "internal/app.go"}
	})

	cmd := m.RefreshCandidatesCmd()
	if cmd == nil {
		t.Fatal("expected RefreshCandidatesCmd to return a command")
	}

	msg := cmd()
	refreshed, ok := msg.(CandidatesRefreshedMsg)
	if !ok {
		t.Fatalf("expected CandidatesRefreshedMsg, got %T", msg)
	}
	if !called {
		t.Error("candidate function was not called")
	}
	if len(refreshed.Candidates) != 2 {
		t.Errorf("candidates = %d, want 2", len(refreshed.Candidates))
	}

	updated, _ := m.Update(refreshed)
	inp := updated.(*Model)
	if len(inp.fileCandidates) != 2 {
		t.Errorf("fileCandidates = %d, want 2", len(inp.fileCandidates))
	}
}

func TestDetectMention_DoesNotCallCandidateFnSynchronously(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st, DefaultKeyMap(), 10)
	m.SetWidth(80)

	called := false
	m.SetCandidateFunc(func() []string {
		called = true
		return nil
	})

	m.SetValue("@cmd")
	m.detectMention()

	if called {
		t.Error("detectMention should not call candidateFn synchronously")
	}
	if m.Completing() {
		t.Error("expected no completion when candidates are empty")
	}
}
