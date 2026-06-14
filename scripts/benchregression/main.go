// Command benchregression parses Go benchmark output and compares it against a
// baseline. It exits with a non-zero status if any benchmark regressed beyond
// the configured threshold.
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// osExit is swapped out in tests so main() can be exercised without killing the
// test process.
var osExit = os.Exit

// exitError carries a specific exit code so main() can distinguish usage errors
// (code 2) from benchmark regressions (code 1).
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }

func main() {
	if err := run(os.Args, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		code := 1
		if ee, ok := err.(*exitError); ok {
			code = ee.code
		}
		osExit(code)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) < 3 {
		return &exitError{code: 2, err: fmt.Errorf("usage: benchregression <baseline> <new> [threshold]")}
	}
	threshold := 0.20
	if len(args) > 3 {
		v, err := strconv.ParseFloat(args[3], 64)
		if err != nil {
			return &exitError{code: 2, err: fmt.Errorf("invalid threshold: %w", err)}
		}
		threshold = v
	}

	baseline, err := parseFile(args[1])
	if err != nil {
		return fmt.Errorf("parse baseline: %w", err)
	}
	newResults, err := parseFile(args[2])
	if err != nil {
		return fmt.Errorf("parse new results: %w", err)
	}

	failed := false
	for name, ns := range newResults {
		base, ok := baseline[name]
		if !ok {
			_, _ = fmt.Fprintf(out, "new benchmark: %s\n", name)
			continue
		}
		if base == 0 {
			_, _ = fmt.Fprintf(out, "no baseline measurement for %s\n", name)
			continue
		}
		increase := (ns - base) / base
		if increase > threshold {
			_, _ = fmt.Fprintf(out, "REGRESSION: %s %.2f%% slower (%.2f -> %.2f ns/op)\n", name, increase*100, base, ns)
			failed = true
		} else {
			_, _ = fmt.Fprintf(out, "ok: %s %.2f%% change (%.2f -> %.2f ns/op)\n", name, increase*100, base, ns)
		}
	}
	for name := range baseline {
		if _, ok := newResults[name]; !ok {
			_, _ = fmt.Fprintf(out, "missing benchmark: %s\n", name)
			failed = true
		}
	}

	if failed {
		return fmt.Errorf("benchmark regression detected")
	}
	return nil
}

// parseFile reads benchmark results from path. Each line is expected to match
// the standard go test -bench output format.
func parseFile(path string) (map[string]float64, error) {
	//nolint:gosec // path is provided by the caller (Makefile or CI), not user input.
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	results := make(map[string]float64)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		name, ns, ok := parseLine(line)
		if !ok {
			continue
		}
		results[name] = ns
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}
	return results, nil
}

// parseLine extracts the benchmark name and ns/op value from a line.
// Example: BenchmarkFoo-10    12345    98.7 ns/op
func parseLine(line string) (string, float64, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "Benchmark") {
		return "", 0, false
	}
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return "", 0, false
	}
	name := fields[0]
	for i := 2; i < len(fields); i++ {
		if fields[i] == "ns/op" && i > 0 {
			v, err := strconv.ParseFloat(fields[i-1], 64)
			if err != nil {
				return "", 0, false
			}
			return name, v, true
		}
	}
	return "", 0, false
}
