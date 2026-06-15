package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hexops/gotextdiff"
	"github.com/hexops/gotextdiff/myers"
	"github.com/hexops/gotextdiff/span"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// Sentinel errors returned by diff helpers.
var (
	ErrDiffPathBlocked      = errors.New("diff path blocked")
	ErrDiffFileTooLarge     = errors.New("diff file too large")
	ErrDiffInvalidArguments = errors.New("diff invalid arguments")
)

// UnifiedDiff returns a unified diff between oldContent and newContent for filename.
func UnifiedDiff(filename, oldContent, newContent string) string {
	edits := myers.ComputeEdits(span.URIFromPath(filename), oldContent, newContent)
	return fmt.Sprint(gotextdiff.ToUnified(filename, filename, oldContent, edits))
}

// readFileForDiff reads path for diff preview, enforcing maxFileReadSize.
// A missing file is treated as empty content without an error.
func readFileForDiff(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("stat file: %w", err)
	}
	if info.Size() > maxFileReadSize {
		return "", fmt.Errorf("%w: %d bytes exceeds max %d bytes", ErrDiffFileTooLarge, info.Size(), maxFileReadSize)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	return string(data), nil
}

// ComputeFileDiff reads the current file and computes a diff against the proposed content.
// protectedPaths is an optional list of additional paths that must be blocked
// (mirroring BuiltInToolExecutor.protectedPaths).
func ComputeFileDiff(path string, proposed []byte, sandboxRoot string, protectedPaths []string) (string, error) {
	if path == "" {
		return "", nil
	}
	if sandboxRoot != "" && !filepath.IsAbs(path) {
		path = filepath.Join(sandboxRoot, path)
	}
	validPath, err := ValidateFilePath(path, sandboxRoot, protectedPaths)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrDiffPathBlocked, err)
	}
	oldContent, err := readFileForDiff(validPath)
	if err != nil {
		return "", err
	}
	return UnifiedDiff(path, oldContent, string(proposed)), nil
}

// ToolCallDiff returns a diff preview for pending write_file or str_replace_file calls.
// protectedPaths is an optional list of additional paths that must be blocked
// (mirroring BuiltInToolExecutor.protectedPaths).
func ToolCallDiff(call api.ToolCall, sandboxRoot string, protectedPaths []string) (string, error) {
	switch call.Name {
	case "write_file":
		var args struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
			return "", fmt.Errorf("%w: %w", ErrDiffInvalidArguments, err)
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
			return "", fmt.Errorf("%w: %w", ErrDiffInvalidArguments, err)
		}
		if sandboxRoot != "" && !filepath.IsAbs(args.Path) {
			args.Path = filepath.Join(sandboxRoot, args.Path)
		}
		validPath, err := ValidateFilePath(args.Path, sandboxRoot, protectedPaths)
		if err != nil {
			return "", fmt.Errorf("%w: %w", ErrDiffPathBlocked, err)
		}
		oldContent, err := readFileForDiff(validPath)
		if err != nil {
			return "", err
		}
		var newContent string
		if args.ReplaceAll {
			newContent = strings.ReplaceAll(oldContent, args.OldString, args.NewString)
		} else {
			newContent = strings.Replace(oldContent, args.OldString, args.NewString, 1)
		}
		return UnifiedDiff(args.Path, oldContent, newContent), nil
	}
	return "", nil
}
