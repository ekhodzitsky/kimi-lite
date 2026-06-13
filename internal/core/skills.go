package core

import (
	"fmt"
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
// A missing directory returns an empty slice and no error.
func DiscoverSkills(dir string) ([]Skill, error) {
	var skills []Skill
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return skills, nil
	}

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(strings.ToLower(path), ".md") {
			return nil
		}
		//nolint:gosec // path is constrained to the user-owned skills directory.
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read skill %s: %w", path, err)
		}
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
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
