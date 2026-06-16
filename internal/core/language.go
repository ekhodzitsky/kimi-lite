package core

import (
	"fmt"
	"unicode"
)

// DetectLanguage returns a simple language code for the given text.
// It uses script-based heuristics because we do not want to add external
// NLP dependencies for a short status sentence.
func DetectLanguage(text string) string {
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Cyrillic, r):
			return "ru"
		case unicode.Is(unicode.Han, r):
			return "zh"
		}
	}
	return "en"
}

// statusWorthyTools lists tools for which the TUI should show a transient
// status sentence before execution. Read-only, near-instant tools are omitted.
var statusWorthyTools = map[string]bool{
	"write_file":        true,
	"str_replace_file":  true,
	"edit":              true,
	"shell":             true,
	"fetch_url":         true,
	"web_search":        true,
	"read_video":        true,
	"dispatch_subagent": true,
}

// IsStatusWorthyTool reports whether a tool should trigger a status message.
func IsStatusWorthyTool(name string) bool {
	return statusWorthyTools[name]
}

// StatusMessage returns a concise status sentence for the given tool in the
// requested language. Unknown languages fall back to English.
func StatusMessage(toolName, lang string) string {
	templates := map[string]string{
		"en": "Running %s…",
		"ru": "Запускаю %s…",
		"zh": "正在运行 %s…",
	}
	tmpl, ok := templates[lang]
	if !ok {
		tmpl = templates["en"]
	}
	return fmt.Sprintf(tmpl, toolName)
}
