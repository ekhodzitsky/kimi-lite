// Package mentions provides file-path candidate sources for @-mentions.
package mentions

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// Provider returns candidate file paths for @-mention completion.
type Provider interface {
	Candidates(root string) ([]string, error)
}

// FileWalker walks the filesystem starting at root and returns relative paths.
// It skips hidden entries, symlinks, and directories beyond maxDepth.
type FileWalker struct {
	MaxDepth int
}

// Candidates implements Provider.
func (w *FileWalker) Candidates(root string) ([]string, error) {
	if root == "" {
		root = "."
	}
	maxDepth := w.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 3
	}
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip inaccessible paths
		}
		if path == root {
			return nil
		}
		base := filepath.Base(path)
		if strings.HasPrefix(base, ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		currentDepth := strings.Count(rel, string(filepath.Separator))
		if d.IsDir() {
			if currentDepth >= maxDepth {
				return fs.SkipDir
			}
			return nil
		}
		out = append(out, rel)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", root, err)
	}
	sort.Strings(out)
	return out, nil
}
