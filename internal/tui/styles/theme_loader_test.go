package styles

import (
	"os"
	"path/filepath"
	"testing"
)

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
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"name":"custom","background":"#111111","foreground":"#eeeeee","primary":"#ff0000"}`)
	if err := os.WriteFile(path, data, 0644); err != nil {
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
}

func TestLoadThemeCustomAbsolutePath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "my-theme.json")
	data := []byte(`{"name":"absolute","background":"#222222"}`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	theme, err := LoadTheme(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if theme.Name != "absolute" {
		t.Errorf("expected name %q, got %q", "absolute", theme.Name)
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
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`not json`), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadTheme("bad", dir)
	if err == nil {
		t.Fatal("expected error for invalid theme JSON")
	}
}
