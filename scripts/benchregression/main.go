// Command benchregression parses Go benchmark output and compares it against a
// baseline. It exits with a non-zero status if any benchmark regressed beyond
// the configured threshold.
package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: benchregression <baseline> <new> [threshold]")
		os.Exit(2)
	}
	threshold := 0.20
	if len(os.Args) > 3 {
		v, err := strconv.ParseFloat(os.Args[3], 64)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid threshold: %v\n", err)
			os.Exit(2)
		}
		threshold = v
	}
	if err := run(os.Args[1], os.Args[2], threshold); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(baselinePath, newPath string, threshold float64) error {
	baseline, err := parseFile(baselinePath)
	if err != nil {
		return fmt.Errorf("parse baseline: %w", err)
	}
	newResults, err := parseFile(newPath)
	if err != nil {
		return fmt.Errorf("parse new results: %w", err)
	}

	failed := false
	for name, ns := range newResults {
		base, ok := baseline[name]
		if !ok {
			fmt.Printf("new benchmark: %s\n", name)
			continue
		}
		if base == 0 {
			fmt.Printf("no baseline measurement for %s\n", name)
			continue
		}
		increase := (ns - base) / base
		if increase > threshold {
			fmt.Printf("REGRESSION: %s %.2f%% slower (%.2f -> %.2f ns/op)\n", name, increase*100, base, ns)
			failed = true
		} else {
			fmt.Printf("ok: %s %.2f%% change (%.2f -> %.2f ns/op)\n", name, increase*100, base, ns)
		}
	}
	for name := range baseline {
		if _, ok := newResults[name]; !ok {
			fmt.Printf("missing benchmark: %s\n", name)
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
