// Package clipboard reads image and file data from the system clipboard.
package clipboard

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const pasteTimeout = 5 * time.Second

// ReadImage reads image data from the clipboard and returns the raw bytes and
// the detected MIME type. It returns an empty result when the clipboard does
// not contain an image.
func ReadImage(ctx context.Context) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(ctx, pasteTimeout)
	defer cancel()

	switch runtime.GOOS {
	case "darwin":
		return readImageMacOS(ctx)
	case "linux":
		return readImageLinux(ctx)
	default:
		return nil, "", fmt.Errorf("clipboard image read not supported on %s", runtime.GOOS)
	}
}

// ReadFilePaths reads plain text from the clipboard and returns each non-empty
// line as a file path. This supports pasting copied files from a file manager.
func ReadFilePaths(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, pasteTimeout)
	defer cancel()

	var out []byte
	var err error
	switch runtime.GOOS {
	case "darwin":
		out, err = runCmd(ctx, "pbpaste")
	case "linux":
		out, err = runCmd(ctx, "xclip", "-selection", "clipboard", "-o")
	default:
		return nil, fmt.Errorf("clipboard file read not supported on %s", runtime.GOOS)
	}
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(out), "\n")
	var paths []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// macOS Finder copies files as file:// URLs.
		if u := strings.TrimPrefix(line, "file://"); u != line {
			line = u
		}
		paths = append(paths, line)
	}
	return paths, nil
}

// SaveData writes data to a temporary file under configDir/tmp and returns the
// absolute path. The file is created with restrictive permissions.
func SaveData(data []byte, ext string, configDir string) (string, error) {
	tmpDir := filepath.Join(configDir, "tmp")
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		return "", fmt.Errorf("create tmp dir: %w", err)
	}
	name := fmt.Sprintf("paste-%d%s", time.Now().UnixNano(), ext)
	path := filepath.Join(tmpDir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("write paste file: %w", err)
	}
	return path, nil
}

// ExtensionForMIME returns a sensible file extension for a MIME type.
func ExtensionForMIME(mime string) string {
	switch {
	case strings.HasPrefix(mime, "image/png"):
		return ".png"
	case strings.HasPrefix(mime, "image/jpeg"), strings.HasPrefix(mime, "image/jpg"):
		return ".jpg"
	case strings.HasPrefix(mime, "image/gif"):
		return ".gif"
	case strings.HasPrefix(mime, "image/webp"):
		return ".webp"
	case strings.HasPrefix(mime, "video/"):
		return "." + strings.TrimPrefix(mime, "video/")
	default:
		return ".bin"
	}
}

// MIMEForPath returns a MIME type guess based on the file extension.
func MIMEForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".mp4":
		return "video/mp4"
	case ".mov":
		return "video/quicktime"
	case ".webm":
		return "video/webm"
	default:
		return "application/octet-stream"
	}
}

func readImageMacOS(ctx context.Context) ([]byte, string, error) {
	// pbpaste only supports plain text, so use AppleScript to read the clipboard
	// as PNG bytes.
	script := `try
	set pngData to (the clipboard as «class PNGf»)
	return pngData
end try`
	out, err := runCmd(ctx, "osascript", "-e", script)
	if err != nil {
		return nil, "", fmt.Errorf("no image in clipboard")
	}
	if len(out) == 0 {
		return nil, "", fmt.Errorf("no image in clipboard")
	}
	return out, "image/png", nil
}

func readImageLinux(ctx context.Context) ([]byte, string, error) {
	out, err := runCmd(ctx, "xclip", "-selection", "clipboard", "-t", "image/png", "-o")
	if err != nil {
		return nil, "", fmt.Errorf("no image in clipboard")
	}
	if len(out) == 0 {
		return nil, "", fmt.Errorf("no image in clipboard")
	}
	return out, "image/png", nil
}

func runCmd(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // command names are hard-coded per OS
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("%s: %s", name, bytes.TrimSpace(ee.Stderr))
		}
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return out, nil
}
