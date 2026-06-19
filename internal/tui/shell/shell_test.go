package shell

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ekhodzitsky/kimi-lite/internal/tui/styles"
)

func newTestModel() *Model {
	return New(styles.New("dark"))
}

func TestModelOpenClose(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	if m.IsOpen() {
		t.Fatal("new model should be closed")
	}

	m.Open()
	if !m.IsOpen() {
		t.Fatal("Open should open the overlay")
	}

	m.Close()
	if m.IsOpen() {
		t.Fatal("Close should close the overlay")
	}

	if m.Toggle(); !m.IsOpen() {
		t.Fatal("Toggle should open the overlay")
	}
	if m.Toggle(); m.IsOpen() {
		t.Fatal("Toggle should close the overlay")
	}
}

func TestModelInput(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m.Open()
	m.SetSize(80)

	m.UpdateMsg(keyPress("h"))
	m.UpdateMsg(keyPress("i"))
	if got := m.Input(); got != "hi" {
		t.Fatalf("input = %q, want hi", got)
	}

	m.UpdateMsg(keyPress("left"))
	m.UpdateMsg(keyPress("!"))
	if got := m.Input(); got != "h!i" {
		t.Fatalf("input = %q, want h!i", got)
	}

	m.UpdateMsg(keyPress("ctrl+u"))
	if got := m.Input(); got != "" {
		t.Fatalf("input = %q, want empty", got)
	}
}

func TestModelExecute(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m.Open()
	m.SetSize(80)

	for _, r := range "echo hello" {
		m.UpdateMsg(keyPress(string(r)))
	}

	_, cmd := m.Update(keyPress("enter"))
	if cmd == nil {
		t.Fatal("enter should return a command")
	}
	msg := cmd()
	em, ok := msg.(ExecuteMsg)
	if !ok {
		t.Fatalf("expected ExecuteMsg, got %T", msg)
	}
	if em.Command != "echo hello" {
		t.Fatalf("command = %q, want echo hello", em.Command)
	}
}

func TestModelConfirmation(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m.Open()
	m.SetSize(80)

	for _, r := range "rm -rf /" {
		m.UpdateMsg(keyPress(string(r)))
	}

	m.StartConfirmation("rm -rf /")
	if !m.IsConfirming() {
		t.Fatal("StartConfirmation should set confirming state")
	}

	_, cmd := m.Update(keyPress("enter"))
	if cmd == nil {
		t.Fatal("enter while confirming should return a command")
	}
	msg := cmd()
	cem, ok := msg.(ConfirmedExecuteMsg)
	if !ok {
		t.Fatalf("expected ConfirmedExecuteMsg, got %T", msg)
	}
	if cem.Command != "rm -rf /" {
		t.Fatalf("command = %q, want rm -rf /", cem.Command)
	}
	if m.IsConfirming() {
		t.Fatal("confirming should be cleared after execution")
	}
	if !m.IsRunning() {
		t.Fatal("running should be true after confirmed execution")
	}
}

func TestModelCancel(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m.Open()
	m.SetRunning("sleep 10", true)

	_, cmd := m.Update(keyPress("ctrl+c"))
	if cmd == nil {
		t.Fatal("ctrl+c while running should return a command")
	}
	if _, ok := cmd().(CancelMsg); !ok {
		t.Fatalf("expected CancelMsg, got %T", cmd())
	}

	m.Open()
	m.SetRunning("sleep 10", true)
	m.SetConfirming(true)
	m.UpdateMsg(keyPress("esc"))
	if m.IsConfirming() {
		t.Fatal("esc should cancel confirmation")
	}
	if !m.IsOpen() {
		t.Fatal("esc should keep overlay open when cancelling confirmation")
	}

	m.UpdateMsg(keyPress("esc"))
	if m.IsOpen() {
		t.Fatal("second esc should close overlay")
	}
}

func TestModelHistory(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m.Open()
	m.SetSize(80)

	m.AddHistory("echo one")
	m.AddHistory("echo two")
	m.AddHistory("echo two") // duplicate of last should be ignored

	if got := len(m.History()); got != 2 {
		t.Fatalf("history length = %d, want 2", got)
	}

	m.UpdateMsg(keyPress("up"))
	if got := m.Input(); got != "echo two" {
		t.Fatalf("input after first up = %q, want echo two", got)
	}

	m.UpdateMsg(keyPress("up"))
	if got := m.Input(); got != "echo one" {
		t.Fatalf("input after second up = %q, want echo one", got)
	}

	m.UpdateMsg(keyPress("down"))
	if got := m.Input(); got != "echo two" {
		t.Fatalf("input after down = %q, want echo two", got)
	}

	m.UpdateMsg(keyPress("down"))
	if got := m.Input(); got != "" {
		t.Fatalf("input after final down = %q, want empty draft", got)
	}
}

func TestModelHistoryDuplicate(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m.AddHistory("ls")
	m.AddHistory("ls")
	if got := len(m.History()); got != 1 {
		t.Fatalf("history length = %d, want 1", got)
	}
}

func TestModelEmptyExecute(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m.Open()
	m.SetSize(80)

	_, cmd := m.Update(keyPress("enter"))
	if cmd != nil {
		t.Fatal("enter with empty input should not return a command")
	}
}

func keyPress(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "home":
		return tea.KeyPressMsg{Code: tea.KeyHome}
	case "end":
		return tea.KeyPressMsg{Code: tea.KeyEnd}
	case "ctrl+u":
		return tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}
	case "ctrl+w":
		return tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	case "ctrl+h":
		return tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModCtrl}
	default:
		if len(s) == 1 {
			return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
		}
		return tea.KeyPressMsg{}
	}
}

func TestModelInitAndUpdateWrapper(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	if m.Init() != nil {
		t.Error("Init() should return nil")
	}

	m.Open()
	for _, r := range "ls" {
		m.Update(keyPress(string(r)))
	}
	model, cmd := m.Update(keyPress("enter"))
	if model != m {
		t.Error("Update() should return the same model")
	}
	if cmd == nil {
		t.Fatal("Update() should return a command for enter")
	}
	if _, ok := cmd().(ExecuteMsg); !ok {
		t.Errorf("expected ExecuteMsg, got %T", cmd())
	}

	// Non-key messages are ignored.
	_, cmd = m.Update(struct{ kind int }{kind: 42})
	if cmd != nil {
		t.Error("Update() should return nil for unknown messages")
	}

	// Messages are ignored when the overlay is closed.
	m.Close()
	_, cmd = m.Update(keyPress("a"))
	if cmd != nil {
		t.Error("Update() should return nil when closed")
	}
}

func TestModelInsertAndDelete(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		setup    func(*Model)
		want     string
		wantCurs int
	}{
		{
			name: "insert at negative cursor",
			setup: func(m *Model) {
				m.input = "abc"
				m.cursor = -5
				m.insert("XY")
			},
			want:     "XYabc",
			wantCurs: 2,
		},
		{
			name: "insert beyond end",
			setup: func(m *Model) {
				m.input = "abc"
				m.cursor = 100
				m.insert("Z")
			},
			want:     "abcZ",
			wantCurs: 4,
		},
		{
			name: "insert multi-byte",
			setup: func(m *Model) {
				m.input = "a"
				m.cursor = 1
				m.insert("日")
			},
			want:     "a日",
			wantCurs: 2,
		},
		{
			name: "delete rune at start",
			setup: func(m *Model) {
				m.input = "abc"
				m.cursor = 0
				m.deleteRuneBackward()
			},
			want:     "abc",
			wantCurs: 0,
		},
		{
			name: "delete rune beyond end",
			setup: func(m *Model) {
				m.input = "abc"
				m.cursor = 100
				m.deleteRuneBackward()
			},
			want:     "abc",
			wantCurs: 100,
		},
		{
			name: "delete multi-byte rune",
			setup: func(m *Model) {
				m.input = "a日b"
				m.cursor = 2
				m.deleteRuneBackward()
			},
			want:     "ab",
			wantCurs: 1,
		},
		{
			name: "delete word trailing spaces",
			setup: func(m *Model) {
				m.input = "hello   "
				m.cursor = len([]rune(m.input))
				m.deleteWordBackward()
			},
			want:     "",
			wantCurs: 0,
		},
		{
			name: "delete word between words",
			setup: func(m *Model) {
				m.input = "hello world"
				m.cursor = len([]rune(m.input))
				m.deleteWordBackward()
			},
			want:     "hello ",
			wantCurs: 6,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := newTestModel()
			tc.setup(m)
			if m.input != tc.want {
				t.Errorf("input = %q, want %q", m.input, tc.want)
			}
			if m.cursor != tc.wantCurs {
				t.Errorf("cursor = %d, want %d", m.cursor, tc.wantCurs)
			}
		})
	}
}

func TestAppendableKeyTextShell(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		text string
		want string
	}{
		{"empty", "", ""},
		{"regular", "x", "x"},
		{"multi-byte", "日", "日"},
		{"control", "\x01", ""},
		{"newline", "\n", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := appendableKeyText(tea.KeyPressMsg{Text: tc.text})
			if got != tc.want {
				t.Errorf("appendableKeyText() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestModelCursorMovement(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m.Open()
	for _, r := range "abc" {
		m.UpdateMsg(keyPress(string(r)))
	}

	m.UpdateMsg(keyPress("left"))
	if m.cursor != 2 {
		t.Errorf("cursor after left = %d, want 2", m.cursor)
	}
	for i := 0; i < 5; i++ {
		m.UpdateMsg(keyPress("left"))
	}
	if m.cursor != 0 {
		t.Errorf("cursor at left bound = %d, want 0", m.cursor)
	}

	m.UpdateMsg(keyPress("right"))
	if m.cursor != 1 {
		t.Errorf("cursor after right = %d, want 1", m.cursor)
	}
	for i := 0; i < 5; i++ {
		m.UpdateMsg(keyPress("right"))
	}
	if m.cursor != 3 {
		t.Errorf("cursor at right bound = %d, want 3", m.cursor)
	}

	m.UpdateMsg(keyPress("home"))
	if m.cursor != 0 {
		t.Errorf("cursor after home = %d, want 0", m.cursor)
	}

	m.UpdateMsg(keyPress("end"))
	if m.cursor != 3 {
		t.Errorf("cursor after end = %d, want 3", m.cursor)
	}
}

func TestModelRunningState(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m.SetRunning("sleep 1", true)
	if !m.IsRunning() {
		t.Fatal("SetRunning(true) should mark running")
	}
	if m.RunningCommand() != "sleep 1" {
		t.Errorf("RunningCommand = %q, want sleep 1", m.RunningCommand())
	}

	m.SetRunning("", false)
	if m.IsRunning() {
		t.Error("SetRunning(false) should clear running")
	}
	if m.RunningCommand() != "" {
		t.Errorf("RunningCommand = %q, want empty", m.RunningCommand())
	}
}

func TestModelCancelWhileRunning(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m.Open()
	m.SetRunning("sleep 10", true)

	_, cmd := m.Update(keyPress("esc"))
	if cmd == nil {
		t.Fatal("esc while running should return a command")
	}
	if _, ok := cmd().(CancelMsg); !ok {
		t.Errorf("expected CancelMsg, got %T", cmd())
	}
	if m.IsOpen() {
		t.Error("esc while running should close the overlay")
	}

	m.Open()
	m.SetRunning("sleep 10", true)
	_, cmd = m.Update(keyPress("ctrl+c"))
	if cmd == nil {
		t.Fatal("ctrl+c while running should return a command")
	}
	if _, ok := cmd().(CancelMsg); !ok {
		t.Errorf("expected CancelMsg, got %T", cmd())
	}

	m.Open()
	m.SetRunning("sleep 10", true)
	_, cmd = m.Update(keyPress("enter"))
	if cmd != nil {
		t.Error("enter while running should not return a command")
	}
}

func TestModelConfirmationEsc(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m.Open()
	m.StartConfirmation("rm -rf /")
	if !m.IsConfirming() {
		t.Fatal("should be confirming")
	}

	m.UpdateMsg(keyPress("esc"))
	if m.IsConfirming() {
		t.Error("esc should cancel confirmation")
	}
	if m.confirmCommand != "" {
		t.Error("confirmCommand should be cleared")
	}
	if !m.IsOpen() {
		t.Error("overlay should stay open after cancelling confirmation")
	}
}

func TestModelSetConfirming(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m.StartConfirmation("cmd")
	m.SetConfirming(false)
	if m.IsConfirming() {
		t.Error("SetConfirming(false) should clear confirming")
	}
	if m.confirmCommand != "" {
		t.Error("confirmCommand should be cleared")
	}
}

func TestModelExecuteTrimsWhitespace(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m.Open()
	m.SetSize(80)

	for _, r := range "  echo hello  " {
		m.UpdateMsg(keyPress(string(r)))
	}

	_, cmd := m.Update(keyPress("enter"))
	if cmd == nil {
		t.Fatal("enter should return a command")
	}
	em := cmd().(ExecuteMsg)
	if em.Command != "echo hello" {
		t.Errorf("command = %q, want echo hello", em.Command)
	}
}

func TestModelApprovalModes(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m.Open()
	m.SetSize(80)

	// Auto / yolo mode emits ExecuteMsg directly.
	for _, r := range "ls" {
		m.UpdateMsg(keyPress(string(r)))
	}
	_, cmd := m.Update(keyPress("enter"))
	if cmd == nil {
		t.Fatal("expected execute command")
	}
	if _, ok := cmd().(ExecuteMsg); !ok {
		t.Errorf("expected ExecuteMsg, got %T", cmd())
	}

	// Manual approval mode emits ConfirmedExecuteMsg.
	m.Open()
	m.StartConfirmation("rm -rf /")
	_, cmd = m.Update(keyPress("enter"))
	if cmd == nil {
		t.Fatal("expected confirmed execute command")
	}
	if _, ok := cmd().(ConfirmedExecuteMsg); !ok {
		t.Errorf("expected ConfirmedExecuteMsg, got %T", cmd())
	}
	if !m.IsRunning() {
		t.Error("model should be running after confirmed execution")
	}
}

func TestModelHistoryEdges(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m.Open()

	// Empty history: up/down should not panic.
	m.UpdateMsg(keyPress("up"))
	m.UpdateMsg(keyPress("down"))
	if m.Input() != "" {
		t.Errorf("input = %q, want empty", m.Input())
	}

	m.AddHistory("first")
	m.AddHistory("second")
	m.AddHistory("first") // non-consecutive duplicate is allowed.
	if got := len(m.History()); got != 3 {
		t.Errorf("history length = %d, want 3", got)
	}

	// History returns a copy.
	h := m.History()
	h[0] = "mutated"
	if m.History()[0] != "first" {
		t.Error("History() should return a copy")
	}
}

func TestModelHistoryMaxSize(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	for i := 0; i < maxHistory+5; i++ {
		m.AddHistory(fmt.Sprintf("cmd%d", i))
	}
	if got := len(m.History()); got != maxHistory {
		t.Errorf("history length = %d, want %d", got, maxHistory)
	}
	wantFirst := "cmd5"
	if m.History()[0] != wantFirst {
		t.Errorf("oldest retained command = %q, want %q", m.History()[0], wantFirst)
	}
}

func TestModelAddHistoryIgnoresEmpty(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m.AddHistory("")
	m.AddHistory("   ")
	m.AddHistory("ls")
	m.AddHistory("ls") // consecutive duplicate ignored.
	if got := len(m.History()); got != 1 {
		t.Errorf("history length = %d, want 1", got)
	}
}

func TestModelView(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	if m.Height() != 0 {
		t.Errorf("closed Height = %d, want 0", m.Height())
	}
	if m.View().Content != "" {
		t.Error("closed View should be empty")
	}

	m.Open()
	m.SetSize(80)
	for _, r := range "echo hi" {
		m.UpdateMsg(keyPress(string(r)))
	}
	view := m.View().Content
	if view == "" {
		t.Error("open View should not be empty")
	}
	if m.Height() < 1 {
		t.Errorf("open Height = %d, want >= 1", m.Height())
	}

	// Truncation on narrow width should not panic.
	m.SetSize(2)
	m.UpdateMsg(keyPress("!"))
	_ = m.View()
}

func TestModelViewConfirmation(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m.Open()
	m.SetSize(80)
	m.StartConfirmation("rm -rf /")
	view := m.View().Content
	if !strings.Contains(view, "rm -rf /") {
		t.Errorf("confirmation View should show command, got %q", view)
	}

	// shellQuote should quote a command containing special characters.
	m.StartConfirmation("echo 'hello'")
	view = m.View().Content
	if !strings.Contains(view, "'") {
		t.Errorf("confirmation View should quote special command, got %q", view)
	}
}

func TestShellQuote(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want string
	}{
		{"ls", "ls"},
		{"echo hello", "'echo hello'"},
		{"echo'quote", "'echo'\"'\"'quote'"},
		{"a&b", "'a&b'"},
	}

	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			if got := shellQuote(tc.in); got != tc.want {
				t.Errorf("shellQuote(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
