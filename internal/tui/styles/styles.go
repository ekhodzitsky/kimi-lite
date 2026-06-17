// Package styles provides Lipgloss styles for the kimi-lite TUI.
package styles

import (
	"encoding/json"
	"fmt"

	"charm.land/lipgloss/v2"
)

// Color is a lipgloss-compatible color represented as a string (hex or ANSI).
// It implements the image/color.Color interface by delegating to lipgloss.Color.
type Color string

// RGBA satisfies the image/color.Color interface.
func (c Color) RGBA() (r, g, b, a uint32) {
	return lipgloss.Color(string(c)).RGBA()
}

// MarshalJSON encodes the color as its string representation.
func (c Color) MarshalJSON() ([]byte, error) {
	b, err := json.Marshal(string(c))
	if err != nil {
		return nil, fmt.Errorf("marshal color: %w", err)
	}
	return b, nil
}

// UnmarshalJSON decodes the color from a JSON string.
func (c *Color) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("unmarshal color: %w", err)
	}
	*c = Color(s)
	return nil
}

// Theme represents a UI theme with color definitions.
type Theme struct {
	Name            string `json:"name"`
	Background      Color  `json:"background"`
	Foreground      Color  `json:"foreground"`
	Primary         Color  `json:"primary"`
	Secondary       Color  `json:"secondary"`
	Success         Color  `json:"success"`
	Warning         Color  `json:"warning"`
	Error           Color  `json:"error"`
	Muted           Color  `json:"muted"`
	Border          Color  `json:"border"`
	UserBubble      Color  `json:"user_bubble"`
	AssistantBubble Color  `json:"assistant_bubble"`
	ToolBubble      Color  `json:"tool_bubble"`
	StatusBarBg     Color  `json:"status_bar_bg"`
	InputBg         Color  `json:"input_bg"`
	Highlight       Color  `json:"highlight"`
}

// darkTheme is the default dark theme.
var darkTheme = Theme{
	Name:            "dark",
	Background:      Color("#1a1a1a"),
	Foreground:      Color("#E0E0E0"),
	Primary:         Color("#4FA8FF"),
	Secondary:       Color("#5BC0BE"),
	Success:         Color("#4EC87E"),
	Warning:         Color("#E8A838"),
	Error:           Color("#E85454"),
	Muted:           Color("#6B6B6B"),
	Border:          Color("#5A5A5A"),
	UserBubble:      Color("#2a2a2a"),
	AssistantBubble: Color("#1a1a1a"),
	ToolBubble:      Color("#333333"),
	StatusBarBg:     Color("#111111"),
	InputBg:         Color("#262626"),
	Highlight:       Color("#4FA8FF"),
}

// lightTheme is the light theme.
var lightTheme = Theme{
	Name:            "light",
	Background:      Color("#eff1f5"),
	Foreground:      Color("#4c4f69"),
	Primary:         Color("#1e66f5"),
	Secondary:       Color("#8839ef"),
	Success:         Color("#40a02b"),
	Warning:         Color("#df8e1d"),
	Error:           Color("#d20f39"),
	Muted:           Color("#8c8fa1"),
	Border:          Color("#bcc0cc"),
	UserBubble:      Color("#ccd0da"),
	AssistantBubble: Color("#eff1f5"),
	ToolBubble:      Color("#bcc0cc"),
	StatusBarBg:     Color("#e6e9ef"),
	InputBg:         Color("#ccd0da"),
	Highlight:       Color("#1e66f5"),
}

// Styles holds all Lipgloss styles for the application.
type Styles struct {
	Theme Theme

	UserMessage         lipgloss.Style
	AssistantMessage    lipgloss.Style
	ToolCall            lipgloss.Style
	ToolCallExpanded    lipgloss.Style
	ToolCallPendingIcon lipgloss.Style
	ToolCallDoneIcon    lipgloss.Style
	ToolCallErrorIcon   lipgloss.Style
	ErrorMessage        lipgloss.Style
	InputBox            lipgloss.Style
	MentionPopup        lipgloss.Style
	SlashPopup          lipgloss.Style
	SlashCommandName    lipgloss.Style
	SlashCommandDesc    lipgloss.Style
	ScrollIndicator     lipgloss.Style
	HelpOverlay         lipgloss.Style
	HelpTitle           lipgloss.Style
	HelpKey             lipgloss.Style
	HelpCommand         lipgloss.Style
	HelpDesc            lipgloss.Style
	ApprovalDialog      lipgloss.Style
	Footer              lipgloss.Style
	FooterModel         lipgloss.Style
	FooterCWD           lipgloss.Style
	FooterGit           lipgloss.Style
	FooterTip           lipgloss.Style
	FooterStatus        lipgloss.Style
	FooterContext       lipgloss.Style
	ModeBadgeYolo       lipgloss.Style
	ModeBadgeAuto       lipgloss.Style
	WelcomeBox          lipgloss.Style
	WelcomeTitle        lipgloss.Style
	WelcomeText         lipgloss.Style
	Activity            lipgloss.Style
	ActivityTool        lipgloss.Style
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

	return NewFromTheme(&theme)
}

// NewFromTheme creates a Styles instance from an arbitrary Theme.
func NewFromTheme(t *Theme) *Styles {
	s := &Styles{Theme: *t}
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

	s.ToolCallPendingIcon = lipgloss.NewStyle().Foreground(t.Primary)
	s.ToolCallDoneIcon = lipgloss.NewStyle().Foreground(t.Success)
	s.ToolCallErrorIcon = lipgloss.NewStyle().Foreground(t.Error)

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

	s.SlashPopup = lipgloss.NewStyle().
		Background(t.Background).
		Foreground(t.Foreground).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Primary).
		Padding(0, 1)
	s.SlashCommandName = lipgloss.NewStyle().Foreground(t.Primary).Bold(true)
	s.SlashCommandDesc = lipgloss.NewStyle().Foreground(t.Muted)

	s.HelpOverlay = lipgloss.NewStyle().
		Background(t.Background).
		Foreground(t.Foreground).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Primary).
		Padding(1, 2)
	s.HelpTitle = lipgloss.NewStyle().Bold(true).Foreground(t.Primary).Background(t.Background)
	s.HelpKey = lipgloss.NewStyle().Foreground(t.Secondary).Background(t.Background)
	s.HelpCommand = lipgloss.NewStyle().Foreground(t.Primary).Background(t.Background).Bold(true)
	s.HelpDesc = lipgloss.NewStyle().Foreground(t.Foreground).Background(t.Background)

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
		borderColor = Color("#bcc0cc")
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

	s.Activity = lipgloss.NewStyle().
		Background(t.Background).
		Foreground(t.Primary).
		Padding(0, 1).
		MarginLeft(1).
		MarginBottom(1)
	s.ActivityTool = lipgloss.NewStyle().
		Background(t.Background).
		Foreground(t.Muted).
		MarginLeft(2)
}
