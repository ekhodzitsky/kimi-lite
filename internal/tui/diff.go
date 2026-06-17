package tui

import (
	"fmt"

	"github.com/ekhodzitsky/kimi-lite/internal/core"
)

// unifiedDiff returns a unified diff between oldContent and newContent for filename.
func unifiedDiff(filename, oldContent, newContent string) string {
	return core.UnifiedDiff(filename, oldContent, newContent)
}

// computeFileDiff reads the current file and computes a diff against the proposed content.
func computeFileDiff(path string, proposed []byte, sandboxRoot string, protectedPaths []string) (string, error) {
	diff, err := core.ComputeFileDiff(path, proposed, sandboxRoot, protectedPaths)
	if err != nil {
		return "", fmt.Errorf("compute file diff: %w", err)
	}
	return diff, nil
}
