package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseLine(t *testing.T) {
	t.Parallel()

	cases := []struct {
		line     string
		wantName string
		wantNs   float64
		wantOk   bool
	}{
		{"BenchmarkFoo-10    12345    98.7 ns/op", "BenchmarkFoo-10", 98.7, true},
		{"BenchmarkBar-8     100      1234 ns/op", "BenchmarkBar-8", 1234, true},
		{"PASS", "", 0, false},
		{"ok  \tmodule\t0.123s", "", 0, false},
	}

	for _, tc := range cases {
		t.Run(tc.line, func(t *testing.T) {
			t.Parallel()
			name, ns, ok := parseLine(tc.line)
			if ok != tc.wantOk {
				t.Fatalf("parseLine ok = %v, want %v", ok, tc.wantOk)
			}
			if !ok {
				return
			}
			if name != tc.wantName || ns != tc.wantNs {
				t.Errorf("parseLine = (%q, %v), want (%q, %v)", name, ns, tc.wantName, tc.wantNs)
			}
		})
	}
}

func TestParseFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "bench.txt")
	content := strings.Join([]string{
		"BenchmarkFoo-10    12345    98.7 ns/op",
		"BenchmarkBar-8     100      1234 ns/op",
		"PASS",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	results, err := parseFile(path)
	if err != nil {
		t.Fatalf("parseFile: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results["BenchmarkFoo-10"] != 98.7 {
		t.Errorf("foo = %v, want 98.7", results["BenchmarkFoo-10"])
	}
	if results["BenchmarkBar-8"] != 1234 {
		t.Errorf("bar = %v, want 1234", results["BenchmarkBar-8"])
	}
}

func TestRun_NoRegression(t *testing.T) {
	tmpDir := t.TempDir()
	basePath := filepath.Join(tmpDir, "base.txt")
	newPath := filepath.Join(tmpDir, "new.txt")
	writeLines(t, basePath, "BenchmarkFoo-10    10000    100.0 ns/op")
	writeLines(t, newPath, "BenchmarkFoo-10    10000    105.0 ns/op")

	if err := run(basePath, newPath, 0.20); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRun_Regression(t *testing.T) {
	tmpDir := t.TempDir()
	basePath := filepath.Join(tmpDir, "base.txt")
	newPath := filepath.Join(tmpDir, "new.txt")
	writeLines(t, basePath, "BenchmarkFoo-10    10000    100.0 ns/op")
	writeLines(t, newPath, "BenchmarkFoo-10    10000    200.0 ns/op")

	if err := run(basePath, newPath, 0.20); err == nil {
		t.Fatal("expected regression error")
	}
}

func writeLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
