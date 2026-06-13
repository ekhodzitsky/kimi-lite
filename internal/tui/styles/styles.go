// Package styles provides Lipgloss styles for the kimi-lite TUI.
package styles

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Theme represents a UI theme with color definitions.
type Theme struct {
	Name            string
	Background      color.Color
	Foreground      color.Color
	Primary         color.Color
	Secondary       color.Color
	Success         color.Color
	Warning         color.Color
	Error           color.Color
	Muted           color.Color
	Border          color.Color
	UserBubble      color.Color
	AssistantBubble color.Color
	ToolBubble      color.Color
	StatusBarBg     color.Color
	SidebarBg       color.Color
	InputBg         color.Color
	Highlight       color.Color
}

// DarkTheme is the default dark theme.
var DarkTheme = Theme{
	Name:            "dark",
	Background:      lipgloss.Color("#1e1e2e"),
	Foreground:      lipgloss.Color("#cdd6f4"),
	Primary:         lipgloss.Color("#89b4fa"),
	Secondary:       lipgloss.Color("#cba6f7"),
	Success:         lipgloss.Color("#a6e3a1"),
	Warning:         lipgloss.Color("#f9e2af"),
	Error:           lipgloss.Color("#f38ba8"),
	Muted:           lipgloss.Color("#6c7086"),
	Border:          lipgloss.Color("#45475a"),
	UserBubble:      lipgloss.Color("#313244"),
	AssistantBubble: lipgloss.Color("#1e1e2e"),
	ToolBubble:      lipgloss.Color("#45475a"),
	StatusBarBg:     lipgloss.Color("#181825"),
	SidebarBg:       lipgloss.Color("#181825"),
	InputBg:         lipgloss.Color("#313244"),
	Highlight:       lipgloss.Color("#89b4fa"),
}

// LightTheme is the light theme.
var LightTheme = Theme{
	Name:            "light",
	Background:      lipgloss.Color("#eff1f5"),
	Foreground:      lipgloss.Color("#4c4f69"),
	Primary:         lipgloss.Color("#1e66f5"),
	Secondary:       lipgloss.Color("#8839ef"),
	Success:         lipgloss.Color("#40a02b"),
	Warning:         lipgloss.Color("#df8e1d"),
	Error:           lipgloss.Color("#d20f39"),
	Muted:           lipgloss.Color("#8c8fa1"),
	Border:          lipgloss.Color("#bcc0cc"),
	UserBubble:      lipgloss.Color("#ccd0da"),
	AssistantBubble: lipgloss.Color("#eff1f5"),
	ToolBubble:      lipgloss.Color("#bcc0cc"),
	StatusBarBg:     lipgloss.Color("#e6e9ef"),
	SidebarBg:       lipgloss.Color("#e6e9ef"),
	InputBg:         lipgloss.Color("#ccd0da"),
	Highlight:       lipgloss.Color("#1e66f5"),
}

// Styles holds all Lipgloss styles for the application.
type Styles struct {
	Theme Theme

	UserMessage      lipgloss.Style
	AssistantMessage lipgloss.Style
	ToolCall         lipgloss.Style
	ToolCallExpanded lipgloss.Style
	ErrorMessage     lipgloss.Style
	StatusBar        lipgloss.Style
	Sidebar          lipgloss.Style
	SidebarTitle     lipgloss.Style
	SidebarItem      lipgloss.Style
	SidebarSelected  lipgloss.Style
	InputBox         lipgloss.Style
	MentionPopup     lipgloss.Style
	ScrollIndicator  lipgloss.Style
	ApprovalDialog   lipgloss.Style
}

// New creates a new Styles instance for the given theme name.
func New(themeName string) *Styles {
	var theme Theme
	switch themeName {
	case "light":
		theme = LightTheme
	default:
		theme = DarkTheme
	}

	s := &Styles{Theme: theme}
	s.init()
	return s
}

func (s *Styles) init() {
	t := s.Theme

	s.UserMessage = lipgloss.NewStyle().
		Background(t.UserBubble).
		Foreground(t.Foreground).
		Padding(0, 1).
		MarginLeft(4).
		MarginRight(1).
		MarginBottom(1).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(t.Primary)

	s.AssistantMessage = lipgloss.NewStyle().
		Background(t.AssistantBubble).
		Foreground(t.Foreground).
		Padding(0, 1).
		MarginLeft(1).
		MarginRight(4).
		MarginBottom(1).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(t.Secondary)

	s.ToolCall = lipgloss.NewStyle().
		Background(t.ToolBubble).
		Foreground(t.Foreground).
		Padding(0, 1).
		MarginLeft(2).
		MarginRight(4).
		MarginBottom(1).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(t.Warning)

	s.ToolCallExpanded = lipgloss.NewStyle().
		Background(t.ToolBubble).
		Foreground(t.Foreground).
		Padding(1).
		MarginLeft(2).
		MarginRight(4).
		MarginBottom(1).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(t.Warning)

	s.ErrorMessage = lipgloss.NewStyle().
		Background(t.Background).
		Foreground(t.Error).
		Padding(0, 1).
		MarginLeft(1).
		MarginRight(4).
		MarginBottom(1).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(t.Error)

	s.StatusBar = lipgloss.NewStyle().
		Background(t.StatusBarBg).
		Foreground(t.Foreground).
		Padding(0, 1).
		Height(1)

	s.Sidebar = lipgloss.NewStyle().
		Background(t.SidebarBg).
		Foreground(t.Foreground).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(t.Border).
		BorderRight(true).
		Padding(0, 1)

	s.SidebarTitle = lipgloss.NewStyle().
		Background(t.SidebarBg).
		Foreground(t.Primary).
		Bold(true).
		Padding(0, 1)

	s.SidebarItem = lipgloss.NewStyle().
		Background(t.SidebarBg).
		Foreground(t.Muted).
		Padding(0, 1)

	s.SidebarSelected = lipgloss.NewStyle().
		Background(t.SidebarBg).
		Foreground(t.Highlight).
		Bold(true).
		Padding(0, 1)

	s.InputBox = lipgloss.NewStyle().
		Background(t.InputBg).
		Foreground(t.Foreground).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(t.Border).
		Padding(0, 1)

	s.MentionPopup = lipgloss.NewStyle().
		Background(t.Background).
		Foreground(t.Foreground).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(t.Primary).
		Padding(0, 1)

	s.ScrollIndicator = lipgloss.NewStyle().
		Foreground(t.Muted).
		Background(t.Background)

	s.ApprovalDialog = lipgloss.NewStyle().
		Background(t.Background).
		Foreground(t.Foreground).
		BorderStyle(lipgloss.DoubleBorder()).
		BorderForeground(t.Warning).
		Padding(1, 2)
}
