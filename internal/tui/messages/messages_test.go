package messages

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ekhodzitsky/kimi-lite/internal/tui/styles"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func TestNewUserMessage(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := NewUserMessage("hello", st)

	if m.Type != TypeUser {
		t.Errorf("Type = %d, want %d", m.Type, TypeUser)
	}
	if m.Content != "hello" {
		t.Errorf("Content = %q, want %q", m.Content, "hello")
	}
	if m.Role != api.RoleUser {
		t.Errorf("Role = %q, want %q", m.Role, api.RoleUser)
	}
}

func TestNewAssistantMessage(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := NewAssistantMessage("markdown content", st)

	if m.Type != TypeAssistant {
		t.Errorf("Type = %d, want %d", m.Type, TypeAssistant)
	}
	if m.Content != "markdown content" {
		t.Errorf("Content = %q, want %q", m.Content, "markdown content")
	}
	if m.Role != api.RoleAssistant {
		t.Errorf("Role = %q, want %q", m.Role, api.RoleAssistant)
	}
}

func TestNewToolCallMessage(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	call := api.ToolCall{ID: "1", Name: "read_file", Arguments: `{"path": "/tmp/test"}`}
	m := NewToolCallMessage(call, st)

	if m.Type != TypeToolCall {
		t.Errorf("Type = %d, want %d", m.Type, TypeToolCall)
	}
	if m.ToolCall.ID != "1" {
		t.Errorf("ToolCall.ID = %q, want %q", m.ToolCall.ID, "1")
	}
}

func TestNewErrorMessage(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	err := errors.New("something went wrong")
	m := NewErrorMessage(err, st)

	if m.Type != TypeError {
		t.Errorf("Type = %d, want %d", m.Type, TypeError)
	}
	if m.Err != err {
		t.Error("Err mismatch")
	}
	if m.Content != err.Error() {
		t.Errorf("Content = %q, want %q", m.Content, err.Error())
	}
}

func TestMessageInit(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := NewUserMessage("test", st)
	cmd := m.Init()
	if cmd != nil {
		t.Error("Init() should return nil")
	}
}

func TestMessageUpdateToggleExpand(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	call := api.ToolCall{ID: "1", Name: "test_tool", Arguments: `{}`}
	m := NewToolCallMessage(call, st)

	if m.Expanded {
		t.Error("initial Expanded should be false")
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg := updated.(*Message)
	if !msg.Expanded {
		t.Error("Expanded should be true after Enter key")
	}

	updated2, _ := msg.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg2 := updated2.(*Message)
	if msg2.Expanded {
		t.Error("Expanded should be false after second Enter key")
	}
}

func TestMessageUpdateMouseToggle(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	call := api.ToolCall{ID: "1", Name: "test_tool", Arguments: `{}`}
	m := NewToolCallMessage(call, st)

	updated, _ := m.Update(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	msg := updated.(*Message)
	if !msg.Expanded {
		t.Error("Expanded should be true after mouse click")
	}
}

func TestAppendContent(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := NewAssistantMessage("hello", st)
	m.AppendContent(" world")

	if m.Content != "hello world" {
		t.Errorf("Content = %q, want %q", m.Content, "hello world")
	}
	if m.Rendered != "" {
		t.Error("Rendered cache should be invalidated")
	}

	// Appending to non-assistant should not change content
	user := NewUserMessage("user", st)
	user.AppendContent(" extra")
	if user.Content != "user" {
		t.Error("User message content should not change")
	}
}

func TestSetToolResult(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	call := api.ToolCall{ID: "1", Name: "test_tool", Arguments: `{}`}
	m := NewToolCallMessage(call, st)

	result := api.ToolResult{CallID: "1", Name: "test_tool", Output: "done"}
	m.SetToolResult(result)

	if m.ToolResult == nil {
		t.Fatal("ToolResult should not be nil")
	}
	if m.ToolResult.Output != "done" {
		t.Errorf("ToolResult.Output = %q, want %q", m.ToolResult.Output, "done")
	}
}

func TestMessageViewUser(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := NewUserMessage("hello", st)
	m.SetWidth(80)
	view := m.View()
	if !strings.Contains(view, "hello") {
		t.Error("User message view should contain content")
	}
}

func TestMessageViewAssistant(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := NewAssistantMessage("**bold**", st)
	m.SetWidth(80)
	view := m.View()
	if view == "" {
		t.Error("Assistant message view should not be empty")
	}
}

func TestMessageViewAssistantRawMode(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := NewAssistantMessage("**bold**", st)
	m.RawMode = true
	m.SetWidth(80)
	view := m.View()
	if !strings.Contains(view, "**bold**") {
		t.Error("Raw mode should contain raw markdown")
	}
}

func TestMessageViewToolCall(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	call := api.ToolCall{ID: "1", Name: "read_file", Arguments: `{"path": "/tmp/test"}`}
	m := NewToolCallMessage(call, st)
	m.SetWidth(80)
	view := m.View()
	if !strings.Contains(view, "read_file") {
		t.Error("Tool call view should contain tool name")
	}
	if !strings.Contains(view, "pending") {
		t.Error("Tool call view should contain pending status")
	}
}

func TestMessageViewToolCallExpanded(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	call := api.ToolCall{ID: "1", Name: "read_file", Arguments: `{"path": "/tmp/test"}`}
	m := NewToolCallMessage(call, st)
	m.Expanded = true
	m.SetWidth(80)
	view := m.View()
	if !strings.Contains(view, "Arguments:") {
		t.Error("Expanded tool call should show arguments")
	}
}

func TestMessageViewToolCallWithResult(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	call := api.ToolCall{ID: "1", Name: "read_file", Arguments: `{}`}
	m := NewToolCallMessage(call, st)
	m.Expanded = true
	m.SetToolResult(api.ToolResult{CallID: "1", Name: "read_file", Output: "file contents"})
	m.SetWidth(80)
	view := m.View()
	if !strings.Contains(view, "done") {
		t.Error("Tool call with result should show done status")
	}
	if !strings.Contains(view, "file contents") {
		t.Error("Tool call with result should show output")
	}
}

func TestMessageViewError(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := NewErrorMessage(errors.New("failure"), st)
	m.SetWidth(80)
	view := m.View()
	if !strings.Contains(view, "failure") {
		t.Error("Error message view should contain error text")
	}
}

func TestWordWrap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		width int
		want  string
	}{
		{"hello world", 20, "hello world"},
		{"hello world", 5, "hello\n worl\nd"},
		{"", 10, ""},
		{"hello\nworld", 10, "hello\nworld"},
	}

	for _, tt := range tests {
		got := wordWrap(tt.input, tt.width)
		if got != tt.want {
			t.Errorf("wordWrap(%q, %d) = %q, want %q", tt.input, tt.width, got, tt.want)
		}
	}
}
