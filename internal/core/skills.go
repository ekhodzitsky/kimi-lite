package core

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Skill represents a user-provided skill loaded from a markdown file.
type Skill struct {
	Name    string
	Content string
}

// DiscoverSkills walks dir recursively and loads every *.md file as a skill.
// A missing directory returns an empty slice and no error. Symlinks are skipped
// to prevent directory traversal outside dir, and each skill file is capped at
// 4 MiB. The context is checked before each file is read.
func DiscoverSkills(ctx context.Context, dir string) ([]Skill, error) {
	var skills []Skill
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return skills, nil
	}

	seen := make(map[string]int)
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(strings.ToLower(path), ".md") {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("load skills: %w", err)
		}

		// #nosec G304 G122 -- skill paths originate from the configured skills directory walk; symlink check is done above.
		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open skill %s: %w", path, err)
		}
		// Cap skill files at 4 MiB to avoid unbounded reads.
		const maxSkillSize = 4 * 1024 * 1024
		data, err := io.ReadAll(io.LimitReader(f, maxSkillSize))
		_ = f.Close()
		if err != nil {
			return fmt.Errorf("read skill %s: %w", path, err)
		}

		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if count, ok := seen[name]; ok {
			count++
			seen[name] = count
			name = fmt.Sprintf("%s_%d", name, count)
		} else {
			seen[name] = 1
		}
		skills = append(skills, Skill{
			Name:    name,
			Content: string(data),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk skills directory: %w", err)
	}
	return skills, nil
}

// FilterSkills returns only the skills whose names appear in names. If names is
// empty, all skills are returned.
func FilterSkills(all []Skill, names []string) []Skill {
	if len(names) == 0 {
		return all
	}
	want := make(map[string]struct{}, len(names))
	for _, n := range names {
		want[n] = struct{}{}
	}
	var out []Skill
	for _, s := range all {
		if _, ok := want[s.Name]; ok {
			out = append(out, s)
		}
	}
	return out
}

// LoadSkillContent concatenates skill contents into a single system-prompt
// fragment.
func LoadSkillContent(skills []Skill) string {
	var b strings.Builder
	for _, s := range skills {
		if b.Len() > 0 {
			b.WriteString("\n\n---\n\n")
		}
		fmt.Fprintf(&b, "# Skill: %s\n\n%s", s.Name, s.Content)
	}
	return b.String()
}
