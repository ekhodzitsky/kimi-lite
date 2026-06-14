package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseLine(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		line     string
		wantName string
		wantNs   float64
		wantOk   bool
	}{
		{"standard", "BenchmarkFoo-10    12345    98.7 ns/op", "BenchmarkFoo-10", 98.7, true},
		{"different-procs", "BenchmarkBar-8     100      1234 ns/op", "BenchmarkBar-8", 1234, true},
		{"PASS", "PASS", "", 0, false},
		{"ok-line", "ok  \tmodule\t0.123s", "", 0, false},
		{"not-benchmark", "SomeOtherLine-10   100      1234 ns/op", "", 0, false},
		{"benchmark-no-fields", "BenchmarkShort", "", 0, false},
		{"benchmark-one-field", "BenchmarkShort 100", "", 0, false},
		{"benchmark-no-nsop", "BenchmarkNoNs-10   100      1234 MB/s", "", 0, false},
		{"benchmark-invalid-float", "BenchmarkBad-10    100      abc ns/op", "", 0, false},
		{"whitespace-only", "   ", "", 0, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
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

func TestParseFile_MissingPath(t *testing.T) {
	t.Parallel()

	_, err := parseFile(filepath.Join(t.TempDir(), "does-not-exist.txt"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestParseFile_ScannerError(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "bench.txt")
	// A single line exceeding bufio.Scanner's default max token size (64 KiB)
	// causes scanner.Err() to return after the file is successfully opened.
	longLine := strings.Repeat("x", 128*1024)
	if err := os.WriteFile(path, []byte(longLine), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, err := parseFile(path)
	if err == nil {
		t.Fatal("expected scanner error")
	}
}

func TestRun_NoRegression(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	basePath := filepath.Join(tmpDir, "base.txt")
	newPath := filepath.Join(tmpDir, "new.txt")
	writeLines(t, basePath, "BenchmarkFoo-10    10000    100.0 ns/op")
	writeLines(t, newPath, "BenchmarkFoo-10    10000    105.0 ns/op")

	var out bytes.Buffer
	if err := run([]string{"benchregression", basePath, newPath, "0.20"}, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "ok:") {
		t.Errorf("expected ok output, got %q", out.String())
	}
}

func TestRun_Regression(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	basePath := filepath.Join(tmpDir, "base.txt")
	newPath := filepath.Join(tmpDir, "new.txt")
	writeLines(t, basePath, "BenchmarkFoo-10    10000    100.0 ns/op")
	writeLines(t, newPath, "BenchmarkFoo-10    10000    200.0 ns/op")

	var out bytes.Buffer
	if err := run([]string{"benchregression", basePath, newPath, "0.20"}, &out); err == nil {
		t.Fatal("expected regression error")
	} else if !strings.Contains(out.String(), "REGRESSION:") {
		t.Errorf("expected REGRESSION output, got %q", out.String())
	}
}

func TestRun_Improvement(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	basePath := filepath.Join(tmpDir, "base.txt")
	newPath := filepath.Join(tmpDir, "new.txt")
	writeLines(t, basePath, "BenchmarkFoo-10    10000    100.0 ns/op")
	writeLines(t, newPath, "BenchmarkFoo-10    10000    50.0 ns/op")

	var out bytes.Buffer
	if err := run([]string{"benchregression", basePath, newPath, "0.20"}, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "ok:") {
		t.Errorf("expected ok output, got %q", out.String())
	}
}

func TestRun_NewBenchmark(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	basePath := filepath.Join(tmpDir, "base.txt")
	newPath := filepath.Join(tmpDir, "new.txt")
	writeLines(t, basePath, "BenchmarkFoo-10    10000    100.0 ns/op")
	writeLines(t, newPath,
		"BenchmarkFoo-10    10000    100.0 ns/op",
		"BenchmarkBar-8     100      1234 ns/op",
	)

	var out bytes.Buffer
	if err := run([]string{"benchregression", basePath, newPath, "0.20"}, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "new benchmark:") {
		t.Errorf("expected new benchmark output, got %q", out.String())
	}
}

func TestRun_ZeroBaseline(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	basePath := filepath.Join(tmpDir, "base.txt")
	newPath := filepath.Join(tmpDir, "new.txt")
	writeLines(t, basePath, "BenchmarkFoo-10    10000    0 ns/op")
	writeLines(t, newPath, "BenchmarkFoo-10    10000    100.0 ns/op")

	var out bytes.Buffer
	if err := run([]string{"benchregression", basePath, newPath, "0.20"}, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "no baseline measurement") {
		t.Errorf("expected no baseline measurement output, got %q", out.String())
	}
}

func TestRun_MissingBenchmark(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	basePath := filepath.Join(tmpDir, "base.txt")
	newPath := filepath.Join(tmpDir, "new.txt")
	writeLines(t, basePath,
		"BenchmarkFoo-10    10000    100.0 ns/op",
		"BenchmarkBar-8     100      1234 ns/op",
	)
	writeLines(t, newPath, "BenchmarkFoo-10    10000    100.0 ns/op")

	var out bytes.Buffer
	if err := run([]string{"benchregression", basePath, newPath, "0.20"}, &out); err == nil {
		t.Fatal("expected regression error")
	} else if !strings.Contains(out.String(), "missing benchmark:") {
		t.Errorf("expected missing benchmark output, got %q", out.String())
	}
}

func TestRun_ParseBaselineError(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	basePath := filepath.Join(tmpDir, "base.txt")
	newPath := filepath.Join(tmpDir, "new.txt")
	writeLines(t, newPath, "BenchmarkFoo-10    10000    100.0 ns/op")

	var out bytes.Buffer
	if err := run([]string{"benchregression", basePath, newPath}, &out); err == nil {
		t.Fatal("expected error")
	}
}

func TestRun_ParseNewError(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	basePath := filepath.Join(tmpDir, "base.txt")
	newPath := filepath.Join(tmpDir, "new.txt")
	writeLines(t, basePath, "BenchmarkFoo-10    10000    100.0 ns/op")

	var out bytes.Buffer
	if err := run([]string{"benchregression", basePath, newPath}, &out); err == nil {
		t.Fatal("expected error")
	}
}

func TestRun_UsageError(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	err := run([]string{"benchregression"}, &out)
	if err == nil {
		t.Fatal("expected usage error")
	}
	if !strings.Contains(err.Error(), "usage:") {
		t.Errorf("expected usage error, got %v", err)
	}
	if ee, ok := err.(*exitError); !ok || ee.code != 2 {
		t.Errorf("expected exit code 2 exitError, got %T %v", err, err)
	}
}

func TestRun_InvalidThreshold(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	err := run([]string{"benchregression", "base.txt", "new.txt", "not-a-number"}, &out)
	if err == nil {
		t.Fatal("expected invalid threshold error")
	}
	if !strings.Contains(err.Error(), "invalid threshold") {
		t.Errorf("expected invalid threshold error, got %v", err)
	}
	if ee, ok := err.(*exitError); !ok || ee.code != 2 {
		t.Errorf("expected exit code 2 exitError, got %T %v", err, err)
	}
}

func TestRun_DefaultThreshold(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	basePath := filepath.Join(tmpDir, "base.txt")
	newPath := filepath.Join(tmpDir, "new.txt")
	writeLines(t, basePath, "BenchmarkFoo-10    10000    100.0 ns/op")
	writeLines(t, newPath, "BenchmarkFoo-10    10000    119.0 ns/op")

	var out bytes.Buffer
	if err := run([]string{"benchregression", basePath, newPath}, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMain_Success(t *testing.T) {
	tmpDir := t.TempDir()
	basePath := filepath.Join(tmpDir, "base.txt")
	newPath := filepath.Join(tmpDir, "new.txt")
	writeLines(t, basePath, "BenchmarkFoo-10    10000    100.0 ns/op")
	writeLines(t, newPath, "BenchmarkFoo-10    10000    105.0 ns/op")

	oldArgs := os.Args
	oldExit := osExit
	defer func() {
		os.Args = oldArgs
		osExit = oldExit
	}()

	var exitCode int
	osExit = func(code int) { exitCode = code }
	os.Args = []string{"benchregression", basePath, newPath, "0.20"}

	main()

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
}

func TestMain_UsageError(t *testing.T) {
	oldArgs := os.Args
	oldExit := osExit
	defer func() {
		os.Args = oldArgs
		osExit = oldExit
	}()

	var exitCode int
	osExit = func(code int) { exitCode = code }
	os.Args = []string{"benchregression"}

	main()

	if exitCode != 2 {
		t.Fatalf("expected exit code 2, got %d", exitCode)
	}
}

func TestMain_InvalidThreshold(t *testing.T) {
	oldArgs := os.Args
	oldExit := osExit
	defer func() {
		os.Args = oldArgs
		osExit = oldExit
	}()

	var exitCode int
	osExit = func(code int) { exitCode = code }
	os.Args = []string{"benchregression", "base.txt", "new.txt", "bad"}

	main()

	if exitCode != 2 {
		t.Fatalf("expected exit code 2, got %d", exitCode)
	}
}

func TestMain_Regression(t *testing.T) {
	tmpDir := t.TempDir()
	basePath := filepath.Join(tmpDir, "base.txt")
	newPath := filepath.Join(tmpDir, "new.txt")
	writeLines(t, basePath, "BenchmarkFoo-10    10000    100.0 ns/op")
	writeLines(t, newPath, "BenchmarkFoo-10    10000    200.0 ns/op")

	oldArgs := os.Args
	oldExit := osExit
	defer func() {
		os.Args = oldArgs
		osExit = oldExit
	}()

	var exitCode int
	osExit = func(code int) { exitCode = code }
	os.Args = []string{"benchregression", basePath, newPath, "0.20"}

	main()

	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
}

func writeLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
