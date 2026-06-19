package messages

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"github.com/charmbracelet/x/ansi"

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

func TestNewErrorMessage_Nil(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := NewErrorMessage(nil, st)

	if m.Type != TypeError {
		t.Errorf("Type = %d, want %d", m.Type, TypeError)
	}
	if m.Err != nil {
		t.Error("Err should be nil")
	}
	if m.Content != "" {
		t.Errorf("Content = %q, want empty", m.Content)
	}

	// Rendering should not panic.
	m.SetWidth(80)
	_ = m.View()
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

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	msg := updated.(*Message)
	if !msg.Expanded {
		t.Error("Expanded should be true after Enter key")
	}

	updated2, _ := msg.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
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

	updated, _ := m.Update(tea.MouseReleaseMsg{Button: tea.MouseLeft})
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
	if !strings.Contains(view.Content, "hello") {
		t.Error("User message view should contain content")
	}
}

func TestMessageViewAssistant(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := NewAssistantMessage("**bold**", st)
	m.SetWidth(80)
	view := m.View()
	if view.Content == "" {
		t.Error("Assistant message view should not be empty")
	}
}

func TestSetRawMode_InvalidatesCache(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := NewAssistantMessage("**bold**", st)
	m.SetWidth(80)

	// Prime the glamour cache.
	_ = m.View()
	if m.Rendered == "" {
		t.Fatal("Rendered cache should be populated after View")
	}
	if m.needsRender {
		t.Fatal("needsRender should be false after render")
	}

	m.SetRawMode(true)

	if !m.RawMode {
		t.Error("RawMode should be true")
	}
	if m.Rendered != "" {
		t.Errorf("Rendered cache should be cleared, got %q", m.Rendered)
	}
	if m.renderCache != "" {
		t.Errorf("renderCache should be cleared, got %q", m.renderCache)
	}
	if !m.needsRender {
		t.Error("needsRender should be true after SetRawMode")
	}
}

func TestSetRawMode_BypassesGlamour(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	content := "**bold** and `code`"
	m := NewAssistantMessage(content, st)
	m.SetWidth(80)

	rendered := m.renderedContent()
	if rendered == content {
		t.Fatal("rendered content should differ from raw content")
	}

	m.SetRawMode(true)
	raw := m.renderedContent()
	if raw != content {
		t.Errorf("raw renderedContent = %q, want %q", raw, content)
	}
	if strings.Contains(raw, "\x1b[") {
		t.Error("raw renderedContent should not contain ANSI escape codes")
	}

	m.SetRawMode(false)
	renderedAgain := m.renderedContent()
	if renderedAgain == content {
		t.Error("rendered content should differ from raw content after disabling raw mode")
	}
}

func TestToggleRawModeKeyBinding(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := NewAssistantMessage("**bold**", st)

	cmd := m.UpdateMsg(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if cmd == nil {
		t.Fatal("UpdateMsg should return a command for raw-mode toggle")
	}
	if !m.RawMode {
		t.Error("RawMode should be true after toggling")
	}

	msg := cmd()
	if _, ok := msg.(RenderInvalidateMsg); !ok {
		t.Errorf("cmd() returned %T, want RenderInvalidateMsg", msg)
	}
}

func TestToggleRawModeKeyBinding_NonAssistantIgnored(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := NewUserMessage("**bold**", st)

	cmd := m.UpdateMsg(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if cmd != nil {
		t.Error("UpdateMsg should not return a command for non-assistant messages")
	}
	if m.RawMode {
		t.Error("RawMode should remain false for non-assistant messages")
	}
}

func TestMessageViewToolCall(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	call := api.ToolCall{ID: "1", Name: "read_file", Arguments: `{"path": "/tmp/test"}`}
	m := NewToolCallMessage(call, st)
	m.SetWidth(80)
	view := m.View()
	if !strings.Contains(view.Content, "read_file") {
		t.Error("Tool call view should contain tool name")
	}
	if !strings.Contains(view.Content, "Using") {
		t.Error("Tool call view should contain Using verb")
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
	if !strings.Contains(view.Content, "Arguments:") {
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
	if !strings.Contains(view.Content, "Used") {
		t.Error("Tool call with result should show Used verb")
	}
	if !strings.Contains(view.Content, "file contents") {
		t.Error("Tool call with result should show output")
	}
}

func TestToolCallPending(t *testing.T) {
	st := styles.New("dark")
	m := NewToolCallMessage(api.ToolCall{Name: "read_file"}, st)
	m.SetWidth(80)
	view := m.View().Content
	if !strings.Contains(view, "Using read_file") {
		t.Errorf("expected Using header, got %q", view)
	}
}

func TestToolCallDone(t *testing.T) {
	st := styles.New("dark")
	m := NewToolCallMessage(api.ToolCall{Name: "read_file"}, st)
	m.SetWidth(80)
	m.SetToolResult(api.ToolResult{CallID: "1", Name: "read_file", Output: "42 lines"})
	view := m.View().Content
	if !strings.Contains(view, "Used read_file") {
		t.Errorf("expected Used header, got %q", view)
	}
}

func TestToolCallWriteFile_LineCountChip(t *testing.T) {
	st := styles.New("dark")
	m := NewToolCallMessage(api.ToolCall{Name: "write_file"}, st)
	m.Expanded = true
	m.SetWidth(80)
	m.SetToolResult(api.ToolResult{CallID: "1", Name: "write_file", Output: "line1\nline2\nline3"})
	view := m.View().Content
	if !strings.Contains(view, "3 lines") {
		t.Errorf("expected '3 lines' chip for write_file, got %q", view)
	}
	if strings.Contains(view, "Output:") {
		t.Errorf("write_file result should not show generic Output label, got %q", view)
	}
}

func TestToolCallStrReplaceFile_LineCountChip(t *testing.T) {
	st := styles.New("dark")
	m := NewToolCallMessage(api.ToolCall{Name: "str_replace_file"}, st)
	m.Expanded = true
	m.SetWidth(80)
	m.SetToolResult(api.ToolResult{CallID: "1", Name: "str_replace_file", Output: "alpha\nbeta"})
	view := m.View().Content
	if !strings.Contains(view, "2 lines") {
		t.Errorf("expected '2 lines' chip for str_replace_file, got %q", view)
	}
	if strings.Contains(view, "Output:") {
		t.Errorf("str_replace_file result should not show generic Output label, got %q", view)
	}
}

func TestToolCallReadFile_KeepsGenericOutput(t *testing.T) {
	st := styles.New("dark")
	m := NewToolCallMessage(api.ToolCall{Name: "read_file"}, st)
	m.Expanded = true
	m.SetWidth(80)
	m.SetToolResult(api.ToolResult{CallID: "1", Name: "read_file", Output: "file contents"})
	view := m.View().Content
	if !strings.Contains(view, "Output: file contents") {
		t.Errorf("expected generic Output label for read_file, got %q", view)
	}
}

func TestMessageViewError(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := NewErrorMessage(errors.New("failure"), st)
	m.SetWidth(80)
	view := m.View()
	if !strings.Contains(view.Content, "failure") {
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
		{"hello world", 5, "hello\nworld"},
		{"", 10, ""},
		{"hello\nworld", 10, "hello\nworld"},
		{"helloworld", 4, "hell\nowor\nld"},
	}

	for _, tt := range tests {
		got := wordWrap(tt.input, tt.width)
		if got != tt.want {
			t.Errorf("wordWrap(%q, %d) = %q, want %q", tt.input, tt.width, got, tt.want)
		}
	}
}

func TestWordWrap_WideRunes(t *testing.T) {
	t.Parallel()

	// "日本語" has a display width of 6 (2 per CJK rune).
	input := "日本語"
	got := wordWrap(input, 4)
	want := "日本\n語"
	if got != want {
		t.Errorf("wordWrap(%q, 4) = %q, want %q", input, got, want)
	}
}

func TestWordWrap_ANSISequences(t *testing.T) {
	t.Parallel()

	// ANSI escape sequences contribute zero width and must not be split.
	input := "\x1b[31mhello world\x1b[0m"
	got := wordWrap(input, 5)
	want := "\x1b[31mhello\nworld\x1b[0m"
	if got != want {
		t.Errorf("wordWrap(%q, 5) = %q, want %q", input, got, want)
	}
}

func TestRenderedContent_SkipsGlamourWhileStreaming(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := NewAssistantMessage("# Heading\n\n**bold** text", st)
	m.SetWidth(80)
	m.SetStreaming(true)

	// While streaming, should return raw content with a cursor indicator.
	raw := m.renderedContent()
	if !strings.HasSuffix(raw, streamingCursor) {
		t.Errorf("streaming renderedContent = %q, want cursor suffix %q", raw, streamingCursor)
	}
	if !strings.Contains(raw, m.Content) {
		t.Errorf("streaming renderedContent = %q, should contain raw %q", raw, m.Content)
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
		got := safeGlamourRender(content, theme, 80)
		if ansi.Strip(got) != ansi.Strip(want) {
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
		_ = safeGlamourRender(content, "dark", 80)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = safeGlamourRender(content, "dark", 80)
		}
	})
}

func TestToggleRawMode(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := NewAssistantMessage("**bold**", st)

	m.ToggleRawMode()
	if !m.RawMode {
		t.Error("RawMode should be true after ToggleRawMode")
	}

	m.ToggleRawMode()
	if m.RawMode {
		t.Error("RawMode should be false after second ToggleRawMode")
	}
}

func TestMax(t *testing.T) {
	t.Parallel()

	if got := max(5, 3); got != 5 {
		t.Errorf("max(5, 3) = %d, want 5", got)
	}
	if got := max(2, 7); got != 7 {
		t.Errorf("max(2, 7) = %d, want 7", got)
	}
	if got := max(4, 4); got != 4 {
		t.Errorf("max(4, 4) = %d, want 4", got)
	}
}

func TestWordWrap_ZeroAndNegativeWidth(t *testing.T) {
	t.Parallel()

	input := "hello world"
	if got := wordWrap(input, 0); got != input {
		t.Errorf("wordWrap(%q, 0) = %q, want %q", input, got, input)
	}
	if got := wordWrap(input, -5); got != input {
		t.Errorf("wordWrap(%q, -5) = %q, want %q", input, got, input)
	}
}

func TestSetRawModeLocked_NoOp(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := NewAssistantMessage("**bold**", st)
	m.SetRawMode(true)
	m.SetRawMode(true) // should be a no-op

	if !m.RawMode {
		t.Error("RawMode should remain true")
	}
	if m.Rendered != "" || m.renderCache != "" {
		t.Error("cache should stay cleared after repeated SetRawMode(true)")
	}
}

func TestSafeGlamourRender_BadCacheEntry(t *testing.T) {
	// Not parallel because it mutates the package-level rendererCache.
	theme := "bad-cache-entry-theme-unique"
	width := 80
	key := fmt.Sprintf("%s:%d", theme, width)
	rendererCache.Store(key, "not-a-cached-renderer")
	defer rendererCache.Delete(key)

	content := "# hello\n"
	got := safeGlamourRender(content, theme, width)
	if got != content {
		t.Errorf("safeGlamourRender with bad cache entry = %q, want raw content %q", got, content)
	}
}

func TestView_DefaultCase(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := NewUserMessage("hello", st)
	m.Type = Type(99)

	if got := m.View().Content; got != "" {
		t.Errorf("View() for unknown message type = %q, want empty", got)
	}
}

func TestViewUser_CacheHit(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := NewUserMessage("hello world this is a long message", st)
	m.SetWidth(40)
	v1 := m.View().Content
	v2 := m.View().Content
	if v1 != v2 {
		t.Error("second View() at same width should return cached output")
	}
}

func TestViewError_CacheHit(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := NewErrorMessage(errors.New("failure"), st)
	m.SetWidth(80)
	v1 := m.View().Content
	v2 := m.View().Content
	if v1 != v2 {
		t.Error("second View() should return cached output")
	}
}

func TestViewToolCall_CacheHit(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	call := api.ToolCall{ID: "1", Name: "read_file", Arguments: `{}`}
	m := NewToolCallMessage(call, st)
	m.SetWidth(80)
	v1 := m.View().Content
	v2 := m.View().Content
	if v1 != v2 {
		t.Error("second View() should return cached output")
	}
}

func TestViewToolCall_ErrorResult(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	call := api.ToolCall{ID: "1", Name: "read_file", Arguments: `{}`}
	m := NewToolCallMessage(call, st)
	m.Expanded = true
	m.SetToolResult(api.ToolResult{CallID: "1", Name: "read_file", Error: "something broke"})
	m.SetWidth(80)

	view := m.View().Content
	if !strings.Contains(view, "Error") {
		t.Error("Tool call with error result should show Error verb")
	}
	if !strings.Contains(view, "something broke") {
		t.Error("Tool call with error result should show error text")
	}
}

func TestUserMessageCache(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := NewUserMessage("hello world this is a long message", st)
	m.SetWidth(40)
	v1 := m.View().Content
	v2 := m.View().Content
	if v1 != v2 {
		t.Error("second View() at same width should return cached output")
	}
	m.SetWidth(30)
	v3 := m.View().Content
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
	v1 := m.View().Content

	// Toggle expand invalidates cache
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	msg := updated.(*Message)
	v2 := msg.View().Content
	if v2 == v1 {
		t.Error("View() after expand toggle should return different output")
	}
}

func TestSafeGlamourRender_CustomThemeDefaultsToDark(t *testing.T) {
	t.Parallel()

	content := "# hello\n"
	// Custom theme names should map to the dark glamour style so markdown
	// rendering doesn't silently fall back to raw text.
	got := safeGlamourRender(content, "my-custom-theme", 80)
	if got == content {
		t.Error("custom theme should render with glamour, not fall back to raw content")
	}
	if !strings.Contains(got, "hello") {
		t.Errorf("rendered output should contain content, got %q", got)
	}
}

func TestPrettyJSONArgs(t *testing.T) {
	t.Parallel()

	raw := `{"path":"/tmp/test","count":1}`
	got := prettyJSONArgs(raw)
	if !strings.Contains(got, "\n") {
		t.Errorf("prettyJSONArgs should indent JSON, got %q", got)
	}
	if !strings.Contains(got, `"path"`) {
		t.Errorf("prettyJSONArgs should preserve keys, got %q", got)
	}

	invalid := "not json"
	if prettyJSONArgs(invalid) != invalid {
		t.Errorf("prettyJSONArgs should return raw input for invalid JSON, got %q", prettyJSONArgs(invalid))
	}
}

func TestRenderedContent_AssistantWidthInvalidation(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	content := "This is a fairly long assistant message that should wrap differently at different widths."
	m := NewAssistantMessage(content, st)
	m.SetWidth(120)
	wide := m.renderedContent()
	m.SetWidth(40)
	narrow := m.renderedContent()
	if wide == narrow {
		t.Error("renderedContent should be recomputed when width changes")
	}
}

func TestRenderedContent_RawModeWraps(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	content := strings.Repeat("word ", 30)
	m := NewAssistantMessage(content, st)
	m.SetWidth(40)
	m.SetRawMode(true)
	got := m.renderedContent()
	if !strings.Contains(got, "\n") {
		t.Errorf("raw mode should wrap long lines, got %q", got)
	}
}

func TestRenderedContent_StreamingCursor(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := NewAssistantMessage("typing", st)
	m.SetWidth(80)
	m.SetStreaming(true)

	got := m.renderedContent()
	if !strings.HasSuffix(got, streamingCursor) {
		t.Errorf("streaming renderedContent = %q, want cursor suffix %q", got, streamingCursor)
	}
}

func TestContentWithParts(t *testing.T) {
	t.Parallel()

	parts := []api.ContentPart{
		{Type: api.ContentPartText, Text: "extra text"},
		{Type: api.ContentPartImageURL, ImageURL: &api.ImageURL{URL: "http://example.com/img.png"}},
		{Type: api.ContentPartImageData, ImageData: &api.ImageData{MIMEType: "image/png", Data: "abc"}},
	}
	got := contentWithParts("base", parts)
	if !strings.Contains(got, "base") {
		t.Error("contentWithParts should preserve base content")
	}
	if !strings.Contains(got, "extra text") {
		t.Error("contentWithParts should include text parts")
	}
	if !strings.Contains(got, "🖼️ image") {
		t.Errorf("contentWithParts should render image placeholder, got %q", got)
	}
	if !strings.Contains(got, "http://example.com/img.png") {
		t.Error("contentWithParts should include image URL")
	}

	if got := contentWithParts("", parts); strings.HasPrefix(got, "\n") {
		t.Errorf("contentWithParts with empty base should not lead with blank lines, got %q", got)
	}
}

func TestUserMessageView_ContentParts(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := NewUserMessage("Look at this", st)
	m.ContentParts = []api.ContentPart{
		{Type: api.ContentPartImageURL, ImageURL: &api.ImageURL{URL: "http://example.com/img.png"}},
	}
	m.SetWidth(80)
	view := m.View().Content
	if !strings.Contains(view, "🖼️ image") {
		t.Errorf("user message view should render image part, got %q", view)
	}
}

func TestAssistantMessageView_ContentParts(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := NewAssistantMessage("Here is an image", st)
	m.ContentParts = []api.ContentPart{
		{Type: api.ContentPartImageURL, ImageURL: &api.ImageURL{URL: "http://example.com/img.png"}},
	}
	m.SetWidth(80)
	m.SetStreaming(false)
	view := m.View().Content
	if !strings.Contains(view, "🖼️ image") {
		t.Errorf("assistant message view should render image part, got %q", view)
	}
}

func TestToolCallView_ContentParts(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	call := api.ToolCall{ID: "1", Name: "read_file", Arguments: `{}`}
	m := NewToolCallMessage(call, st)
	m.Expanded = true
	m.SetToolResult(api.ToolResult{
		CallID: "1",
		Name:   "read_file",
		Output: "done",
		ContentParts: []api.ContentPart{
			{Type: api.ContentPartImageData, ImageData: &api.ImageData{MIMEType: "image/png", Data: "abc"}},
		},
	})
	m.SetWidth(80)
	view := m.View().Content
	if !strings.Contains(view, "🖼️ image") {
		t.Errorf("tool result view should render image content parts, got %q", view)
	}
}

func TestGoldenMessageViewUser(t *testing.T) {
	st := styles.New("dark")
	m := NewUserMessage("Hello, assistant!", st)
	m.SetWidth(60)
	compareGolden(t, "message_user", m.View().Content)
}

func TestGoldenMessageViewAssistant(t *testing.T) {
	st := styles.New("dark")
	m := NewAssistantMessage("## Summary\n\nThis is **bold** and `code`.", st)
	m.SetWidth(60)
	compareGolden(t, "message_assistant", m.View().Content)
}

func TestGoldenMessageViewToolCall(t *testing.T) {
	st := styles.New("dark")
	call := api.ToolCall{ID: "call_1", Name: "read_file", Arguments: `{"path": "/tmp/test"}`}
	m := NewToolCallMessage(call, st)
	m.Expanded = true
	m.SetWidth(60)
	compareGolden(t, "message_toolcall", m.View().Content)
}

func TestGoldenMessageViewError(t *testing.T) {
	st := styles.New("dark")
	m := NewErrorMessage(errors.New("something went wrong"), st)
	m.SetWidth(60)
	compareGolden(t, "message_error", m.View().Content)
}
