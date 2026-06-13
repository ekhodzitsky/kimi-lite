package messages

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"

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

func TestRenderedContent_SkipsGlamourWhileStreaming(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := NewAssistantMessage("# Heading\n\n**bold** text", st)
	m.SetWidth(80)
	m.SetStreaming(true)

	// While streaming, should return raw content
	raw := m.renderedContent()
	if raw != m.Content {
		t.Errorf("streaming renderedContent = %q, want raw %q", raw, m.Content)
	}
	if strings.Contains(raw, "\x1b[") {
		t.Error("streaming content should not contain ANSI escape codes")
	}

	// After streaming ends, should render with glamour
	m.SetStreaming(false)
	rendered := m.renderedContent()
	if rendered == m.Content {
		t.Error("post-stream renderedContent should not equal raw content")
	}
}

func TestCachedRendererOutput(t *testing.T) {
	t.Parallel()

	content := "# Heading\n\n**bold** text"
	for _, theme := range []string{"dark", "light"} {
		want, err := glamour.Render(content, theme)
		if err != nil {
			t.Fatalf("glamour.Render error: %v", err)
		}
		got := safeGlamourRender(content, theme)
		if got != want {
			t.Errorf("theme=%s: cached output differs from glamour.Render", theme)
		}
	}
}

func BenchmarkRenderMarkdown(b *testing.B) {
	content := "# Heading\n\n**bold** text and some `code`\n\n- item 1\n- item 2\n"
	b.Run("uncached", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = glamour.Render(content, "dark")
		}
	})
	b.Run("cached", func(b *testing.B) {
		// Prime cache
		_ = safeGlamourRender(content, "dark")
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = safeGlamourRender(content, "dark")
		}
	})
}

func TestUserMessageCache(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := NewUserMessage("hello world this is a long message", st)
	m.SetWidth(40)
	v1 := m.View()
	v2 := m.View()
	if v1 != v2 {
		t.Error("second View() at same width should return cached output")
	}
	m.SetWidth(30)
	v3 := m.View()
	if v3 == v1 {
		t.Error("View() after SetWidth should reflect new wrap")
	}
}

func TestToolCallCacheInvalidation(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	call := api.ToolCall{ID: "1", Name: "read_file", Arguments: `{"path": "/tmp/test"}`}
	m := NewToolCallMessage(call, st)
	m.SetWidth(80)
	v1 := m.View()

	// Toggle expand invalidates cache
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg := updated.(*Message)
	v2 := msg.View()
	if v2 == v1 {
		t.Error("View() after expand toggle should return different output")
	}
}
