# Public API Guide

> Scoped rules for `pkg/api`. This package is the contract layer; changes here
> affect every consumer. When instructions conflict, the closest `AGENTS.md`
> wins.
>
> **Version:** 2.0
> **Last updated:** 2026-06-17

## Stability

- Avoid breaking changes to exported types, interfaces, functions, and constants.
- Prefer extending interfaces/types over changing existing signatures.
- New required fields on structs are breaking; add optional fields or use
  functional options.
- Removing or renaming exported identifiers is breaking.
- `TurnManager` was extended with plan-mode and steering methods in v0.5.0.
  This is a breaking change to external implementers; it is documented in
  `CHANGELOG.md` and `docs/adr/2026-06-15-kimi-code-0.15.0-parity.md`.
- `TurnStore` was extended with `NextTurnSeq` in v0.6.0 (unreleased) to support
  monotonic turn sequence numbers across session resume. This is a breaking
  change to external store implementations; it is documented in `CHANGELOG.md`
  and `docs/adr/2026-06-17-kimi-code-0.17.0-parity-decisions.md`.

## Deprecation

- If a public API must change, deprecate the old form first.
- Add a godoc comment starting with `Deprecated:` explaining the replacement.
- Record the deprecation in an ADR and `CHANGELOG.md`.
- Only remove after a reasonable runway (e.g., one minor release).

## Interface Design

- Keep interfaces small and focused (one or two methods is often enough).
- Define interfaces in the consuming package when possible.
- Avoid leaking implementation details in interface names or parameters.
- All exported identifiers must have godoc comments.

## Testing Compatibility

- Add tests that compile against the public API.
- Do not rely on unexported symbols in public-API tests.
- When adding examples, use `Example` tests or doc comments.

## Cross-References

- Root `AGENTS.md` for general conventions.
- `docs/adr/` for design decisions that affect public API.
