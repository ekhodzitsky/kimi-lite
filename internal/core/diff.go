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
func ComputeFileDiff(path string, proposed []byte, sandboxRoot string) string {
	if path == "" {
		return ""
	}
	if sandboxRoot != "" && !filepath.IsAbs(path) {
		path = filepath.Join(sandboxRoot, path)
	}
	validPath, err := ValidateFilePath(path, sandboxRoot, nil)
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
func ToolCallDiff(call api.ToolCall, sandboxRoot string) string {
	switch call.Name {
	case "write_file":
		var args struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
			return ""
		}
		return ComputeFileDiff(args.Path, []byte(args.Content), sandboxRoot)
	case "str_replace_file":
		var args struct {
			Path      string `json:"path"`
			OldString string `json:"old_string"`
			NewString string `json:"new_string"`
		}
		if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
			return ""
		}
		if sandboxRoot != "" && !filepath.IsAbs(args.Path) {
			args.Path = filepath.Join(sandboxRoot, args.Path)
		}
		validPath, err := ValidateFilePath(args.Path, sandboxRoot, nil)
		if err != nil {
			return ""
		}
		oldContent := ""
		// #nosec G304 -- path originates from validated tool-call arguments.
		if data, err := os.ReadFile(validPath); err == nil {
			oldContent = string(data)
		}
		newContent := strings.ReplaceAll(oldContent, args.OldString, args.NewString)
		return UnifiedDiff(args.Path, oldContent, newContent)
	}
	return ""
}
