package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverSkills(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(tmpDir, "python.md"), []byte("# Python\nUse black."), 0644); err != nil {
		t.Fatalf("write python.md: %v", err)
	}
	nested := filepath.Join(tmpDir, "go")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nested, "golang.md"), []byte("# Go\nUse gofmt."), 0644); err != nil {
		t.Fatalf("write golang.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "notes.txt"), []byte("ignored"), 0644); err != nil {
		t.Fatalf("write notes.txt: %v", err)
	}

	skills, err := DiscoverSkills(tmpDir)
	if err != nil {
		t.Fatalf("DiscoverSkills: %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(skills))
	}

	names := make(map[string]bool)
	for _, s := range skills {
		names[s.Name] = true
	}
	if !names["python"] || !names["golang"] {
		t.Errorf("expected python and golang skills, got %v", names)
	}
}

func TestDiscoverSkills_MissingDir(t *testing.T) {
	t.Parallel()
	skills, err := DiscoverSkills("/nonexistent/path/that/does/not/exist")
	if err != nil {
		t.Fatalf("expected no error for missing dir, got %v", err)
	}
	if len(skills) != 0 {
		t.Errorf("expected 0 skills, got %d", len(skills))
	}
}

func TestFilterSkills(t *testing.T) {
	t.Parallel()
	all := []Skill{
		{Name: "python", Content: "python"},
		{Name: "go", Content: "go"},
		{Name: "rust", Content: "rust"},
	}

	filtered := FilterSkills(all, []string{"python", "rust"})
	if len(filtered) != 2 {
		t.Fatalf("expected 2 filtered skills, got %d", len(filtered))
	}

	allAgain := FilterSkills(all, nil)
	if len(allAgain) != 3 {
		t.Errorf("expected all 3 skills when names empty, got %d", len(allAgain))
	}
}

func TestLoadSkillContent(t *testing.T) {
	t.Parallel()
	skills := []Skill{
		{Name: "python", Content: "Use black."},
		{Name: "go", Content: "Use gofmt."},
	}
	content := LoadSkillContent(skills)
	if content == "" {
		t.Fatal("expected non-empty content")
	}
	if !contains(content, "Use black.") || !contains(content, "Use gofmt.") {
		t.Errorf("content missing skill text: %q", content)
	}
}

func contains(s, substr string) bool {
	return containsString(s, substr)
}

func containsString(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
