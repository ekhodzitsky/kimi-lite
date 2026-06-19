package shell

import (
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
