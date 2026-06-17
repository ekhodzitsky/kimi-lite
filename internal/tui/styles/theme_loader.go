package styles

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// LoadTheme loads a theme by name.
// If name is "dark" or "" it returns the built-in dark theme.
// If name is "light" it returns the built-in light theme.
// Otherwise it tries to load <configDir>/themes/<name>.json, or the file at
// name if it is an absolute path.
func LoadTheme(name, configDir string) (*Theme, error) {
	switch name {
	case "dark", "":
		return &darkTheme, nil
	case "light":
		return &lightTheme, nil
	}

	path := name
	if !filepath.IsAbs(name) {
		path = filepath.Join(configDir, "themes", name+".json")
	}

	path = filepath.Clean(path)
	data, err := os.ReadFile(path) // #nosec G304
	if err != nil {
		return nil, fmt.Errorf("read theme %q: %w", path, err)
	}

	var t Theme
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("parse theme %q: %w", path, err)
	}
	if t.Name == "" {
		t.Name = name
	}
	return &t, nil
}
