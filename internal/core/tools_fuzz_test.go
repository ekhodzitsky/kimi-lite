package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// FuzzIsBlockedHost ensures the local SSRF guard never panics on arbitrary
// host strings and accepts known private/loopback seeds.
func FuzzIsBlockedHost(f *testing.F) {
	seeds := []string{
		"localhost",
		"127.0.0.1",
		"0.0.0.0",
		"::1",
		"::ffff:127.0.0.1",
		"10.0.0.1",
		"172.16.0.1",
		"192.168.1.1",
		"169.254.1.1",
		"169.254.169.254",
		"100.64.0.1",
		"fd12:3456::1",
		"fc00::1",
		"fe80::1",
		"::ffff:10.0.0.1",
		"example.com",
		"8.8.8.8",
		"not-an-ip!@#$%",
		"",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		// Must not panic on any input.
		_ = isBlockedHost(input)
	})
}

// FuzzValidatePath ensures validatePath never panics on arbitrary strings and
// that any returned relative path resolves to a location within the sandbox
// root.
func FuzzValidatePath(f *testing.F) {
	seeds := []string{
		"../outside",
		"../../../etc/passwd",
		"foo/../../../etc/passwd",
		"....//....//etc/passwd",
		"/etc/passwd",
		"subdir/../..",
		"subdir/../file.txt",
		"./././file",
		"normal.txt",
		"symlink-style-name",
		"%2e%2e%2fencoded",
		strings.Repeat("../", 100),
		".",
		"..",
		"",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		tmp := t.TempDir()

		// Create a symlink inside the sandbox pointing outside.
		outside := t.TempDir()
		outsideFile := filepath.Join(outside, "secret.txt")
		if err := os.WriteFile(outsideFile, []byte("secret"), 0644); err != nil {
			t.Fatalf("write outside file: %v", err)
		}
		linkPath := filepath.Join(tmp, "link.txt")
		if err := os.Symlink(outsideFile, linkPath); err != nil {
			t.Fatalf("create symlink: %v", err)
		}

		exec, err := NewBuiltInToolExecutor(ToolExecutorConfig{SandboxRoot: tmp})
		if err != nil {
			t.Fatalf("NewBuiltInToolExecutor: %v", err)
		}
		defer exec.Close()

		// Must not panic on any input.
		rel, err := exec.validatePath(input)
		if err != nil {
			return
		}

		// If validation succeeds, the resolved absolute path must be inside the
		// sandbox root. New paths may not exist yet; fall back to the unresolved
		// absolute path in that case.
		abs := filepath.Join(exec.sandboxRoot, rel)
		resolved, err := filepath.EvalSymlinks(abs)
		if err != nil {
			resolved = abs
		}
		if !isUnder(resolved, exec.sandboxRoot) {
			t.Errorf("resolved path %q escapes sandbox %q (input %q, rel %q)", resolved, exec.sandboxRoot, input, rel)
		}
	})
}
