# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Registered `kimi-k2.7-code` and `kimi-for-coding` in the LLM model registry
  with a 256K context window.
- Dev-time Graphify integration: Makefile targets, `docs/dev/graphify.md`,
  `AGENTS.md` guidance, `.graphifyignore`, and a CI workflow that builds the
  repo knowledge graph as an artifact.
- TUI `/help` overlay now reflects live keybindings and adds context-sensitive
  shortcut sections for approval, plan, and steer overlays.
- `?` key toggles the help overlay.
- `/resume` alias for `/sessions`.
- `clipboard.WriteText` helper and `y` key in the sessions picker to copy the
  resume command to the system clipboard.
- Footer now renders a `PLAN` badge, a `MANUAL` approval-mode badge, and the
  active tool count.
- Welcome panel now displays the real build version via
  `debug.ReadBuildInfo()`.
- Activity panel shows dedicated status lines for `TurnWaitingApproval` and
  `TurnWaitingPlan`.
- TUI message queueing: while the assistant is streaming or thinking, pressing
  `Enter` queues the draft and shows a "Queued: N" indicator in the input
  placeholder, footer, and activity panel. Queued messages auto-send in FIFO
  order when the turn completes. `Ctrl+C` first cancels the active stream and
  preserves the queue; a second `Ctrl+C` clears the draft.

### Changed

- `README.md` now documents how to configure Kimi K2.7 Code and the Kimi Code
  subscription endpoint (`https://api.kimi.com/coding/v1`).
- TUI approval, plan, and steer overlays updated per audit: plan panel scrolls
  and uses Enter/Esc; approval dialog shows batch progress, suppresses
  "Allow for this session" for unsafe tools, renders fullscreen diffs
  edge-to-edge, and caches diffs asynchronously; steer overlay now has a
  visible cursor and line-editing keys.
- TUI input fixes: `Shift+Tab` plan-mode toggle no longer double-toggles;
  default newline bindings now include `Shift+Enter` and `Ctrl+J`;
  slash-command selection auto-submits argument-free commands;
  completion popups support `PageUp`/`PageDown` scrolling; `@`-mention
  insertion appends a trailing space.
- TUI message and viewport rendering: assistant markdown now respects panel
  width; image `ContentParts` render as placeholders; viewport scroll indicator
  is rendered on a dedicated status line; raw mode wraps long lines; text uses
  soft word wrapping; streaming assistant messages append a cursor indicator;
  tool-call arguments are pretty-printed via `encoding/json`.
- Sessions picker: cross-directory resume hint now uses `--session` instead of
  the non-existent `--resume`; picker width is enforced and footer text is
  truncated to fit; display-width-aware truncation handles CJK runes; theme
  integration uses semantic styles from `internal/tui/styles`.
- Footer `YOLO` badge now uses the warning color instead of error red.
- Footer context percentage is clamped to `100%+` with an overflow indicator.
- Welcome panel truncates long directory/session/model values.
- Activity panel respects panel width and truncates long tool-output lines.
- Theme loader: custom theme paths are confined to `configDir/themes/` to
  prevent directory traversal; partial custom themes load with dark-theme
  defaults for missing colors; user-message foreground/border are now
  theme-driven instead of hardcoded.
- Glamour markdown style falls back to `"dark"` for unknown custom theme names.

### Fixed

- Plain-text clipboard pastes were silently dropped when they produced a
  `ContentPartText` with an empty attachment path.
- `Tab`/`Shift+Tab` could not navigate completion popups in the integrated TUI.
- Configurable keybindings are now consistently applied across handlers and the
  help overlay via `effectiveKeybindings()` defaults.
- Duplicate `enter` entries in the help overlay are now disambiguated by focus.

## [0.6.0] - 2026-06-17

### Added

- `api.Turn` now carries a monotonic `Seq` number per session.
- `api.TurnStore` gains `NextTurnSeq` so resumed sessions continue turn
  numbering from the highest persisted sequence instead of restarting.

### Changed

- **Breaking:** `api.TurnStore` is an exported interface; the new
  `NextTurnSeq` method breaks existing third-party implementations.
- `internal/store/sqlite.go` persists and restores turn sequence numbers.
- `internal/core/turn.go` assigns the next sequence number at the start of
  each turn.

### Not Ported

- Kimi Code 0.17.0's server-hosted web UI (`kimi server`, `kimi web`),
  OAuth token refresh error handling, debug TPS skipping, and web-login
  fixes are intentionally out of scope for `kimi-lite`. See
  `docs/adr/2026-06-17-kimi-code-0.17.0-parity-decisions.md`.

## [0.5.0] - 2026-06-17

### Added

- Image and file paste support in the TUI. Press `Ctrl+V` or `Alt+V` to paste
  clipboard images or file paths; pasted attachments are copied to
  `<config-dir>/tmp` and attached as multimodal content parts on the outgoing
  user message. The default keybinding is configurable via `keybindings.paste`.
- `api.ContentPart` now supports inline `image_data` in addition to `image_url`.
- `api.TurnManager` gains `RunTurnWithContentParts`,
  `RunTurnWithPlan`, `RunTurnWithPlanWithContentParts`, `ResumeWithPlan`, and
  `Steer` to support plan mode and mid-stream steering.
- Mid-stream steering: press `Ctrl+S` during streaming to inject a follow-up
  instruction.
- Plan mode: press `Shift+Tab` to have the assistant generate a plan for
  approval before executing tool calls.

### Changed

- **Breaking:** `api.TurnManager` is an exported interface; the new methods
  listed above break existing third-party implementations. Projects implementing
  `TurnManager` will need to add these methods.
- `internal/llm/client.go` now validates local attachment paths against a
  configured set of roots and refuses to read files outside those roots or
  above a 10 MB size cap.
- `Model.Update` no longer performs blocking I/O; `RunTurn*` calls are
  dispatched inside `tea.Cmd` functions.

### Fixed

- Non-image file attachments (e.g., `.txt`, `.go`) are no longer silently
  dropped when sending a message.
- Modal overlays (`help`, `steer`, `approval diff`) now consume keyboard input
  instead of leaking it to the input/viewport components.
- Plan-mode turns that unexpectedly produce tool calls before plan approval are
  rejected instead of executing tools first.
- Mention candidate file paths are loaded asynchronously and no longer block
  the UI on every `@` keystroke.
- The default config now includes `keybindings.paste`.

## [0.4.2] - 2026-06-16

### Changed

- Audited and updated `AGENTS.md` to match the current codebase: added `internal/observability`, corrected tool count, listed `dispatch_subagent`, `RiskEvaluator`, `TurnEventStatus`, MCP schema normalization, workspace prompt, and status-message behavior.

## [0.4.1] - 2026-06-16

### Changed

- Refreshed `README.md`: fixed stale "Python" claim, listed SSE MCP, all-sessions picker, multi-modal messages, language-aware UI, and workspace prompt.

## [0.4.0] - 2026-06-16

### Added

- `mcp.NewClientFromServerConfig` now supports `transport = "sse"`, so direct SSE MCP servers work with `kimi-lite doctor` and the ACP/MCP subcommands.
- Same-language status sentence displayed in the TUI before non-trivial tool calls (`shell`, `write_file`, `edit`, etc.).
- MCP JSON Schema normalization layer adapts standard MCP `inputSchema` to Moonshot's stricter schema subset (fills missing types, collapses `["string","null"]` arrays, removes parent `type` with `anyOf`/`oneOf`).
- Workspace tree added to the system prompt; hidden directories are collapsed with a hint to use `list_directory` to expand them.
- Compaction now emits the generated summary into the TUI transcript so the user sees what was preserved.
- Media type detection from file headers in `read_video`, with extension-based fallback.
- Skill directory is included in the loaded-skill context block so skills can reference adjacent files.
- Cancel key (`ctrl+c`) first clears the draft input text while a stream is running; a second press cancels the stream.
- System prompt now instructs the model to keep reasoning/thinking in the user's language.
- Status bar truncates long status text to stay within narrow terminal widths.

### Fixed

- All-sessions picker shows a hint to press `a` when the current directory has no sessions.

## [0.3.0] - 2026-06-16

### Added

- `turn_interrupt` hook event that fires when the user cancels a running turn.
- Legacy SSE MCP transport (`transport = "sse"`) for servers that expose JSON-RPC over Server-Sent Events.
- Session recovery for interrupted tool calls: resuming a session now synthesizes missing tool-result messages so dangling assistant tool calls do not break the next turn.
- Multi-modal message support for image tool outputs. `api.Message` now carries `ContentParts`, the OpenAI request builder emits `image_url` parts, and the SQLite store persists them via a new migration.
- All-sessions picker (`/sessions`) with search, pagination, and a current-directory/all-directories toggle. Selecting a session resumes it and loads its transcript.

## [0.2.10] - 2026-06-14

### Fixed

- **Comprehensive audit fixes** — addressed resource leaks, concurrency bugs, and security issues across the codebase:
  - App/ACP: `App.Close()` now closes MCP/executor/store even on turn shutdown timeout; removed double-close in ACP mode; bounded ACP frame reads to prevent OOM; pprof binds to loopback only; subagent tool allowlist is enforced at execution time.
  - Config: loader defaults match `DefaultConfig()`; env vars resolve correctly when unset; map key casing is preserved for provider/MCP env and headers; MCP timeouts and turn/tool-round bounds are validated; default config is written atomically.
  - Store: `GetTurns` reports parse errors; migrations reject gaps/duplicates; in-memory DBs are isolated; `ClearMessages` updates session timestamp; `UpdateSession`/`DeleteSession` report missing rows; DB file is created with `0600` atomically.
  - LLM: fixed `ChatStream` goroutine/connection leaks and `FallbackClient` timer leaks; `context.DeadlineExceeded` is no longer retried; `ModelAlias.Provider` is honored.
  - Core: turn context is cancelled on completion; tool execution recovers from panics; `Fork` is atomic; `RiskEvaluator` aligns with `ValidateFilePath`; context compressor reports accurate summarized counts and avoids floor overflow.
  - Tools: `read_video` uses sandboxed open + temp copy; ReDoS pattern limits and cancellable grep; shell timeout clamped before duration conversion; process-group cleanup shared across core and hooks.
  - MCP/Git: `StdioTransport.Close` cannot hang on blocked writers; prefixed-name collisions are disambiguated; git timeout is configurable; `Commit` no longer stages unrelated files; git environment is sanitized.
  - Netutil/TUI: expanded SSRF blocklist (IPv4-compatible IPv6, multicast, documentation ranges); TUI message cache is written under lock; sidebar mouse click works; external editor handles paths with spaces.
  - Tests: portable JSON in integration test; `goleak` coverage; `IsReadOnly` taken from executor.

## [0.2.9] - 2026-06-13

### Added

- **MCP read-only auto-approval regression test** — added end-to-end coverage verifying that read-only MCP tools listed in `behavior.auto_approve` are validated against `ToolExecutor.IsReadOnly` and auto-approved, while non-read-only MCP tools are dropped.

## [0.2.8] - 2026-06-13

### Added

- **`list_directory` built-in tool** — read-only directory listing is now available to the model and auto-approved by default.
- **Approval diff preview** — pressing `d` in the approval dialog shows an in-memory unified diff for pending `write_file`/`str_replace_file` calls before deciding.
- **Fuzz targets** — added `FuzzReadChunk` (LLM SSE parsing), `FuzzIsBlockedHost`, and `FuzzValidatePath` (core sandbox escape).
- **End-to-end integration test** — `tests/integration` now exercises a full user-input → LLM → tool-call → tool-result → final-response cycle with real SQLite, executor, and httptest LLM.
- **CI fuzz smoke step** — GitHub Actions now runs all fuzz targets for 10 seconds each on Ubuntu.

### Changed

- **SQLite hardening** — DSN is now path-escaped, in-memory DBs use a named shared DSN, and all connection-scoped PRAGMAs (`foreign_keys`, `journal_mode`, `busy_timeout`) are applied via the driver `_pragma` DSN key so every connection is consistently configured.
- **File-tool hardening** — file operations use `os.Root` to close the `validatePath` TOCTOU/hardlink/symlink-escape hole.
- **Compaction** — leading system/identity prompts are preserved across `/compact`, the keepRecent boundary is pair-aware (won't split assistant tool_calls from their tool results), and tool activity is included in summaries.
- **Input history** — history is now bounded by `session.max_history` and de-duplicates consecutive identical entries.
- **External editor** — `ui.editor` is wired through `tea.ExecProcess`; falls back to `$EDITOR` and then a default editor.
- **Status bar** — token/context usage is estimated and displayed live when `ui.show_token_count` is enabled.
- **Sidebar** — vertical scrolling keeps the cursor/selection visible in large file trees.

### Removed

- Dead advertised surfaces: `/goal`, `/btw`, and plan mode (Shift+Tab) were no-ops and have been removed from code, config, and docs.

## [0.2.7] - 2026-06-13

### Changed

- **Full Charm v2 migration** — migrated the entire TUI from `github.com/charmbracelet/*` v1 to `charm.land/*/v2` for `bubbletea`, `bubbles`, `lipgloss`, and `glamour`. This removes the mixed v1/v2 dependency graph and aligns the project with the latest Charm ecosystem.

## [0.2.6] - 2026-06-13

### Changed

- **Lipgloss v2 overlay compositor** — replaced the hand-rolled ANSI string splicing in the approval-dialog overlay with `charm.land/lipgloss/v2`'s `Canvas`/`Compositor`. This closes the R08 parity-gap item and makes wide-rune/CJK dialog positioning more reliable.

## [0.2.5] - 2026-06-13

### Added

- **Raw markdown toggle** — press `r` while the viewport is focused to toggle raw/rendered markdown for assistant messages.
- **MCP read-only auto-approval** — read-only MCP tools (as annotated by the server) are now eligible for auto-approval in `auto` mode.
- **Tool-aware compaction** — `/compact` now includes tool-call names/arguments and tool-result content in the summary so tool history is not flattened.

### Changed

- **Approval dialog** — file-edit approvals show an in-memory unified diff preview before yes/no/always.

## [0.2.4] - 2026-06-13

### Changed

- **Release action versions** — bumped `anchore/sbom-action/download-syft` to v0.24.0 and `sigstore/cosign-installer` to v4.1.2 in the release workflow.

## [0.2.3] - 2026-06-13

### Fixed

- **Cosign signing** — switched `.goreleaser.yml` to the cosign bundle format (`--bundle`) so keyless artifact signing works with cosign v2.

## [0.2.2] - 2026-06-13

### Changed

- **CI/CD modernization** — bumped all GitHub Actions to Node 24 compatible versions (`actions/checkout` v6, `actions/setup-go` v6, `actions/upload-artifact` v7, `codecov/codecov-action` v7).
- **Automated releases** — added `.github/workflows/release.yml` that runs GoReleaser on every `v*` tag, generates SBOMs with `syft`, and signs artifacts with keyless `cosign`.
- **GoReleaser signing** — switched `.goreleaser.yml` to keyless cosign signing (`--yes`) and removed the Homebrew tap block until a dedicated tap repository token is configured.

## [0.2.1] - 2026-06-13

### Fixed

- **Process group cleanup** — `shell` and lifecycle `hooks` now kill the entire process group on timeout or cancellation, so child processes spawned by shells (e.g. `sleep`) are terminated instead of outliving the parent.

## [0.2.0] - 2026-06-13

### Added

- **Subagent dispatch** — new `dispatch_subagent` tool lets the LLM delegate focused work to built-in `coder`, `explore`, and `plan` subagents. Subagents run in an ephemeral, cancellable LLM↔tool loop with per-type tool restrictions and no session persistence.
- **Lifecycle hooks** — user-configurable commands run at `session_start`, `turn_start`, `turn_end`, `tool_call`, `tool_result`, `approval_request`, and `approval_decision`. `tool_call` hooks can gate (block) tool execution on non-zero exit.
- **ACP server** — new `kimi-lite acp` subcommand speaks JSON-RPC 2.0 over stdio and supports `initialize`, `session/new`, `session/load`, `session/prompt` (with streaming `session/update` notifications), and `session/cancel`.
- **Runtime observability** — internal `api.MetricsCollector` emits counters and latency histograms from sessions, turns, tools, and LLM calls. The new `--pprof <addr>` flag exposes a `/debug/pprof` server for live profiling, and `go.uber.org/goleak` guards against goroutine leaks in CI.
- **Risk-aware approval gates** — every tool call is scored `low`/`medium`/`high` based on its name and arguments. Paths that escape the sandbox are always high risk, and destructive tools like `shell` default to high. Configurable `permission.risk_rules` override defaults; `permission.risk_threshold` controls the auto-approve cutoff.
- **Better token estimation** — context compression now uses a pluggable `TokenEstimator` with a heuristic that counts ASCII at ~4 chars/token and non-ASCII runes at 1 token each, giving more accurate budgets for mixed-language and code-heavy conversations.
- **CI coverage gate** — new `make coverage-gate` target and GitHub Actions job enforce a 70% minimum statement coverage threshold.
- **Long-running smoke test** — new `tests/long_running_test.go` runs 25 full turn cycles against a fake LLM and verifies no goroutine leaks with `goleak`.
- **Fuzz tests** — added `FuzzHeuristicTokenEstimator` and `FuzzRiskEvaluator` to `internal/core`, plus a `make fuzz` target and CI fuzz-smoke entries.
- **Windows CI build** — added a `windows-latest` build job to GitHub Actions to catch Windows cross-compile regressions.
- **README refresh** — documented the current feature set, quality signals, and development commands.
- **Benchmark regression detection** — added `scripts/benchregression`, a `make bench-regression` target, and a CI job that fails if any benchmark is more than 20% slower than the baseline.
- **Skill discovery** — markdown files in `~/.config/kimi-lite/skills/` are automatically loaded into the system prompt; `behavior.skills` selects specific skills.
- **Video input** — new `read_video` tool extracts metadata and up to 10 base64 PNG key frames from a video file using `ffmpeg`/`ffprobe` when available. The tool is read-only and sandboxed.

### Changed

- Bumped minimum Go version to `1.26.4` and upgraded `golang.org/x/net` to `v0.53.0` to resolve `govulncheck` findings.

### Fixed

- Resolved all pre-existing `golangci-lint` issues: unchecked I/O errors, `gofmt`, `wrapcheck`, `revive`, `gosec`, and `noctx`.
- Hardened `internal/config` tests against real `~/.config/kimi-lite/config.toml` by isolating `$HOME` in environment-variable resolution tests.

## [0.1.0] - 2026-06-10

### Added

- **Core chat loop** — streaming LLM responses with native Bubble Tea TUI.
- **Built-in tools** — `read_file`, `write_file`, `str_replace_file`, `glob`, `grep`, `shell`, `fetch_url` with sandboxed file access and size guards.
- **Approval system** — configurable auto-approval for read-only tools, manual confirmation for destructive operations, `--yolo` flag.
- **Session persistence** — SQLite with WAL mode, schema migrations, resume with `--continue` or `--session`.
- **Context compression** — `/compact` command to summarize history and free context window.
- **MCP integration** — connects through `mcp-guard` proxy for stable Model Context Protocol tools.
- **Fallback LLM** — automatic failover to a secondary provider on primary failure.
- **Git integration** — auto `git status` in context, `/checkpoint` to commit changes.
- **Export / Import** — `kimi-lite export` and `kimi-lite import` for portable session snapshots.
- **Health checks** — `kimi-lite doctor` verifies config, database, LLM connectivity and MCP status.
- **Per-chunk SSE timeout** — streaming reads respect `context.Context` to recover from hung connections.
- **Graceful shutdown** — `sync.WaitGroup` with 10-second timeout for in-flight turns.
- **Security** — SSRF redirect protection, DNS rebinding guard, symlink sandbox, environment isolation, 10 MB response/tool limits.
- **Observability** — `--debug` flag, sanitized error logging, structured `slog` output.
- **Cross-platform** — static binary with `CGO_ENABLED=0`, supports macOS, Linux (glibc & musl), ARM64.

[Unreleased]: https://github.com/ekhodzitsky/kimi-lite/compare/v0.6.0...HEAD
[0.6.0]: https://github.com/ekhodzitsky/kimi-lite/releases/tag/v0.6.0
[0.5.0]: https://github.com/ekhodzitsky/kimi-lite/releases/tag/v0.5.0
[0.3.0]: https://github.com/ekhodzitsky/kimi-lite/releases/tag/v0.3.0
[0.2.10]: https://github.com/ekhodzitsky/kimi-lite/releases/tag/v0.2.10
[0.2.9]: https://github.com/ekhodzitsky/kimi-lite/releases/tag/v0.2.9
[0.2.8]: https://github.com/ekhodzitsky/kimi-lite/releases/tag/v0.2.8
[0.2.7]: https://github.com/ekhodzitsky/kimi-lite/releases/tag/v0.2.7
[0.2.6]: https://github.com/ekhodzitsky/kimi-lite/releases/tag/v0.2.6
[0.2.5]: https://github.com/ekhodzitsky/kimi-lite/releases/tag/v0.2.5
[0.2.4]: https://github.com/ekhodzitsky/kimi-lite/releases/tag/v0.2.4
[0.2.3]: https://github.com/ekhodzitsky/kimi-lite/releases/tag/v0.2.3
[0.2.2]: https://github.com/ekhodzitsky/kimi-lite/releases/tag/v0.2.2
[0.2.1]: https://github.com/ekhodzitsky/kimi-lite/releases/tag/v0.2.1
[0.2.0]: https://github.com/ekhodzitsky/kimi-lite/releases/tag/v0.2.0
[0.1.0]: https://github.com/ekhodzitsky/kimi-lite/releases/tag/v0.1.0
