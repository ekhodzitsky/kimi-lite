package images

import (
	"testing"
)

func TestDetectCapability(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  map[string]string
		want Capability
	}{
		{
			name: "none by default",
			env:  map[string]string{},
			want: None,
		},
		{
			name: "kitty via KITTY_WINDOW_ID",
			env:  map[string]string{"KITTY_WINDOW_ID": "1"},
			want: Kitty,
		},
		{
			name: "kitty via TERM",
			env:  map[string]string{"TERM": "xterm-kitty"},
			want: Kitty,
		},
		{
			name: "iterm2 via TERM_PROGRAM",
			env:  map[string]string{"TERM_PROGRAM": "iTerm.app"},
			want: Iterm2,
		},
		{
			name: "iterm2 for WezTerm",
			env:  map[string]string{"TERM_PROGRAM": "WezTerm"},
			want: Iterm2,
		},
		{
			name: "sixel via TERM",
			env:  map[string]string{"TERM": "xterm-sixel"},
			want: Sixel,
		},
		{
			name: "sixel via MLTERM",
			env:  map[string]string{"MLTERM": "1"},
			want: Sixel,
		},
		{
			name: "sixel via YAFT",
			env:  map[string]string{"YAFT": "1"},
			want: Sixel,
		},
		{
			name: "sixel via WEZTERM_EXECUTABLE",
			env:  map[string]string{"WEZTERM_EXECUTABLE": "/Applications/WezTerm.app"},
			want: Sixel,
		},
		{
			name: "kitty wins over sixel",
			env: map[string]string{
				"KITTY_WINDOW_ID": "1",
				"TERM":            "xterm-sixel",
			},
			want: Kitty,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := detectCapability(func(k string) string { return tt.env[k] })
			if got != tt.want {
				t.Errorf("detectCapability() = %v, want %v", got, tt.want)
			}
		})
	}
}
