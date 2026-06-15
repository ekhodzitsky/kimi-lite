package tui

import (
	"fmt"

	"github.com/ekhodzitsky/kimi-lite/internal/core"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
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

// toolCallDiff returns a diff preview for pending write_file or str_replace_file calls.
func toolCallDiff(call api.ToolCall, sandboxRoot string, protectedPaths []string) (string, error) {
	diff, err := core.ToolCallDiff(call, sandboxRoot, protectedPaths)
	if err != nil {
		return "", fmt.Errorf("tool call diff: %w", err)
	}
	return diff, nil
}
