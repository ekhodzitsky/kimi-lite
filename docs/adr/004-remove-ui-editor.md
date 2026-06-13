# ADR-004: Remove `ui.editor` configuration field

## Status

Accepted, amended

## Context

The `ui.editor` setting was exposed in `pkg/api.UIConfig` and documented in the
configuration schema, but no production code consumed it. No key binding or
command launched the configured editor, and no TUI component read
`cfg.UI.Editor`. Keeping the field was misleading for users and added an unused
public API surface that would have to be maintained.

## Decision

Remove the `Editor` field from `pkg/api.UIConfig` and all references in the
configuration loading path and tests. The feature is out of scope for the
current UX; if external-editor integration is needed later, it will be designed
as a new top-level workflow rather than retrofitting a dead config field.

## Consequences

- `pkg/api.UIConfig` no longer carries `Editor`.
- Existing TOML files with `ui.editor` will be silently ignored.

## Amendment: external-editor workflow

External editor integration was implemented as a real workflow. The `Editor`
field was reintroduced to `pkg/api.UIConfig`, and `keybindings.external_editor`
(default `ctrl+g`) was added. Pressing the binding in the input component
writes the current buffer to a temp file, runs the editor via
`tea.ExecProcess`, and reads the saved content back into the textarea on return.
The temp file is cleaned up afterward. Resolution order is:

1. `ui.editor` if non-empty.
2. `$VISUAL` if set.
3. `$EDITOR` if set.
4. `vi` as a final fallback.

## Consequences

- `pkg/api.UIConfig` carries `Editor` again and is wired to behavior.
- `pkg/api.KeybindingConfig` carries `ExternalEditor` (default `ctrl+g`).
- Existing TOML files with `ui.editor` now launch the configured editor.
- Any new public field must still be wired to real behavior before release.
