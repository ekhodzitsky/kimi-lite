// Package sidebar provides a file browser sidebar for the kimi-lite TUI.
package sidebar

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/ekhodzitsky/kimi-lite/internal/tui/styles"
)

const (
	defaultSidebarWidth = 30
	maxTreeDepth        = 2
	sidebarNamePadding  = 4
	sidebarNameEllipsis = 7
)

// TreeNode represents a node in the directory tree.
type TreeNode struct {
	Name     string
	Path     string
	IsDir    bool
	Expanded bool
	Children []*TreeNode
	Selected bool
	Depth    int
}

// ToggleMsg is sent when the sidebar visibility is toggled.
type ToggleMsg struct{}

// SelectFileMsg is sent when a file is selected.
type SelectFileMsg struct {
	Path string
}

// Model is the sidebar component model.
type Model struct {
	styles    *styles.Styles
	root      string
	rootNode  *TreeNode
	selected  string
	visible   bool
	width     int
	height    int
	cursor    int // linear cursor index for keyboard navigation
	offset    int // first visible row in the flat list
	flat      []*TreeNode
	flatDirty bool
}

// SetRoot changes the root directory and refreshes the tree.
func (m *Model) SetRoot(root string) error {
	if root == "" {
		return fmt.Errorf("root cannot be empty")
	}
	m.root = root
	return m.refresh()
}

// SelectedPath returns the path of the currently selected file.
func (m *Model) SelectedPath() string {
	return m.selected
}

// VisiblePaths returns the paths of all nodes currently visible in the flat
// tree, including collapsed directories.
func (m *Model) VisiblePaths() []string {
	if m.flatDirty {
		m.rebuildFlat()
	}
	paths := make([]string, len(m.flat))
	for i, node := range m.flat {
		paths[i] = node.Path
	}
	return paths
}

// New creates a new sidebar model for the given root directory.
func New(st *styles.Styles, root string) (*Model, error) {
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("get working directory: %w", err)
		}
	}

	m := &Model{
		styles:  st,
		root:    root,
		visible: true,
		width:   defaultSidebarWidth,
	}
	if err := m.refresh(); err != nil {
		return nil, err
	}
	return m, nil
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.UpdateMsg(msg)
	return m, cmd
}

// UpdateMsg processes a message and returns the resulting command.
func (m *Model) UpdateMsg(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if !m.visible {
			return nil
		}
		switch msg.String() {
		case "up":
			m.moveCursor(-1)
		case "down":
			m.moveCursor(1)
		case "left":
			m.collapseCurrent()
		case "right":
			m.expandCurrent()
		case "enter":
			return m.selectCurrent()
		case " ":
			m.toggleCurrent()
		}
	case tea.MouseReleaseMsg:
		if !m.visible {
			return nil
		}
		m.handleClick(msg.Y)
	}
	return nil
}

// View implements tea.Model.
func (m *Model) View() tea.View {
	if !m.visible {
		return tea.NewView("")
	}

	var b strings.Builder
	b.WriteString(m.styles.SidebarTitle.Render("📁 Files") + "\n")
	if m.flatDirty {
		m.rebuildFlat()
	}
	m.ensureCursorVisible()

	end := m.offset + m.visibleRows()
	if end > len(m.flat) {
		end = len(m.flat)
	}
	for i := m.offset; i < end; i++ {
		node := m.flat[i]
		line := m.renderNode(node, i == m.cursor)
		b.WriteString(line + "\n")
	}
	content := b.String()
	if len(content) > 0 && content[len(content)-1] == '\n' {
		content = content[:len(content)-1]
	}
	return tea.NewView(m.styles.Sidebar.Width(m.width).Height(m.height).Render(content))
}

// SetSize sets the sidebar dimensions.
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// visibleRows returns the number of flat rows that fit in the current height.
// One row is reserved for the title line.
func (m *Model) visibleRows() int {
	rows := m.height - 1
	if rows < 0 {
		return 0
	}
	return rows
}

// ensureCursorVisible adjusts the scroll offset so that the cursor row is
// always inside the visible viewport.
func (m *Model) ensureCursorVisible() {
	if len(m.flat) == 0 {
		m.offset = 0
		return
	}

	rows := m.visibleRows()

	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+rows {
		m.offset = m.cursor - rows + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
	if m.offset >= len(m.flat) {
		m.offset = len(m.flat) - 1
	}
}

// Toggle toggles sidebar visibility.
func (m *Model) Toggle() {
	m.visible = !m.visible
}

// Visible returns whether the sidebar is visible.
func (m *Model) Visible() bool {
	return m.visible
}

// Width returns the sidebar width.
func (m *Model) Width() int {
	if m.visible {
		return m.width
	}
	return 0
}

func (m *Model) refresh() error {
	node, err := buildTree(m.root, maxTreeDepth, true)
	if err != nil {
		return err
	}
	m.rootNode = node
	m.rebuildFlat()
	if len(m.flat) > 0 && m.cursor >= len(m.flat) {
		m.cursor = len(m.flat) - 1
	}
	m.ensureCursorVisible()
	return nil
}

func (m *Model) rebuildFlat() {
	m.flat = nil
	if m.rootNode == nil {
		m.flatDirty = false
		return
	}
	m.traverse(m.rootNode, 0)
	m.flatDirty = false
}

func (m *Model) traverse(node *TreeNode, depth int) {
	node.Depth = depth
	m.flat = append(m.flat, node)
	if node.Expanded {
		for _, child := range node.Children {
			m.traverse(child, depth+1)
		}
	}
}

func (m *Model) renderNode(node *TreeNode, isCursor bool) string {
	prefix := strings.Repeat("  ", node.Depth)
	icon := "📄"
	if node.IsDir {
		if node.Expanded {
			icon = "📂"
		} else {
			icon = "📁"
		}
	}
	name := node.Name
	maxLen := m.width - len(prefix) - sidebarNameEllipsis
	runes := []rune(name)
	if maxLen > 0 && len(runes) > maxLen {
		name = string(runes[:maxLen]) + "..."
	}
	line := prefix + icon + " " + name
	if isCursor {
		return m.styles.SidebarSelected.Render("> " + line)
	}
	if node.Selected {
		return m.styles.SidebarSelected.Render("  " + line)
	}
	return m.styles.SidebarItem.Render("  " + line)
}

func (m *Model) moveCursor(delta int) {
	m.rebuildFlat()
	if len(m.flat) == 0 {
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.flat) {
		m.cursor = len(m.flat) - 1
	}
	m.ensureCursorVisible()
}

func (m *Model) collapseCurrent() {
	if m.cursor < 0 || m.cursor >= len(m.flat) {
		return
	}
	node := m.flat[m.cursor]
	if node.IsDir && node.Expanded {
		node.Expanded = false
		m.flatDirty = true
		m.ensureCursorVisible()
	}
}

func (m *Model) expandCurrent() {
	if m.cursor < 0 || m.cursor >= len(m.flat) {
		return
	}
	node := m.flat[m.cursor]
	if node.IsDir && !node.Expanded {
		node.Expanded = true
		m.flatDirty = true
		m.ensureCursorVisible()
	}
}

func (m *Model) toggleCurrent() {
	if m.cursor < 0 || m.cursor >= len(m.flat) {
		return
	}
	node := m.flat[m.cursor]
	if node.IsDir {
		node.Expanded = !node.Expanded
		m.flatDirty = true
		m.ensureCursorVisible()
	}
}

func (m *Model) selectCurrent() tea.Cmd {
	if m.cursor < 0 || m.cursor >= len(m.flat) {
		return nil
	}
	node := m.flat[m.cursor]
	if node.IsDir {
		node.Expanded = !node.Expanded
		m.flatDirty = true
		m.ensureCursorVisible()
		return nil
	}
	m.selected = node.Path
	for _, n := range m.flat {
		n.Selected = false
	}
	node.Selected = true
	return func() tea.Msg {
		return SelectFileMsg{Path: node.Path}
	}
}

func (m *Model) handleClick(y int) {
	m.rebuildFlat()
	idx := y - 1 // subtract title line
	if idx < 0 {
		return
	}
	idx += m.offset
	if idx >= len(m.flat) {
		return
	}
	m.cursor = idx
	node := m.flat[idx]
	if node.IsDir {
		node.Expanded = !node.Expanded
		m.flatDirty = true
	} else {
		m.selected = node.Path
		for _, n := range m.flat {
			n.Selected = false
		}
		node.Selected = true
	}
	m.ensureCursorVisible()
}

func buildTree(path string, maxDepth int, isRoot bool) (*TreeNode, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("stat %q: %w", path, err)
	}

	// Follow a symlinked root so the sidebar still works, but skip symlinked
	// child entries to prevent directory traversal outside the project tree.
	if info.Mode()&os.ModeSymlink != 0 {
		if !isRoot {
			return nil, nil
		}
		info, err = os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("stat root symlink %q: %w", path, err)
		}
	}

	node := &TreeNode{
		Name:  info.Name(),
		Path:  path,
		IsDir: info.IsDir(),
	}
	if info.IsDir() && maxDepth > 0 {
		entries, err := os.ReadDir(path)
		if err != nil {
			if isRoot {
				return nil, fmt.Errorf("read directory %q: %w", path, err)
			}
			return node, nil // partial tree
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			entryInfo, err := entry.Info()
			if err != nil {
				continue
			}
			if entryInfo.Mode()&os.ModeSymlink != 0 {
				continue
			}
			childPath := filepath.Join(path, entry.Name())
			child, err := buildTree(childPath, maxDepth-1, false)
			if err != nil || child == nil {
				continue
			}
			node.Children = append(node.Children, child)
		}
		sort.Slice(node.Children, func(i, j int) bool {
			if node.Children[i].IsDir != node.Children[j].IsDir {
				return node.Children[i].IsDir
			}
			return node.Children[i].Name < node.Children[j].Name
		})
		node.Expanded = true
	}
	return node, nil
}
