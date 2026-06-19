// Package images detects and renders terminal image protocols.
package images

import (
	"os"
	"strings"
)

// Capability represents a terminal image rendering protocol.
type Capability int

const (
	// None means the terminal does not support inline images.
	None Capability = iota
	// Sixel means the terminal supports Sixel graphics.
	Sixel
	// Kitty means the terminal supports the Kitty graphics protocol.
	Kitty
	// Iterm2 means the terminal supports the iTerm2 inline-image protocol.
	Iterm2
)

// String returns the human-readable capability name.
func (c Capability) String() string {
	switch c {
	case Sixel:
		return "sixel"
	case Kitty:
		return "kitty"
	case Iterm2:
		return "iterm2"
	default:
		return "none"
	}
}

// DetectCapability inspects the process environment and returns the best
// supported terminal image protocol for the current terminal.
func DetectCapability() Capability {
	return detectCapability(os.Getenv)
}

func detectCapability(getenv func(string) string) Capability {
	term := getenv("TERM")
	termProgram := getenv("TERM_PROGRAM")

	// Kitty graphics protocol.
	if getenv("KITTY_WINDOW_ID") != "" || term == "xterm-kitty" {
		return Kitty
	}

	// iTerm2 inline images (WezTerm also supports this protocol).
	if termProgram == "iTerm.app" || termProgram == "WezTerm" {
		return Iterm2
	}

	// Sixel support.
	if strings.Contains(term, "sixel") ||
		strings.EqualFold(term, "mlterm") ||
		getenv("MLTERM") != "" ||
		getenv("YAFT") != "" ||
		getenv("WEZTERM_EXECUTABLE") != "" {
		return Sixel
	}

	return None
}
