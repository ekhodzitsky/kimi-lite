# TUI Development Guide

> Scoped rules for `internal/tui`. See root `AGENTS.md` for build/test commands
> and general Go conventions.
>
> **Version:** 2.0
> **Last updated:** 2026-06-17

## Architecture

The TUI uses [Bubble Tea](https://github.com/charmbracelet/bubbletea):
Model-Update-View with a root model that orchestrates child components.

### Key Components

- `Model` in `model.go` — root Bubble Tea model (split across `model_send.go`,
  `model_key.go`, `model_layout.go`, `model_overlay.go`, `model_approval.go`).
- `input` — multi-line textarea with history; `ctrl+g` opens the buffer in the
  external editor; `shift+tab` toggles plan mode; `ctrl+v`/`alt+v` pastes
  clipboard images or file paths as attachments.
- `viewport` — scrollable output area.
- `messages` — message rendering (Markdown via Glamour).
- `activity` — transient activity panel showing pending tools and live output.
- `footer` — two-line status bar with model, cwd, git, context usage, and tips.
- `help` — scrollable `/help` overlay with shortcuts and slash commands.
- `welcome` — empty-state welcome box.
- `sessions` — modal session picker with search, pagination, and current/all
  directory toggle.
- `mentions` — file-path candidate provider for `@`-mention completion.
- `styles` — Lipgloss themes and custom JSON theme loader.

## Rules

- **No IO in `Update`**. All side effects (file reads, shell commands, LLM
  calls, MCP calls) happen inside `tea.Cmd` functions.
- **Root model owns orchestration**. Route messages in `Model.Update()`; child
  components handle their own focused state.
- **Messages only through `tea.Msg`**. Components signal events via custom
  message types.
- **State changes belong in `Update`**, not inside commands.
- **Overlays consume keyboard input**. When `help`, `steer`, `plan`, or
  `approval fullscreen` overlays are open, keys must not leak to child
  components.
- Prefer methods on the model for `tea.Cmd` factories rather than inline
  closures.
- Use `tea.Batch()` when returning multiple independent commands.
- Keep components as separate Bubble Tea models with `Init()`, `Update()`, and
  `View()`.

## Adding a New TUI Component

1. Create a package under `internal/tui/<component>/`.
2. Define a `Model` struct with `Init()`, `Update(tea.Msg) (tea.Model, tea.Cmd)`,
   and `View() string`.
3. Define custom `tea.Msg` types for component events.
4. Add the component to the root `Model` in `internal/tui/model.go`.
5. Wire message handling in root `Update()`.
6. Add tests for `Update`/`View` logic; use golden files where appropriate.

## Styling

- Keep all style definitions in `internal/tui/styles`.
- Use semantic style names rather than hardcoding colors.
- Status messages truncate on narrow terminals.

## Testing

- Add `*_test.go` files alongside the component code.
- Test `Update` state transitions and `View` output.
- Golden/snapshot tests are acceptable for rendered output; update them with
  `go test ./... -update` if the UI changes intentionally.
