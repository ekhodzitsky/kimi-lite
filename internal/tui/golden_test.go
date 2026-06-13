package tui

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// updateGolden, when true, regenerates the golden files instead of comparing.
var updateGolden = flag.Bool("update", false, "update golden files")

// TestMain pins the lipgloss color profile to ASCII so that golden-file
// comparisons are deterministic across terminals and CI environments.
func TestMain(m *testing.M) {
	flag.Parse()
	lipgloss.SetColorProfile(termenv.Ascii)
	os.Exit(m.Run())
}

// goldenPath returns the path to a golden file for the named snapshot.
func goldenPath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("testdata", name+".golden")
}

// compareGolden compares the rendered output against the named golden file.
// Run with -update to regenerate golden files.
func compareGolden(t *testing.T, name, got string) {
	t.Helper()
	path := goldenPath(t, name)

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
	want := string(wantBytes)
	if got != want {
		t.Errorf("golden mismatch for %q\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}
