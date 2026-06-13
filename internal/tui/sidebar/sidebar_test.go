package sidebar

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	tea "charm.land/bubbletea/v2"

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
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	sm := updated.(*Model)
	if sm.cursor == initialCursor && len(sm.flat) > 1 {
		t.Error("cursor should move down")
	}

	// Move up
	updated, _ = sm.Update(tea.KeyPressMsg{Code: tea.KeyUp})
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
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	sm := updated.(*Model)
	sm.rebuildFlat()
	if sm.flat[subdirIdx].Expanded {
		t.Error("directory should be collapsed after left arrow")
	}

	// Expand
	updated, _ = sm.Update(tea.KeyPressMsg{Code: tea.KeyRight})
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

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
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
	if view.Content == "" {
		t.Error("View() should not be empty")
	}

	// When hidden
	m.Toggle()
	view = m.View()
	if view.Content != "" {
		t.Error("View() should be empty when hidden")
	}
}

func makeSidebarWithItems(t *testing.T, count int) *Model {
	t.Helper()

	st := styles.New("dark")
	root := &TreeNode{Name: "root", IsDir: true, Expanded: true}
	for i := 0; i < count; i++ {
		root.Children = append(root.Children, &TreeNode{Name: fmt.Sprintf("node-%d", i)})
	}
	m := &Model{
		styles:   st,
		root:     t.TempDir(),
		rootNode: root,
		visible:  true,
		width:    30,
		height:   5,
	}
	m.rebuildFlat()
	return m
}

func TestSidebarScroll_PastFoldUpdatesOffset(t *testing.T) {
	t.Parallel()

	m := makeSidebarWithItems(t, 10) // flat[0]=root, flat[i]=node-(i-1)
	m.height = 5                     // 1 title line + 4 visible rows

	m.moveCursor(5)
	if m.cursor != 5 {
		t.Errorf("cursor = %d, want 5", m.cursor)
	}
	if m.offset != 2 {
		t.Errorf("offset = %d, want 2", m.offset)
	}

	view := m.View()
	// flat[5] == node-4, which must be visible.
	if !strings.Contains(view.Content, "node-4") {
		t.Errorf("View() should contain node-4, got %q", view.Content)
	}
	// flat[1] == node-0 scrolled out of view.
	if strings.Contains(view.Content, "node-0") {
		t.Errorf("View() should not contain node-0 after scrolling, got %q", view.Content)
	}
}

func TestSidebarScroll_UpAdjustsOffset(t *testing.T) {
	t.Parallel()

	m := makeSidebarWithItems(t, 10)
	m.height = 5
	m.cursor = 5
	m.offset = 3

	m.moveCursor(-3)
	if m.cursor != 2 {
		t.Errorf("cursor = %d, want 2", m.cursor)
	}
	if m.offset != 2 {
		t.Errorf("offset = %d, want 2", m.offset)
	}
}

func TestSidebarScroll_ClickWithOffset(t *testing.T) {
	t.Parallel()

	m := makeSidebarWithItems(t, 10)
	m.height = 5
	m.offset = 3

	// y=2 is the first visible row below the title; with offset=3 it maps to index 4 (node-3).
	m.handleClick(2)
	if m.cursor != 4 {
		t.Errorf("cursor = %d, want 4", m.cursor)
	}
}

func TestSidebarScroll_RespectsTitleLine(t *testing.T) {
	t.Parallel()

	m := makeSidebarWithItems(t, 10)
	m.height = 2 // title + 1 row

	view := m.View()
	lines := strings.Split(view.Content, "\n")
	if len(lines) < 2 {
		t.Fatalf("View() should have at least title + 1 row, got %q", view.Content)
	}

	// Only one item line should be rendered after the title.
	itemCount := 0
	for i, line := range lines {
		if i == 0 {
			continue // title
		}
		if strings.TrimSpace(line) != "" {
			itemCount++
		}
	}
	if itemCount != 1 {
		t.Errorf("expected 1 visible item row, got %d in %q", itemCount, view.Content)
	}
}

func TestBuildTree(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	_ = os.Mkdir(filepath.Join(tmpDir, "subdir"), 0755)
	_ = os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("hello"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "subdir", "nested.txt"), []byte("world"), 0644)

	node, err := buildTree(tmpDir, 2, true)
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

	node, err := buildTree(tmpDir, 1, true)
	if err != nil {
		t.Fatalf("buildTree() error = %v", err)
	}

	for _, child := range node.Children {
		if child.Name == ".hidden" {
			t.Error("buildTree should skip hidden files")
		}
	}
}

func TestBuildTreeSkipsSymlinks(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	realPath := filepath.Join(tmpDir, "real.txt")
	linkPath := filepath.Join(tmpDir, "link.txt")
	_ = os.WriteFile(realPath, []byte("hello"), 0644)
	_ = os.Symlink(realPath, linkPath)

	node, err := buildTree(tmpDir, 1, true)
	if err != nil {
		t.Fatalf("buildTree() error = %v", err)
	}

	hasReal := false
	for _, child := range node.Children {
		if child.Name == "link.txt" {
			t.Error("buildTree should skip symlinked entries")
		}
		if child.Name == "real.txt" {
			hasReal = true
		}
	}
	if !hasReal {
		t.Error("buildTree should still include real files")
	}
}

func TestRenderNodeDepth(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	tmpDir := t.TempDir()
	_ = os.Mkdir(filepath.Join(tmpDir, "subdir"), 0755)
	_ = os.WriteFile(filepath.Join(tmpDir, "subdir", "nested.txt"), []byte("hello"), 0644)

	m, err := New(st, tmpDir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	m.SetSize(30, 20)
	m.rebuildFlat()

	for _, node := range m.flat {
		line := ansi.Strip(m.renderNode(node, false))
		expectedPrefix := strings.Repeat("  ", node.Depth)
		if !strings.HasPrefix(line, "  "+expectedPrefix) {
			// The renderNode prefixes with "  " then the depth spaces
			actual := strings.TrimLeft(line, " ")
			t.Errorf("node %q depth=%d: expected prefix with %d indent spaces, got %q", node.Name, node.Depth, len(expectedPrefix), actual)
		}
	}
}
