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
func computeFileDiff(path string, proposed []byte, sandboxRoot string) string {
	return core.ComputeFileDiff(path, proposed, sandboxRoot)
}

// toolCallDiff returns a diff preview for pending write_file or str_replace_file calls.
func toolCallDiff(call api.ToolCall, sandboxRoot string) string {
	return core.ToolCallDiff(call, sandboxRoot)
}
