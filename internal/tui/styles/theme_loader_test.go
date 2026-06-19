package styles

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// validCustomTheme is a complete custom theme used by tests.
const validCustomTheme = `{
	"name": "custom",
	"background": "#111111",
	"foreground": "#eeeeee",
	"primary": "#ff0000",
	"secondary": "#00ff00",
	"success": "#00ff00",
	"warning": "#ffff00",
	"error": "#ff0000",
	"muted": "#888888",
	"border": "#333333",
	"user_bubble": "#222222",
	"assistant_bubble": "#111111",
	"tool_bubble": "#333333",
	"status_bar_bg": "#000000",
	"input_bg": "#222222",
	"highlight": "#ff0000"
}`

func TestLoadThemeBuiltIn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		wantName string
	}{
		{"dark", "dark", "dark"},
		{"empty defaults to dark", "", "dark"},
		{"light", "light", "light"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			theme, err := LoadTheme(tt.input, "")
			if err != nil {
				t.Fatal(err)
			}
			if theme.Name != tt.wantName {
				t.Errorf("expected %q, got %q", tt.wantName, theme.Name)
			}
		})
	}
}

func TestLoadThemeCustom(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "themes", "custom.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(validCustomTheme), 0o644); err != nil {
		t.Fatal(err)
	}

	theme, err := LoadTheme("custom", dir)
	if err != nil {
		t.Fatal(err)
	}
	if theme.Name != "custom" {
		t.Errorf("expected name %q, got %q", "custom", theme.Name)
	}
	if theme.Background != Color("#111111") {
		t.Errorf("expected background %q, got %q", "#111111", theme.Background)
	}
	if theme.Foreground != Color("#eeeeee") {
		t.Errorf("expected foreground %q, got %q", "#eeeeee", theme.Foreground)
	}
	if theme.Primary != Color("#ff0000") {
		t.Errorf("expected primary %q, got %q", "#ff0000", theme.Primary)
	}
	if theme.UserMessageFg != theme.Primary {
		t.Errorf("expected default UserMessageFg to fall back to primary, got %q", theme.UserMessageFg)
	}
	if theme.UserMessageBorder != theme.Border {
		t.Errorf("expected default UserMessageBorder to fall back to border, got %q", theme.UserMessageBorder)
	}
}

func TestLoadThemeCustomAbsolutePath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	themesDir := filepath.Join(dir, "themes")
	path := filepath.Join(themesDir, "my-theme.json")
	data := []byte(`{
		"name": "absolute",
		"background": "#222222",
		"foreground": "#eeeeee",
		"primary": "#ff0000",
		"secondary": "#00ff00",
		"success": "#00ff00",
		"warning": "#ffff00",
		"error": "#ff0000",
		"muted": "#888888",
		"border": "#333333",
		"user_bubble": "#222222",
		"assistant_bubble": "#111111",
		"tool_bubble": "#333333",
		"status_bar_bg": "#000000",
		"input_bg": "#222222",
		"highlight": "#ff0000"
	}`)
	if err := os.MkdirAll(themesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	theme, err := LoadTheme(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	if theme.Name != "absolute" {
		t.Errorf("expected name %q, got %q", "absolute", theme.Name)
	}
}

func TestLoadThemeAbsolutePathOutsideThemesDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "escape-theme.json")
	if err := os.WriteFile(path, []byte(validCustomTheme), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadTheme(path, dir)
	if err == nil {
		t.Fatal("expected error for absolute theme path outside themes directory")
	}
	if !strings.Contains(err.Error(), "escapes themes directory") {
		t.Errorf("expected directory-traversal error, got %v", err)
	}
}

func TestLoadThemeDirectoryTraversal(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Create a file outside the themes directory that a ".." traversal would hit.
	outside := filepath.Join(dir, "secret.json")
	if err := os.WriteFile(outside, []byte(validCustomTheme), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadTheme("../secret", dir)
	if err == nil {
		t.Fatal("expected error for directory traversal")
	}
	if !strings.Contains(err.Error(), "escapes themes directory") {
		t.Errorf("expected directory-traversal error, got %v", err)
	}
}

func TestLoadThemeMissing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	_, err := LoadTheme("nonexistent", dir)
	if err == nil {
		t.Fatal("expected error for missing theme")
	}
}

func TestLoadThemeInvalidJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "themes", "bad.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`not json`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadTheme("bad", dir)
	if err == nil {
		t.Fatal("expected error for invalid theme JSON")
	}
}

func TestLoadThemeMissingRequiredColors(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "themes", "incomplete.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"name":"incomplete","background":"#111111","foreground":"#eeeeee"}`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadTheme("incomplete", dir)
	if err == nil {
		t.Fatal("expected error for incomplete theme")
	}
	var validationErr *ThemeValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected *ThemeValidationError, got %T", err)
	}
	if len(validationErr.Missing) == 0 {
		t.Error("expected missing keys to be reported")
	}
	for _, key := range []string{"primary", "secondary", "error"} {
		found := false
		for _, missing := range validationErr.Missing {
			if missing == key {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected missing key %q in %v", key, validationErr.Missing)
		}
	}
}

func TestThemeValidationError(t *testing.T) {
	t.Parallel()

	err := &ThemeValidationError{Missing: []string{"primary", "background"}}
	got := err.Error()
	if !strings.Contains(got, "primary") || !strings.Contains(got, "background") {
		t.Errorf("error message should list missing keys, got %q", got)
	}
}
