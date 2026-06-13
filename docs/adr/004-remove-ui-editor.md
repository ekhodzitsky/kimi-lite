# ADR-004: Remove `ui.editor` configuration field

## Status

Accepted

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
- No ADR is required for future re-introduction, but any new public field must
  follow the same pattern and be wired to real behavior before release.
