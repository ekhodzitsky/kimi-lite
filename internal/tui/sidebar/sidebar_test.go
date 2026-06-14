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

func TestSetRoot(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	tmpDir := t.TempDir()
	m, err := New(st, tmpDir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := m.SetRoot(""); err == nil {
		t.Error("SetRoot(\"\") should return error")
	}

	newDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(newDir, "file.txt"), []byte("x"), 0644)
	if err := m.SetRoot(newDir); err != nil {
		t.Fatalf("SetRoot(newDir) error = %v", err)
	}
	if m.root != newDir {
		t.Errorf("root = %q, want %q", m.root, newDir)
	}

	if err := m.SetRoot(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Error("SetRoot with invalid path should return error")
	}
}

func TestSelectedPath(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	tmpDir := t.TempDir()
	m, err := New(st, tmpDir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if got := m.SelectedPath(); got != "" {
		t.Errorf("SelectedPath() = %q, want empty", got)
	}

	m.selected = "/some/path"
	if got := m.SelectedPath(); got != "/some/path" {
		t.Errorf("SelectedPath() = %q, want /some/path", got)
	}
}

func TestVisiblePaths(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("x"), 0644)
	m, err := New(st, tmpDir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	paths := m.VisiblePaths()
	if len(paths) == 0 {
		t.Fatal("VisiblePaths() should not be empty")
	}
	if paths[0] != tmpDir {
		t.Errorf("VisiblePaths()[0] = %q, want %q", paths[0], tmpDir)
	}

	m.flatDirty = true
	_ = m.VisiblePaths()
	if m.flatDirty {
		t.Error("VisiblePaths() should rebuild flat list")
	}
}

func TestInit(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m, err := New(st, t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if cmd := m.Init(); cmd != nil {
		t.Errorf("Init() = %v, want nil", cmd)
	}
}

func TestToggleCurrent(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	tmpDir := t.TempDir()
	_ = os.Mkdir(filepath.Join(tmpDir, "dir"), 0755)
	_ = os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("x"), 0644)
	m, err := New(st, tmpDir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	m.SetSize(30, 20)
	m.rebuildFlat()

	var dirIdx int
	for i, n := range m.flat {
		if n.Name == "dir" {
			dirIdx = i
			break
		}
	}
	m.cursor = dirIdx

	m.toggleCurrent()
	if m.flat[dirIdx].Expanded {
		t.Error("dir should be collapsed after toggle")
	}

	m.toggleCurrent()
	if !m.flat[dirIdx].Expanded {
		t.Error("dir should be expanded after second toggle")
	}

	// Toggle on a file is a no-op.
	for i, n := range m.flat {
		if n.Name == "file.txt" {
			m.cursor = i
			break
		}
	}
	before := m.flat[m.cursor].Expanded
	m.toggleCurrent()
	if m.flat[m.cursor].Expanded != before {
		t.Error("toggle on file should not change expanded state")
	}

	// Space key triggers toggleCurrent.
	m.cursor = dirIdx
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyExtended, Text: " "})
	sm := updated.(*Model)
	sm.rebuildFlat()
	if sm.flat[dirIdx].Expanded {
		t.Error("dir should be collapsed after space key")
	}
}

func TestUpdateMsgHidden(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("x"), 0644)
	m, err := New(st, tmpDir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	m.Toggle()
	if m.Visible() {
		t.Fatal("sidebar should be hidden")
	}

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if cmd != nil {
		t.Errorf("hidden key press should return nil cmd, got %v", cmd)
	}
	_ = updated

	updated, cmd = m.Update(tea.MouseReleaseMsg{Y: 2})
	if cmd != nil {
		t.Errorf("hidden mouse release should return nil cmd, got %v", cmd)
	}
	_ = updated
}

func TestSelectCurrent(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	tmpDir := t.TempDir()
	_ = os.Mkdir(filepath.Join(tmpDir, "dir"), 0755)
	_ = os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("x"), 0644)
	m, err := New(st, tmpDir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	m.SetSize(30, 20)
	m.rebuildFlat()

	// Invalid cursor.
	m.cursor = -1
	if cmd := m.selectCurrent(); cmd != nil {
		t.Errorf("selectCurrent() with invalid cursor = %v, want nil", cmd)
	}
	m.cursor = 1000
	if cmd := m.selectCurrent(); cmd != nil {
		t.Errorf("selectCurrent() with out of range cursor = %v, want nil", cmd)
	}

	// Directory toggles expanded state.
	var dirIdx int
	for i, n := range m.flat {
		if n.Name == "dir" {
			dirIdx = i
			break
		}
	}
	m.cursor = dirIdx
	if cmd := m.selectCurrent(); cmd != nil {
		t.Errorf("selectCurrent() on dir should return nil, got %v", cmd)
	}
	if m.flat[dirIdx].Expanded {
		t.Error("dir should be collapsed after selectCurrent")
	}

	// File selection returns SelectFileMsg and marks selected.
	var fileIdx int
	for i, n := range m.flat {
		if n.Name == "file.txt" {
			fileIdx = i
			break
		}
	}
	m.cursor = fileIdx
	cmd := m.selectCurrent()
	if cmd == nil {
		t.Fatal("selectCurrent() on file should return cmd")
	}
	msg := cmd()
	if sf, ok := msg.(SelectFileMsg); !ok {
		t.Errorf("expected SelectFileMsg, got %T", msg)
	} else if sf.Path != m.flat[fileIdx].Path {
		t.Errorf("SelectFileMsg.Path = %q, want %q", sf.Path, m.flat[fileIdx].Path)
	}
	if !m.flat[fileIdx].Selected {
		t.Error("file node should be selected")
	}
}

func TestMoveCursorBounds(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m, err := New(st, t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Empty flat list should not panic.
	m.flat = nil
	m.cursor = 0
	m.moveCursor(-1)
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0", m.cursor)
	}

	m = makeSidebarWithItems(t, 5)
	m.cursor = 0
	m.moveCursor(-10)
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0", m.cursor)
	}

	m.moveCursor(100)
	if m.cursor != len(m.flat)-1 {
		t.Errorf("cursor = %d, want %d", m.cursor, len(m.flat)-1)
	}
}

func TestEnsureCursorVisibleBounds(t *testing.T) {
	t.Parallel()

	m := makeSidebarWithItems(t, 5)
	m.height = 3 // title + 2 rows

	// Empty flat list.
	m.flat = nil
	m.offset = 5
	m.ensureCursorVisible()
	if m.offset != 0 {
		t.Errorf("offset = %d, want 0", m.offset)
	}

	m = makeSidebarWithItems(t, 5)
	m.height = 3
	m.cursor = 0
	m.offset = 10
	m.ensureCursorVisible()
	if m.offset >= len(m.flat) {
		t.Errorf("offset = %d should be within flat bounds", m.offset)
	}
}

func TestRefreshInvalidRoot(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	if _, err := New(st, filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("New() with missing root should return error")
	}
}

func TestBuildTreeSymlinkedRoot(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	realDir := filepath.Join(tmpDir, "real")
	linkDir := filepath.Join(tmpDir, "link")
	_ = os.Mkdir(realDir, 0755)
	_ = os.WriteFile(filepath.Join(realDir, "file.txt"), []byte("x"), 0644)
	_ = os.Symlink(realDir, linkDir)

	node, err := buildTree(linkDir, 1, true)
	if err != nil {
		t.Fatalf("buildTree() error = %v", err)
	}
	if !node.IsDir {
		t.Error("symlinked root should resolve to directory")
	}
	if len(node.Children) != 1 {
		t.Errorf("expected 1 child, got %d", len(node.Children))
	}
}

func TestBuildTreeDepthZero(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	_ = os.Mkdir(filepath.Join(tmpDir, "subdir"), 0755)
	_ = os.WriteFile(filepath.Join(tmpDir, "subdir", "file.txt"), []byte("x"), 0644)

	node, err := buildTree(tmpDir, 0, true)
	if err != nil {
		t.Fatalf("buildTree() error = %v", err)
	}
	if !node.IsDir {
		t.Error("root should be a directory")
	}
	if len(node.Children) != 0 {
		t.Errorf("depth 0 should not recurse, got %d children", len(node.Children))
	}
}

func TestBuildTreePartialTree(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	childDir := filepath.Join(tmpDir, "child")
	_ = os.Mkdir(childDir, 0755)
	_ = os.WriteFile(filepath.Join(childDir, "file.txt"), []byte("x"), 0644)

	// Make child unreadable so ReadDir fails; parent should still return a partial tree.
	_ = os.Chmod(childDir, 0000)
	defer os.Chmod(childDir, 0755) //nolint:errcheck // cleanup

	node, err := buildTree(tmpDir, 2, true)
	if err != nil {
		t.Fatalf("buildTree() error = %v", err)
	}
	if len(node.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(node.Children))
	}
	if len(node.Children[0].Children) != 0 {
		t.Errorf("unreadable child should yield no grandchildren, got %d", len(node.Children[0].Children))
	}
}

func TestHandleClick(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("x"), 0644)
	m, err := New(st, tmpDir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	m.SetSize(30, 20)
	m.offset = 0
	m.rebuildFlat()

	// Click on title line is ignored.
	before := m.cursor
	m.handleClick(0)
	if m.cursor != before {
		t.Errorf("click on title should not change cursor, got %d", m.cursor)
	}

	// Click beyond visible rows is ignored.
	m.handleClick(1000)
	if m.cursor != before {
		t.Errorf("click beyond rows should not change cursor, got %d", m.cursor)
	}

	// Click a file selects it.
	fileIdx := -1
	for i, n := range m.flat {
		if n.Name == "file.txt" {
			fileIdx = i
			break
		}
	}
	if fileIdx < 0 {
		t.Fatal("file.txt not found in flat list")
	}
	m.handleClick(fileIdx + 1) // +1 for title line
	if m.cursor != fileIdx {
		t.Errorf("cursor = %d, want %d", m.cursor, fileIdx)
	}
	if m.selected != m.flat[fileIdx].Path {
		t.Errorf("selected = %q, want %q", m.selected, m.flat[fileIdx].Path)
	}
	if !m.flat[fileIdx].Selected {
		t.Error("file node should be selected after click")
	}
}

func TestRenderNodeTruncation(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := &Model{
		styles: st,
		width:  10,
	}
	node := &TreeNode{Name: "very-long-file-name.txt", Depth: 0}
	rendered := ansi.Strip(m.renderNode(node, false))
	if !strings.Contains(rendered, "...") {
		t.Errorf("renderNode should truncate long names, got %q", rendered)
	}
}

func TestRenderNodeSelected(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := &Model{styles: st, width: 30}
	node := &TreeNode{Name: "file.txt", IsDir: true, Expanded: true, Selected: true}
	rendered := ansi.Strip(m.renderNode(node, false))
	if !strings.Contains(rendered, "📂") {
		t.Errorf("renderNode should show open folder icon, got %q", rendered)
	}
	if !strings.Contains(rendered, "file.txt") {
		t.Errorf("renderNode should include name, got %q", rendered)
	}
}

func TestNewGetwdError(t *testing.T) {
	// Cannot run in parallel because we mutate the working directory.
	st := styles.New("dark")
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	deletedDir := t.TempDir()
	if err := os.Chdir(deletedDir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	if err := os.Remove(deletedDir); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	if _, err := New(st, ""); err == nil {
		t.Error("New(\"\") with deleted cwd should return error")
	}
}

func TestUpdateMsgUnknownKey(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("x"), 0644)
	m, err := New(st, tmpDir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	m.SetSize(30, 20)

	before := m.cursor
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyExtended, Text: "a"})
	if cmd != nil {
		t.Errorf("unknown key should return nil cmd, got %v", cmd)
	}
	sm := updated.(*Model)
	if sm.cursor != before {
		t.Errorf("unknown key should not move cursor, got %d", sm.cursor)
	}
}

func TestViewEmptyFlat(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := &Model{
		styles:  st,
		visible: true,
		width:   30,
		height:  5,
		flat:    []*TreeNode{},
	}
	view := m.View()
	if !strings.Contains(view.Content, "📁 Files") {
		t.Errorf("View() should render title, got %q", view.Content)
	}
}

func TestRebuildFlatNilRoot(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := &Model{
		styles:    st,
		rootNode:  nil,
		flat:      []*TreeNode{{Name: "stale"}},
		flatDirty: true,
	}
	m.rebuildFlat()
	if len(m.flat) != 0 {
		t.Errorf("flat = %v, want empty", m.flat)
	}
	if m.flatDirty {
		t.Error("flatDirty should be false after rebuildFlat")
	}
}

func TestRefreshAdjustsCursor(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	tmpDir := t.TempDir()
	m, err := New(st, tmpDir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	m.cursor = 100
	if err := m.refresh(); err != nil {
		t.Fatalf("refresh() error = %v", err)
	}
	if m.cursor != len(m.flat)-1 {
		t.Errorf("cursor = %d, want %d", m.cursor, len(m.flat)-1)
	}
}

func TestEnsureCursorVisibleOffsetBeyondEnd(t *testing.T) {
	t.Parallel()

	m := makeSidebarWithItems(t, 3)
	m.height = 10
	m.cursor = 100
	m.offset = 100
	m.ensureCursorVisible()
	if m.offset != len(m.flat)-1 {
		t.Errorf("offset = %d, want %d", m.offset, len(m.flat)-1)
	}
}

func TestCursorOperationsInvalidCursor(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	tmpDir := t.TempDir()
	_ = os.Mkdir(filepath.Join(tmpDir, "dir"), 0755)
	m, err := New(st, tmpDir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	m.SetSize(30, 20)
	m.rebuildFlat()
	m.cursor = -1

	m.collapseCurrent()
	m.expandCurrent()
	m.toggleCurrent()

	m.cursor = len(m.flat) + 10
	m.collapseCurrent()
	m.expandCurrent()
	m.toggleCurrent()
}

func TestHandleClickDirectory(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	tmpDir := t.TempDir()
	_ = os.Mkdir(filepath.Join(tmpDir, "dir"), 0755)
	_ = os.WriteFile(filepath.Join(tmpDir, "dir", "file.txt"), []byte("x"), 0644)
	m, err := New(st, tmpDir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	m.SetSize(30, 20)
	m.offset = 0
	m.rebuildFlat()

	var dirIdx int
	for i, n := range m.flat {
		if n.Name == "dir" {
			dirIdx = i
			break
		}
	}

	// Directories are expanded by default; first click collapses.
	m.handleClick(dirIdx + 1) // +1 for title
	if m.flat[dirIdx].Expanded {
		t.Error("directory should be collapsed after first click")
	}
	m.handleClick(dirIdx + 1)
	if !m.flat[dirIdx].Expanded {
		t.Error("directory should be expanded after second click")
	}
}

func TestRenderNodeMaxLenNonPositive(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := &Model{styles: st, width: 1}
	node := &TreeNode{Name: "file.txt", Depth: 0}
	rendered := ansi.Strip(m.renderNode(node, false))
	if rendered == "" {
		t.Error("renderNode should still render something with tiny width")
	}
}

func TestBuildTreeChildLstatError(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	childDir := filepath.Join(tmpDir, "child")
	_ = os.Mkdir(childDir, 0755)
	_ = os.WriteFile(filepath.Join(childDir, "file.txt"), []byte("x"), 0644)

	_ = os.Chmod(childDir, 0000)
	defer os.Chmod(childDir, 0755) //nolint:errcheck // cleanup

	node, err := buildTree(tmpDir, 2, true)
	if err != nil {
		t.Fatalf("buildTree() error = %v", err)
	}
	if len(node.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(node.Children))
	}
	if len(node.Children[0].Children) != 0 {
		t.Errorf("expected 0 grandchildren, got %d", len(node.Children[0].Children))
	}
}

func TestUpdateMsgMouseClickVisible(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("x"), 0644)
	m, err := New(st, tmpDir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	m.SetSize(30, 20)
	m.offset = 0

	updated, cmd := m.Update(tea.MouseReleaseMsg{Y: 2})
	if cmd != nil {
		t.Errorf("mouse click should return nil cmd, got %v", cmd)
	}
	sm := updated.(*Model)
	if sm.selected == "" {
		t.Error("mouse click should select file")
	}
}

func TestViewFlatDirty(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("x"), 0644)
	m, err := New(st, tmpDir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	m.SetSize(30, 20)
	m.flatDirty = true

	view := m.View()
	if !strings.Contains(view.Content, "file.txt") {
		t.Errorf("View() should render file after rebuilding dirty flat list, got %q", view.Content)
	}
	if m.flatDirty {
		t.Error("flatDirty should be false after View")
	}
}

func TestMoveCursorEmptyFlat(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := &Model{
		styles:   st,
		rootNode: nil,
		flat:     nil,
	}
	m.moveCursor(-1)
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0", m.cursor)
	}
}

func TestRenderNodeCollapsedDir(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := &Model{styles: st, width: 30}
	node := &TreeNode{Name: "dir", IsDir: true, Expanded: false}
	rendered := ansi.Strip(m.renderNode(node, false))
	if !strings.Contains(rendered, "📁") {
		t.Errorf("renderNode should show closed folder icon, got %q", rendered)
	}
}

func TestBuildTreeSymlinkedRootError(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	linkDir := filepath.Join(tmpDir, "link")
	_ = os.Symlink(filepath.Join(tmpDir, "missing"), linkDir)

	if _, err := buildTree(linkDir, 1, true); err == nil {
		t.Error("buildTree() with broken symlinked root should return error")
	}
}

func TestBuildTreeRootReadDirError(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("x"), 0644)
	_ = os.Chmod(tmpDir, 0000)
	defer os.Chmod(tmpDir, 0755) //nolint:errcheck // cleanup

	if _, err := buildTree(tmpDir, 1, true); err == nil {
		t.Error("buildTree() with unreadable root should return error")
	}
}

func TestBuildTreeNonRootSymlink(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	realPath := filepath.Join(tmpDir, "real.txt")
	linkPath := filepath.Join(tmpDir, "link.txt")
	_ = os.WriteFile(realPath, []byte("hello"), 0644)
	_ = os.Symlink(realPath, linkPath)

	// Calling buildTree directly on a non-root symlink returns nil.
	node, err := buildTree(linkPath, 1, false)
	if err != nil {
		t.Fatalf("buildTree() error = %v", err)
	}
	if node != nil {
		t.Errorf("expected nil node for non-root symlink, got %+v", node)
	}
}
