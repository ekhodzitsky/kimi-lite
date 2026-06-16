package mentions

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func TestFileWalker_EmptyRootFallsBackToCwd(t *testing.T) {
	t.Parallel()

	w := &FileWalker{MaxDepth: 3}
	cands, err := w.Candidates("")
	if err != nil {
		t.Fatalf("Candidates(\"\") error = %v", err)
	}
	if len(cands) == 0 {
		t.Fatal("expected at least one candidate from current directory")
	}
	for _, c := range cands {
		if filepath.IsAbs(c) {
			t.Errorf("candidate %q is absolute, want relative", c)
		}
	}
}

func TestFileWalker_SkipsHiddenFilesAndDirs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustCreate(t, filepath.Join(root, "visible.txt"))
	mustCreate(t, filepath.Join(root, ".hidden"))
	mustMkdir(t, filepath.Join(root, ".hidden_dir"))
	mustCreate(t, filepath.Join(root, ".hidden_dir", "file.txt"))

	w := &FileWalker{MaxDepth: 3}
	cands, err := w.Candidates(root)
	if err != nil {
		t.Fatalf("Candidates error = %v", err)
	}
	want := []string{"visible.txt"}
	if !slices.Equal(cands, want) {
		t.Errorf("candidates = %v, want %v", cands, want)
	}
}

func TestFileWalker_SkipsSymlinks(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	target := filepath.Join(root, "target.txt")
	mustCreate(t, target)
	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("skipping symlink test: %v", err)
	}

	w := &FileWalker{MaxDepth: 3}
	cands, err := w.Candidates(root)
	if err != nil {
		t.Fatalf("Candidates error = %v", err)
	}
	want := []string{"target.txt"}
	if !slices.Equal(cands, want) {
		t.Errorf("candidates = %v, want %v", cands, want)
	}
}

func TestFileWalker_MaxDepth(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustCreate(t, filepath.Join(root, "a.txt"))
	mustMkdir(t, filepath.Join(root, "dir1"))
	mustCreate(t, filepath.Join(root, "dir1", "b.txt"))
	mustMkdir(t, filepath.Join(root, "dir1", "dir2"))
	mustCreate(t, filepath.Join(root, "dir1", "dir2", "c.txt"))

	w := &FileWalker{MaxDepth: 2}
	cands, err := w.Candidates(root)
	if err != nil {
		t.Fatalf("Candidates error = %v", err)
	}
	want := []string{"a.txt", filepath.Join("dir1", "b.txt"), filepath.Join("dir1", "dir2", "c.txt")}
	if !slices.Equal(cands, want) {
		t.Errorf("candidates = %v, want %v", cands, want)
	}
}

func TestFileWalker_DefaultDepth(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "d1", "d2", "d3", "d4"))
	mustCreate(t, filepath.Join(root, "d1", "d2", "d3", "d4", "deep.txt"))

	w := &FileWalker{MaxDepth: 0}
	cands, err := w.Candidates(root)
	if err != nil {
		t.Fatalf("Candidates error = %v", err)
	}
	// Default depth is 3, so d1/d2/d3 is included but d4 contents are skipped.
	if slices.Contains(cands, filepath.Join("d1", "d2", "d3", "d4", "deep.txt")) {
		t.Errorf("depth-4 file should not be included, got %v", cands)
	}
}

func TestFileWalker_SkipsInaccessiblePaths(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("skipping permission test on windows")
	}

	root := t.TempDir()
	mustCreate(t, filepath.Join(root, "readable.txt"))
	inaccessible := filepath.Join(root, "inaccessible")
	mustMkdir(t, inaccessible)
	mustCreate(t, filepath.Join(inaccessible, "inside.txt"))
	if err := os.Chmod(inaccessible, 0o000); err != nil {
		t.Fatalf("Chmod error = %v", err)
	}
	defer os.Chmod(inaccessible, 0o755) //nolint:errcheck // best-effort cleanup

	w := &FileWalker{MaxDepth: 3}
	cands, err := w.Candidates(root)
	if err != nil {
		t.Fatalf("Candidates error = %v", err)
	}
	want := []string{"readable.txt"}
	if !slices.Equal(cands, want) {
		t.Errorf("candidates = %v, want %v", cands, want)
	}
}

func TestFileWalker_ReturnsSortedRelativePaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustCreate(t, filepath.Join(root, "z.txt"))
	mustCreate(t, filepath.Join(root, "a.txt"))
	mustMkdir(t, filepath.Join(root, "m"))
	mustCreate(t, filepath.Join(root, "m", "n.txt"))

	w := &FileWalker{MaxDepth: 3}
	cands, err := w.Candidates(root)
	if err != nil {
		t.Fatalf("Candidates error = %v", err)
	}
	for _, c := range cands {
		if filepath.IsAbs(c) {
			t.Errorf("candidate %q is absolute, want relative", c)
		}
	}
	if !slices.IsSorted(cands) {
		t.Errorf("candidates not sorted: %v", cands)
	}
}

func mustCreate(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}
