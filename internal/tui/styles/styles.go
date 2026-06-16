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
	InputBg         color.Color
	Highlight       color.Color
}

// darkTheme is the default dark theme.
var darkTheme = Theme{
	Name:            "dark",
	Background:      lipgloss.Color("#1a1a1a"),
	Foreground:      lipgloss.Color("#E0E0E0"),
	Primary:         lipgloss.Color("#4FA8FF"),
	Secondary:       lipgloss.Color("#5BC0BE"),
	Success:         lipgloss.Color("#4EC87E"),
	Warning:         lipgloss.Color("#E8A838"),
	Error:           lipgloss.Color("#E85454"),
	Muted:           lipgloss.Color("#6B6B6B"),
	Border:          lipgloss.Color("#5A5A5A"),
	UserBubble:      lipgloss.Color("#2a2a2a"),
	AssistantBubble: lipgloss.Color("#1a1a1a"),
	ToolBubble:      lipgloss.Color("#333333"),
	StatusBarBg:     lipgloss.Color("#111111"),
	InputBg:         lipgloss.Color("#262626"),
	Highlight:       lipgloss.Color("#4FA8FF"),
}

// lightTheme is the light theme.
var lightTheme = Theme{
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
	InputBox         lipgloss.Style
	MentionPopup     lipgloss.Style
	ScrollIndicator  lipgloss.Style
	ApprovalDialog   lipgloss.Style
	Footer           lipgloss.Style
	FooterModel      lipgloss.Style
	FooterCWD        lipgloss.Style
	FooterGit        lipgloss.Style
	FooterTip        lipgloss.Style
	FooterStatus     lipgloss.Style
	FooterContext    lipgloss.Style
	ModeBadgeYolo    lipgloss.Style
	ModeBadgeAuto    lipgloss.Style
	WelcomeBox       lipgloss.Style
	WelcomeTitle     lipgloss.Style
	WelcomeText      lipgloss.Style
}

// New creates a new Styles instance for the given theme name.
func New(themeName string) *Styles {
	var theme Theme
	switch themeName {
	case "light":
		theme = lightTheme
	default:
		theme = darkTheme
	}

	s := &Styles{Theme: theme}
	s.init()
	return s
}

func (s *Styles) init() {
	t := s.Theme

	s.UserMessage = lipgloss.NewStyle().
		Background(t.UserBubble).
		Foreground(lipgloss.Color("#FFCB6B")).
		Padding(0, 1).
		MarginLeft(1).
		MarginRight(4).
		MarginBottom(1).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#FFCB6B"))

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

	s.Footer = lipgloss.NewStyle().Background(t.StatusBarBg).Foreground(t.Foreground)
	s.FooterModel = lipgloss.NewStyle().Background(t.StatusBarBg).Foreground(t.Primary).Bold(true)
	s.FooterCWD = lipgloss.NewStyle().Background(t.StatusBarBg).Foreground(t.Muted)
	s.FooterGit = lipgloss.NewStyle().Background(t.StatusBarBg).Foreground(t.Success)
	s.FooterTip = lipgloss.NewStyle().Background(t.StatusBarBg).Foreground(t.Muted)
	s.FooterStatus = lipgloss.NewStyle().Background(t.StatusBarBg).Foreground(t.Foreground)
	s.FooterContext = lipgloss.NewStyle().Background(t.StatusBarBg).Foreground(t.Secondary)
	s.ModeBadgeYolo = lipgloss.NewStyle().Background(t.Error).Foreground(t.Foreground).Bold(true)
	s.ModeBadgeAuto = lipgloss.NewStyle().Background(t.Primary).Foreground(t.Background).Bold(true)

	borderColor := t.Border
	if t.Name == "light" {
		borderColor = lipgloss.Color("#bcc0cc")
	}
	s.WelcomeBox = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Background(t.Background).
		Foreground(t.Foreground).
		Padding(1, 2).
		MarginBottom(1)
	s.WelcomeTitle = lipgloss.NewStyle().Bold(true).Foreground(t.Primary).Background(t.Background)
	s.WelcomeText = lipgloss.NewStyle().Foreground(t.Foreground).Background(t.Background)
}
