package clipboard

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestExtensionForMIME(t *testing.T) {
	t.Parallel()
	tests := []struct {
		mime string
		want string
	}{
		{"image/png", ".png"},
		{"image/jpeg", ".jpg"},
		{"image/gif", ".gif"},
		{"image/webp", ".webp"},
		{"video/mp4", ".mp4"},
		{"application/octet-stream", ".bin"},
	}
	for _, tt := range tests {
		t.Run(tt.mime, func(t *testing.T) {
			t.Parallel()
			if got := ExtensionForMIME(tt.mime); got != tt.want {
				t.Errorf("ExtensionForMIME(%q) = %q, want %q", tt.mime, got, tt.want)
			}
		})
	}
}

func TestMIMEForPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		want string
	}{
		{"/tmp/foo.png", "image/png"},
		{"/tmp/foo.jpg", "image/jpeg"},
		{"/tmp/foo.JPG", "image/jpeg"},
		{"/tmp/foo.mp4", "video/mp4"},
		{"/tmp/foo.bin", "application/octet-stream"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			if got := MIMEForPath(tt.path); got != tt.want {
				t.Errorf("MIMEForPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestReadFilePaths_macOS(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS only")
	}
	t.Parallel()
	// We cannot reliably control the system clipboard in tests; just ensure the
	// function runs without panic and returns a non-error for an empty clip.
	_, err := ReadFilePaths(t.Context())
	if err != nil {
		t.Logf("ReadFilePaths returned error (expected if clipboard is empty): %v", err)
	}
}

func TestSaveData(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	data := []byte("hello paste")
	path, err := SaveData(data, ".txt", tmpDir)
	if err != nil {
		t.Fatalf("SaveData error: %v", err)
	}
	if !filepath.IsAbs(path) {
		t.Errorf("SaveData returned relative path: %q", path)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("saved data = %q, want %q", got, data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat saved file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("saved file permissions = %o, want %o", info.Mode().Perm(), 0o600)
	}
}

func TestCopyFileToTemp(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "source.txt")
	data := []byte("hello paste")
	if err := os.WriteFile(src, data, 0o600); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	dst, err := CopyFileToTemp(src, tmpDir)
	if err != nil {
		t.Fatalf("CopyFileToTemp error: %v", err)
	}
	if !filepath.IsAbs(dst) {
		t.Errorf("CopyFileToTemp returned relative path: %q", dst)
	}
	if !strings.Contains(dst, filepath.Join(tmpDir, "tmp")) {
		t.Errorf("destination = %q, want under configDir/tmp", dst)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read copied file: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("copied data = %q, want %q", got, data)
	}

	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("stat copied file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("copied file permissions = %o, want %o", info.Mode().Perm(), 0o600)
	}
}

func TestCopyFileToTemp_SizeCap(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "huge.bin")
	if err := os.WriteFile(src, make([]byte, MaxPasteFileSize+1), 0o600); err != nil {
		t.Fatalf("write large source file: %v", err)
	}

	_, err := CopyFileToTemp(src, tmpDir)
	if err == nil {
		t.Fatal("expected error for oversized file")
	}
}

func TestReadFilePaths_WindowsFallback(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows only")
	}
	t.Parallel()
	// We cannot reliably control the system clipboard in tests; just ensure the
	// PowerShell fallback runs without panic.
	_, err := ReadFilePaths(t.Context())
	if err != nil {
		t.Logf("ReadFilePaths returned error (expected if clipboard is empty): %v", err)
	}
}
