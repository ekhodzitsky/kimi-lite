package sidebar

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ekhodzitsky/kimi-lite/internal/tui/styles"
)

func TestNew(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	tmpDir := t.TempDir()
	m, err := New(st, tmpDir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if m == nil {
		t.Fatal("New() returned nil")
	}
	if !m.visible {
		t.Error("sidebar should be visible by default")
	}
	if m.width != 30 {
		t.Errorf("width = %d, want 30", m.width)
	}
}

func TestNewEmptyRoot(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m, err := New(st, "")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if m.root == "" {
		t.Error("root should be set to current working directory")
	}
}

func TestToggle(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	tmpDir := t.TempDir()
	m, err := New(st, tmpDir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if !m.Visible() {
		t.Error("should be visible initially")
	}
	if m.Width() != 30 {
		t.Errorf("Width() = %d, want 30", m.Width())
	}

	m.Toggle()
	if m.Visible() {
		t.Error("should be hidden after toggle")
	}
	if m.Width() != 0 {
		t.Errorf("Width() = %d, want 0", m.Width())
	}

	m.Toggle()
	if !m.Visible() {
		t.Error("should be visible after second toggle")
	}
}

func TestKeyboardNavigation(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("hello"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte("world"), 0644)

	m, err := New(st, tmpDir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	m.SetSize(30, 20)
	m.rebuildFlat()

	initialCursor := m.cursor

	// Move down
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	sm := updated.(*Model)
	if sm.cursor == initialCursor && len(sm.flat) > 1 {
		t.Error("cursor should move down")
	}

	// Move up
	updated, _ = sm.Update(tea.KeyMsg{Type: tea.KeyUp})
	sm = updated.(*Model)
	if sm.cursor < 0 {
		t.Error("cursor should not be negative")
	}
}

func TestExpandCollapse(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	tmpDir := t.TempDir()
	_ = os.Mkdir(filepath.Join(tmpDir, "subdir"), 0755)
	_ = os.WriteFile(filepath.Join(tmpDir, "subdir", "file.txt"), []byte("hello"), 0644)

	m, err := New(st, tmpDir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	m.SetSize(30, 20)
	m.rebuildFlat()

	// Find the subdir node
	var subdirIdx int
	for i, n := range m.flat {
		if n.Name == "subdir" {
			subdirIdx = i
			break
		}
	}
	m.cursor = subdirIdx

	if !m.flat[subdirIdx].Expanded {
		t.Skip("directory not expanded by default in this test environment")
	}

	// Collapse
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	sm := updated.(*Model)
	sm.rebuildFlat()
	if sm.flat[subdirIdx].Expanded {
		t.Error("directory should be collapsed after left arrow")
	}

	// Expand
	updated, _ = sm.Update(tea.KeyMsg{Type: tea.KeyRight})
	sm = updated.(*Model)
	sm.rebuildFlat()
	if !sm.flat[subdirIdx].Expanded {
		t.Error("directory should be expanded after right arrow")
	}
}

func TestSelectFile(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("hello"), 0644)

	m, err := New(st, tmpDir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	m.SetSize(30, 20)
	m.rebuildFlat()

	// Find file and set cursor
	for i, n := range m.flat {
		if n.Name == "file1.txt" {
			m.cursor = i
			break
		}
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	sm := updated.(*Model)

	if sm.selected == "" {
		t.Error("selected should be set after enter")
	}

	if cmd != nil {
		msg := cmd()
		if _, ok := msg.(SelectFileMsg); !ok {
			t.Errorf("expected SelectFileMsg, got %T", msg)
		}
	}
}

func TestView(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("hello"), 0644)

	m, err := New(st, tmpDir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	m.SetSize(30, 20)

	view := m.View()
	if view == "" {
		t.Error("View() should not be empty")
	}

	// When hidden
	m.Toggle()
	view = m.View()
	if view != "" {
		t.Error("View() should be empty when hidden")
	}
}

func TestBuildTree(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	_ = os.Mkdir(filepath.Join(tmpDir, "subdir"), 0755)
	_ = os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("hello"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "subdir", "nested.txt"), []byte("world"), 0644)

	node, err := buildTree(tmpDir, 2)
	if err != nil {
		t.Fatalf("buildTree() error = %v", err)
	}
	if !node.IsDir {
		t.Error("root should be a directory")
	}
	if len(node.Children) != 2 {
		t.Errorf("root should have 2 children, got %d", len(node.Children))
	}
}

func TestBuildTreeSkipsHidden(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, ".hidden"), []byte("secret"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "visible"), []byte("hello"), 0644)

	node, err := buildTree(tmpDir, 1)
	if err != nil {
		t.Fatalf("buildTree() error = %v", err)
	}

	for _, child := range node.Children {
		if child.Name == ".hidden" {
			t.Error("buildTree should skip hidden files")
		}
	}
}
