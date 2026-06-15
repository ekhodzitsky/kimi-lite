package tui

import (
	"github.com/ekhodzitsky/kimi-lite/internal/core"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// unifiedDiff returns a unified diff between oldContent and newContent for filename.
func unifiedDiff(filename, oldContent, newContent string) string {
	return core.UnifiedDiff(filename, oldContent, newContent)
}

// computeFileDiff reads the current file and computes a diff against the proposed content.
func computeFileDiff(path string, proposed []byte, sandboxRoot string, protectedPaths []string) (string, error) {
	return core.ComputeFileDiff(path, proposed, sandboxRoot, protectedPaths)
}

// toolCallDiff returns a diff preview for pending write_file or str_replace_file calls.
func toolCallDiff(call api.ToolCall, sandboxRoot string, protectedPaths []string) (string, error) {
	return core.ToolCallDiff(call, sandboxRoot, protectedPaths)
}
