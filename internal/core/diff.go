package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hexops/gotextdiff"
	"github.com/hexops/gotextdiff/myers"
	"github.com/hexops/gotextdiff/span"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// UnifiedDiff returns a unified diff between oldContent and newContent for filename.
func UnifiedDiff(filename, oldContent, newContent string) string {
	edits := myers.ComputeEdits(span.URIFromPath(filename), oldContent, newContent)
	return fmt.Sprint(gotextdiff.ToUnified(filename, filename, oldContent, edits))
}

// ComputeFileDiff reads the current file and computes a diff against the proposed content.
// protectedPaths is an optional list of additional paths that must be blocked
// (mirroring BuiltInToolExecutor.protectedPaths).
func ComputeFileDiff(path string, proposed []byte, sandboxRoot string, protectedPaths []string) string {
	if path == "" {
		return ""
	}
	if sandboxRoot != "" && !filepath.IsAbs(path) {
		path = filepath.Join(sandboxRoot, path)
	}
	validPath, err := ValidateFilePath(path, sandboxRoot, protectedPaths)
	if err != nil {
		return ""
	}
	oldContent := ""
	// #nosec G304 -- path originates from validated tool-call arguments.
	if data, err := os.ReadFile(validPath); err == nil {
		oldContent = string(data)
	}
	return UnifiedDiff(path, oldContent, string(proposed))
}

// ToolCallDiff returns a diff preview for pending write_file or str_replace_file calls.
// protectedPaths is an optional list of additional paths that must be blocked
// (mirroring BuiltInToolExecutor.protectedPaths).
func ToolCallDiff(call api.ToolCall, sandboxRoot string, protectedPaths []string) string {
	switch call.Name {
	case "write_file":
		var args struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
			return ""
		}
		return ComputeFileDiff(args.Path, []byte(args.Content), sandboxRoot, protectedPaths)
	case "str_replace_file":
		var args struct {
			Path       string `json:"path"`
			OldString  string `json:"old_string"`
			NewString  string `json:"new_string"`
			ReplaceAll bool   `json:"replace_all"`
		}
		if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
			return ""
		}
		if sandboxRoot != "" && !filepath.IsAbs(args.Path) {
			args.Path = filepath.Join(sandboxRoot, args.Path)
		}
		validPath, err := ValidateFilePath(args.Path, sandboxRoot, protectedPaths)
		if err != nil {
			return ""
		}
		oldContent := ""
		// #nosec G304 -- path originates from validated tool-call arguments.
		if data, err := os.ReadFile(validPath); err == nil {
			oldContent = string(data)
		}
		var newContent string
		if args.ReplaceAll {
			newContent = strings.ReplaceAll(oldContent, args.OldString, args.NewString)
		} else {
			newContent = strings.Replace(oldContent, args.OldString, args.NewString, 1)
		}
		return UnifiedDiff(args.Path, oldContent, newContent)
	}
	return ""
}
