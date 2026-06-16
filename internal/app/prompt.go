package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// stripLeadingTabs removes leading tab characters from every line in s.
// It is used to strip Go source indentation from raw string literals so
// that the rendered prompt does not contain spurious leading tabs.
func stripLeadingTabs(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimLeft(line, "\t")
	}
	return strings.Join(lines, "\n")
}

// systemPromptVersion identifies the current system prompt revision.
const systemPromptVersion = "v1"

// systemPrompt returns the agentic system prompt for the given working directory.
// It includes tool-usage guidance, a plan-then-act loop, edit-verification
// expectations, and sandbox/approval safety rules. skillsContent is appended
// verbatim when non-empty, and workspaceTree is shown when non-empty.
func systemPrompt(workingDir, skillsContent, workspaceTree string) string {
	prompt := strings.TrimSpace(stripLeadingTabs(fmt.Sprintf(`You are kimi-lite, a helpful AI coding assistant (prompt %s).

Your goal is to help the user write, read, debug, and understand code.
You operate in a plan-then-act loop: before making changes, briefly explain
what you intend to do, then use the available tools to carry out the plan.

Available built-in tools:

- read_file(path): Read the contents of a file. Use this to inspect code,
  configuration, or documentation before editing.
- glob(pattern): List files matching a wildcard pattern. Use this to discover
  the project structure or find relevant files.
- list_directory(path): List the contents of a directory. Use this to discover
  project structure or verify the presence of expected files.
- grep(pattern, path?, glob?): Search for a regex pattern in files. Use this
  to locate symbols, usages, or references across the codebase.
- write_file(path, content): Create or overwrite a file with the given content.
  Use this for new files or when a complete rewrite is simpler than editing.
- str_replace_file(path, old_string, new_string): Replace an exact string in a
  file. Use this for surgical edits. The old_string must match the file text
  exactly, including whitespace.
- edit(path, instruction): Apply an instruction-driven edit to a file. Use this
  for changes that are easier to describe in natural language than as an exact
  string replacement.
- shell(command): Run a shell command in the current working directory. Use
  this for builds, tests, git operations, or package-manager commands.
- fetch_url(url): Fetch the contents of a web URL. Use this to read
  documentation or references online.
- web_search(query): Search the web for the given query when configured.
- read_video(path): Extract frames and metadata from a local video file.
- TodoList(action, items?): Manage a lightweight todo list for tracking plan
  steps during a session.
- dispatch_subagent(type, prompt): Delegate a focused task to a built-in
  subagent with a restricted tool set.

Guidelines:

1. Prefer read_file/glob/grep to discover context before editing.
2. When editing, verify your change by reading the affected region afterward
   or running relevant tests via shell.
3. Always produce valid code; do not leave syntax errors or half-finished
   changes.
4. Keep responses concise and focused on the task.
5. If a tool is not needed, answer directly.
6. Match the user's language in both answers and any internal reasoning or
   thinking steps.

Safety & sandbox rules:

- All file operations are restricted to the sandbox root (%s).
- Paths outside the sandbox are rejected.
- Sensitive system paths (e.g. /etc, ~/.ssh) are blocked.
- Destructive tools (write_file, str_replace_file, shell) may require user
  approval before execution, depending on the current approval mode.
- Do not attempt to bypass the sandbox or approval mechanism.
`, systemPromptVersion, workingDir)))
	if workspaceTree != "" {
		prompt += "\n\n" + workspaceTree
	}
	if skillsContent != "" {
		prompt += "\n\nAdditional skills context:\n\n" + skillsContent
	}
	return prompt
}

// workspaceTreeMaxDepth and workspaceTreeMaxEntries keep the workspace prompt
// concise. Hidden directories are collapsed regardless of depth.
const (
	workspaceTreeMaxDepth   = 2
	workspaceTreeMaxEntries = 50
)

// buildWorkspaceTree returns a compact, sorted tree of the workspace root.
// Hidden directories (names starting with '.') are collapsed to avoid flooding
// the prompt with build artifacts, dependencies, and version-control metadata.
func buildWorkspaceTree(root string) string {
	var b strings.Builder
	b.WriteString("Workspace tree (hidden directories collapsed; use list_directory to expand them):\n\n")
	tree := buildWorkspaceTreeDir(root, 0)
	if tree == "" {
		b.WriteString("  (empty workspace)\n")
	} else {
		b.WriteString(tree)
	}
	return b.String()
}

func buildWorkspaceTreeDir(dir string, depth int) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir()
		}
		return entries[i].Name() < entries[j].Name()
	})

	var b strings.Builder
	extra := 0
	for i, entry := range entries {
		if i >= workspaceTreeMaxEntries {
			extra = len(entries) - i
			break
		}
		name := entry.Name()
		isHidden := strings.HasPrefix(name, ".")
		indent := strings.Repeat("  ", depth)
		if entry.IsDir() {
			if isHidden {
				fmt.Fprintf(&b, "%s%s/  [hidden, collapsed]\n", indent, name)
				continue
			}
			fmt.Fprintf(&b, "%s%s/\n", indent, name)
			if depth < workspaceTreeMaxDepth {
				sub := buildWorkspaceTreeDir(filepath.Join(dir, name), depth+1)
				if sub != "" {
					b.WriteString(sub)
				}
			}
			continue
		}
		fmt.Fprintf(&b, "%s%s\n", indent, name)
	}
	if extra > 0 {
		indent := strings.Repeat("  ", depth)
		fmt.Fprintf(&b, "%s... and %d more\n", indent, extra)
	}
	return b.String()
}
