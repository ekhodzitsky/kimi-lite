package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// FuzzValidatePath exercises BuiltInToolExecutor.validatePath with arbitrary
// inputs. It ensures the validator never panics and that any path it accepts
// stays within the configured sandbox root.
func FuzzValidatePath(f *testing.F) {
	seeds := []string{
		"file.txt",
		"subdir/file.txt",
		"../escape",
		"foo/../../../etc/passwd",
		"....//....//etc/passwd",
		"subdir/../file.txt",
		"/absolute/outside",
		"link",
		"",
		".",
		"..",
		"./././file",
	}

	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, path string) {
		sandbox := t.TempDir()

		// Create a symlink inside the sandbox that points outside; validatePath
		// must either reject it or resolve it to a path still under the root.
		_ = os.Symlink("/etc/passwd", filepath.Join(sandbox, "link"))

		exec, err := NewBuiltInToolExecutor(ToolExecutorConfig{SandboxRoot: sandbox})
		if err != nil {
			t.Fatalf("create executor: %v", err)
		}
		defer exec.Close()

		rel, err := exec.validatePath(path)
		if err != nil {
			return
		}

		// A successful validation with a sandbox root must return a relative
		// path whose joined absolute path is inside the sandbox.
		sep := string(filepath.Separator)
		resolvedRoot := exec.sandboxRoot
		joined := filepath.Join(resolvedRoot, rel)
		cleaned := filepath.Clean(joined)

		if cleaned != resolvedRoot && !strings.HasPrefix(cleaned, resolvedRoot+sep) {
			t.Errorf("validatePath(%q) accepted escaped path: rel=%q joined=%q root=%q", path, rel, cleaned, resolvedRoot)
		}
	})
}
