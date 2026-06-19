package styles

import (
	"testing"

	"charm.land/lipgloss/v2"
)

func TestNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		themeName string
		wantName  string
	}{
		{"dark theme", "dark", "dark"},
		{"light theme", "light", "light"},
		{"unknown defaults to dark", "unknown", "dark"},
		{"empty defaults to dark", "", "dark"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(tt.themeName)
			if s.Theme.Name != tt.wantName {
				t.Errorf("New(%q).Theme.Name = %q, want %q", tt.themeName, s.Theme.Name, tt.wantName)
			}
		})
	}
}

func TestStylesInit(t *testing.T) {
	t.Parallel()

	s := New("dark")

	if _, ok := s.UserMessage.GetBackground().(lipgloss.NoColor); ok {
		t.Error("UserMessage background not set")
	}
	if _, ok := s.AssistantMessage.GetBackground().(lipgloss.NoColor); ok {
		t.Error("AssistantMessage background not set")
	}
	if _, ok := s.ToolCall.GetBackground().(lipgloss.NoColor); ok {
		t.Error("ToolCall background not set")
	}
	if _, ok := s.ErrorMessage.GetForeground().(lipgloss.NoColor); ok {
		t.Error("ErrorMessage foreground not set")
	}
	if _, ok := s.InputBox.GetBackground().(lipgloss.NoColor); ok {
		t.Error("InputBox background not set")
	}
	if _, ok := s.Attachment.GetForeground().(lipgloss.NoColor); ok {
		t.Error("Attachment foreground not set")
	}
	if _, ok := s.CompletionSelected.GetBackground().(lipgloss.NoColor); ok {
		t.Error("CompletionSelected background not set")
	}
	if _, ok := s.InputBoxFocused.GetBackground().(lipgloss.NoColor); ok {
		t.Error("InputBoxFocused background not set")
	}
}

func TestThemeColors(t *testing.T) {
	t.Parallel()

	dark := New("dark")
	light := New("light")

	if dark.Theme.Background == light.Theme.Background {
		t.Error("dark and light themes should have different background colors")
	}
	if dark.Theme.Foreground == light.Theme.Foreground {
		t.Error("dark and light themes should have different foreground colors")
	}
}

func TestUserMessageUsesThemeTokens(t *testing.T) {
	t.Parallel()

	dark := New("dark")
	if dark.UserMessage.GetForeground() != Color(dark.Theme.UserMessageFg) {
		t.Error("UserMessage foreground should use theme UserMessageFg")
	}

	light := New("light")
	if light.UserMessage.GetForeground() != Color(light.Theme.UserMessageFg) {
		t.Error("UserMessage foreground should use theme UserMessageFg")
	}
	if light.Theme.UserMessageFg == dark.Theme.UserMessageFg {
		t.Error("light and dark UserMessageFg should differ")
	}
}

func TestNewFromThemeDefaultsUserMessageColors(t *testing.T) {
	t.Parallel()

	theme := Theme{
		Name:            "minimal",
		Background:      "#111111",
		Foreground:      "#eeeeee",
		Primary:         "#ff0000",
		Secondary:       "#00ff00",
		Success:         "#00ff00",
		Warning:         "#ffff00",
		Error:           "#ff0000",
		Muted:           "#888888",
		Border:          "#333333",
		UserBubble:      "#222222",
		AssistantBubble: "#111111",
		ToolBubble:      "#333333",
		StatusBarBg:     "#000000",
		InputBg:         "#222222",
		Highlight:       "#ff0000",
	}
	s := NewFromTheme(&theme)
	if s.UserMessage.GetForeground() != theme.Primary {
		t.Errorf("expected default UserMessageFg to fall back to primary, got %v", s.UserMessage.GetForeground())
	}
	if s.UserMessage.GetBorderLeftForeground() != theme.Border {
		t.Errorf("expected default UserMessageBorder to fall back to border, got %v", s.UserMessage.GetBorderLeftForeground())
	}
}
