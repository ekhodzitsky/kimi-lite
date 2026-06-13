package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

	skills, err := DiscoverSkills(context.Background(), tmpDir)
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
	skills, err := DiscoverSkills(context.Background(), "/nonexistent/path/that/does/not/exist")
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

func TestDiscoverSkills_SkipsSymlinks(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(tmpDir, "real.md"), []byte("real"), 0644); err != nil {
		t.Fatalf("write real.md: %v", err)
	}
	if err := os.Symlink(filepath.Join(tmpDir, "real.md"), filepath.Join(tmpDir, "link.md")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	skills, err := DiscoverSkills(context.Background(), tmpDir)
	if err != nil {
		t.Fatalf("DiscoverSkills: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Name != "real" {
		t.Errorf("skill name = %q, want real", skills[0].Name)
	}
}

func TestDiscoverSkills_LimitsFileSize(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	large := strings.Repeat("x", 4*1024*1024+1)
	if err := os.WriteFile(filepath.Join(tmpDir, "big.md"), []byte(large), 0644); err != nil {
		t.Fatalf("write big.md: %v", err)
	}

	skills, err := DiscoverSkills(context.Background(), tmpDir)
	if err != nil {
		t.Fatalf("DiscoverSkills: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if len(skills[0].Content) != 4*1024*1024 {
		t.Errorf("content length = %d, want %d", len(skills[0].Content), 4*1024*1024)
	}
}

func TestDiscoverSkills_DeduplicatesNames(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	a := filepath.Join(tmpDir, "a")
	b := filepath.Join(tmpDir, "b")
	if err := os.MkdirAll(a, 0755); err != nil {
		t.Fatalf("mkdir a: %v", err)
	}
	if err := os.MkdirAll(b, 0755); err != nil {
		t.Fatalf("mkdir b: %v", err)
	}
	if err := os.WriteFile(filepath.Join(a, "skill.md"), []byte("skill a"), 0644); err != nil {
		t.Fatalf("write a/skill.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(b, "skill.md"), []byte("skill b"), 0644); err != nil {
		t.Fatalf("write b/skill.md: %v", err)
	}

	skills, err := DiscoverSkills(context.Background(), tmpDir)
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
	if !names["skill"] || !names["skill_2"] {
		t.Errorf("expected skill and skill_2, got %v", names)
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
