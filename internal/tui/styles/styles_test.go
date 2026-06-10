package styles

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
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
	if _, ok := s.StatusBar.GetBackground().(lipgloss.NoColor); ok {
		t.Error("StatusBar background not set")
	}
	if _, ok := s.Sidebar.GetBackground().(lipgloss.NoColor); ok {
		t.Error("Sidebar background not set")
	}
	if _, ok := s.InputBox.GetBackground().(lipgloss.NoColor); ok {
		t.Error("InputBox background not set")
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
