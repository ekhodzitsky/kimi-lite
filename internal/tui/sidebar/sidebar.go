// Package sidebar provides a file browser sidebar for the kimi-lite TUI.
package sidebar

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

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
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if !m.visible {
			return m, nil
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
			return m, m.selectCurrent()
		case " ":
			m.toggleCurrent()
		}
	case tea.MouseMsg:
		if !m.visible {
			return m, nil
		}
		if msg.Action == tea.MouseActionRelease && msg.Button == tea.MouseButtonLeft {
			m.handleClick(msg.Y)
		}
	}
	return m, nil
}

// View implements tea.Model.
func (m *Model) View() string {
	if !m.visible {
		return ""
	}

	var b strings.Builder
	b.WriteString(m.styles.SidebarTitle.Render("📁 Files") + "\n")
	if m.flatDirty {
		m.rebuildFlat()
	}
	for i, node := range m.flat {
		line := m.renderNode(node, i == m.cursor)
		b.WriteString(line + "\n")
	}
	content := b.String()
	if len(content) > 0 && content[len(content)-1] == '\n' {
		content = content[:len(content)-1]
	}
	return m.styles.Sidebar.Width(m.width).Height(m.height).Render(content)
}

// SetSize sets the sidebar dimensions.
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
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
	node, err := buildTree(m.root, maxTreeDepth)
	if err != nil {
		return err
	}
	m.rootNode = node
	m.rebuildFlat()
	if len(m.flat) > 0 && m.cursor >= len(m.flat) {
		m.cursor = len(m.flat) - 1
	}
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
	m.flat = append(m.flat, node)
	if node.Expanded {
		for _, child := range node.Children {
			m.traverse(child, depth+1)
		}
	}
}

func (m *Model) renderNode(node *TreeNode, isCursor bool) string {
	prefix := strings.Repeat("  ", m.depth(node))
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
	if maxLen > 0 && len(name) > maxLen {
		name = name[:maxLen] + "..."
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

func (m *Model) depth(node *TreeNode) int {
	if node.Path == m.root {
		return 0
	}
	rel, _ := filepath.Rel(m.root, node.Path)
	if rel == "." {
		return 0
	}
	return strings.Count(rel, string(filepath.Separator))
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
}

func (m *Model) collapseCurrent() {
	if m.cursor < 0 || m.cursor >= len(m.flat) {
		return
	}
	node := m.flat[m.cursor]
	if node.IsDir && node.Expanded {
		node.Expanded = false
		m.flatDirty = true
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
	if idx < 0 || idx >= len(m.flat) {
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
}

func buildTree(path string, maxDepth int) (*TreeNode, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	node := &TreeNode{
		Name:  info.Name(),
		Path:  path,
		IsDir: info.IsDir(),
	}
	if info.IsDir() && maxDepth > 0 {
		entries, err := os.ReadDir(path)
		if err != nil {
			return node, nil // partial tree
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			childPath := filepath.Join(path, entry.Name())
			child, err := buildTree(childPath, maxDepth-1)
			if err != nil {
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
