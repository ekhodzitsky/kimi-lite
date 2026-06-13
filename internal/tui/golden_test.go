package tui

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// updateGolden, when true, regenerates the golden files instead of comparing.
var updateGolden = flag.Bool("update", false, "update golden files")

// goldenPath returns the path to a golden file for the named snapshot.
func goldenPath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("testdata", name+".golden")
}

// compareGolden compares the rendered output against the named golden file.
// Run with -update to regenerate golden files. ANSI escape sequences are
// stripped before comparison so snapshots stay deterministic regardless of
// the terminal's color profile.
func compareGolden(t *testing.T, name, got string) {
	t.Helper()
	path := goldenPath(t, name)

	got = ansi.Strip(got)
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden file %s: %v", path, err)
		}
		return
	}

	wantBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden file %s: %v", path, err)
	}
	want := ansi.Strip(string(wantBytes))
	if got != want {
		t.Errorf("golden mismatch for %q\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}
