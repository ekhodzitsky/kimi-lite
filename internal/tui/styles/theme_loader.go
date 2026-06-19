package styles

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
)

// ThemeValidationError is returned when a custom theme is missing required
// colors. Missing contains the JSON keys that were absent or empty.
type ThemeValidationError struct {
	Missing []string
}

// Error returns a human-readable description of the missing theme keys.
func (e *ThemeValidationError) Error() string {
	return fmt.Sprintf("theme missing required colors: %s", strings.Join(e.Missing, ", "))
}

// LoadTheme loads a theme by name.
// If name is "dark" or "" it returns the built-in dark theme.
// If name is "light" it returns the built-in light theme.
// Otherwise it tries to load <configDir>/themes/<name>.json. Absolute paths
// are accepted only when they resolve inside the same themes directory.
func LoadTheme(name, configDir string) (*Theme, error) {
	switch name {
	case "dark", "":
		return &darkTheme, nil
	case "light":
		return &lightTheme, nil
	}

	themesDir := filepath.Join(configDir, "themes")
	path := name
	if !filepath.IsAbs(name) {
		path = filepath.Join(themesDir, name+".json")
	}

	path = filepath.Clean(path)
	themesDir = filepath.Clean(themesDir)
	if !isWithinDir(path, themesDir) {
		return nil, fmt.Errorf("theme path %q escapes themes directory %q", path, themesDir)
	}

	data, err := os.ReadFile(path) // #nosec G304 - path validated above
	if err != nil {
		return nil, fmt.Errorf("read theme %q: %w", path, err)
	}

	var t Theme
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("parse theme %q: %w", path, err)
	}

	// Backward-compatible defaults: any missing color fields are filled from the
	// built-in dark theme so existing partial custom themes keep loading.
	setThemeDefaults(&t)

	if err := validateTheme(&t); err != nil {
		return nil, fmt.Errorf("validate theme %q: %w", path, err)
	}

	if t.Name == "" {
		t.Name = name
	}
	return &t, nil
}

// isWithinDir reports whether path is equal to dir or contained within it.
// On case-insensitive filesystems (Windows, macOS) the comparison is
// case-insensitive; on other systems it is exact.
func isWithinDir(path, dir string) bool {
	sep := string(filepath.Separator)
	prefix := dir + sep

	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(path, dir) ||
			strings.HasPrefix(strings.ToLower(path), strings.ToLower(prefix))
	}
	return path == dir || strings.HasPrefix(path, prefix)
}

// setThemeDefaults fills any empty Color fields in t with the corresponding
// values from the built-in dark theme. This keeps older partial custom themes
// loadable while still allowing explicit overrides.
func setThemeDefaults(t *Theme) {
	tv := reflect.ValueOf(t).Elem()
	dv := reflect.ValueOf(&darkTheme).Elem()
	colorType := reflect.TypeOf(Color(""))
	for i := 0; i < tv.NumField(); i++ {
		if tv.Type().Field(i).Type != colorType {
			continue
		}
		if tv.Field(i).String() == "" {
			tv.Field(i).Set(dv.Field(i))
		}
	}
}

// validateTheme returns a structured error listing any required colors that are
// missing or empty.
func validateTheme(t *Theme) error {
	v := reflect.ValueOf(t).Elem()
	var missing []string
	for i := 0; i < v.NumField(); i++ {
		f := v.Type().Field(i)
		if f.Type != reflect.TypeOf(Color("")) {
			continue
		}
		tag := f.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		key := strings.Split(tag, ",")[0]
		if v.Field(i).String() == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return &ThemeValidationError{Missing: missing}
	}
	return nil
}
