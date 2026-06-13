# ADR-005: Human-readable branch and commit conventions

## Status

Accepted

## Context

The project previously used machine-prefixed branch names (`feature/P12`, `feature/R08`, `integration/wave-1`) and task-ID commit subjects. While this made backlog tracking easy for automation, it made the repository history look generated and hard to read for human contributors and reviewers. It also leaked the internal audit task identifiers into the public Git history.

We want `kimi-lite` to look and feel like a human-maintained open-source project, even when we use automation to accelerate development.

## Decision

1. **Branch names are descriptive and ID-free.** Use kebab-case names that describe the change:
   - `fix-shell-working-directory`
   - `add-non-interactive-prompt-mode`
   - `improve-read-file-pagination`
   - `add-web-search-tool`
   - `refactor-approval-state-machine`
   - `update-readme-installation`
   Avoid numeric prefixes, wave names, and task codes.

2. **Commit messages follow Conventional Commits and read like a human wrote them.**
   - Use types: `feat`, `fix`, `refactor`, `perf`, `test`, `docs`, `chore`.
   - Keep the subject line under 72 characters and free of task IDs.
   - Explain the *why* and *what* in the body when the change is non-trivial.
   - Example:
     ```
     feat: add non-interactive prompt mode

     Add `-p/--prompt` flag that runs a single user message through the
     agent loop and prints the final response. This enables scripting and
     CI use cases without launching the TUI.
     ```

3. **Pull requests and merge messages follow the same rule.** Titles and descriptions are written for humans, not for task trackers.

4. **Internal task identifiers stay in the issue tracker / project board.** They must not appear in branch names, commit subjects, or PR titles.

## Consequences

- Repository history is readable and looks like a typical high-quality open-source project.
- Backlog automation must map tasks to branches via metadata outside of Git (e.g., issue links in PR descriptions).
- Existing historical commits with task IDs remain in the object database but are no longer visible on any branch after cleanup.
