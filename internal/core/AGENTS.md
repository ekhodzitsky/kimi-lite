# Core Business Logic Guide

> Scoped rules for `internal/core`. See root `AGENTS.md` for build/test commands
> and general Go conventions.
>
> **Version:** 2.0
> **Last updated:** 2026-06-17

## What's Inside

- `SessionManager` — create, resume, list sessions; recovers interrupted tool
  calls by synthesizing missing tool-result messages on resume.
- `TurnManager` — orchestrates input → LLM → tools → output; preserves
  multi-modal `ToolResult.ContentParts`; emits `TurnEventStatus` messages for
  non-trivial tools; streams live shell output via `ToolProgressCallback`;
  supports plan mode and mid-stream steering.
  - `RunTurnWithContentParts` / `RunTurnWithPlanWithContentParts` — multimodal
    turn entry points.
  - `RunTurnWithPlan` — assistant emits a plan and waits for user approval.
  - `ResumeWithPlan` — resumes a plan-mode turn after approval/rejection.
  - `Steer` — injects a follow-up instruction into a streaming turn.
- `BuiltInToolExecutor` — 13 built-in tools with sandboxed file access.
- `CompositeToolExecutor` — routes tool calls across multiple executors.
- `ApprovalGate` — auto/manual/yolo approval modes; integrates with
  `RiskEvaluator`.
- `RiskEvaluator` — scores tool calls `low`/`medium`/`high`.
- `ContextCompressor` — summarizes conversation history while preserving system
  prompts and pair-aware boundaries.
- `DetectLanguage` / `StatusMessage` — localized status sentences before
  non-trivial tool calls.

## Built-in Tools

Current tools: `read_file`, `write_file`, `str_replace_file`, `edit`, `glob`,
`grep`, `shell`, `fetch_url`, `list_directory`, `web_search`, `read_video`,
`dispatch_subagent`, `TodoList`.

### Tool Execution Conventions

- Tools run inside the configured sandbox root when `SandboxRoot` is set.
- `os.OpenRoot` is used with a sandbox root; `O_NOFOLLOW` fallback is used
  otherwise.
- Protected paths and sensitive system/secret trees are blocked.
- Hardlink-escape checks run on sandboxed reads.
- `web_search` is registered only when an `api.WebSearcher` provider is
  injected.
- When a tool fails, return a `ToolResult` with the error surfaced to the model
  so the turn can recover. Do not crash the agent loop.

## Adding a New Tool

1. Add the tool definition to `BuiltInToolExecutor.Definitions()` in
   `internal/core/tools.go`.
2. Add execution logic in `BuiltInToolExecutor.Execute()` switch.
3. Mark the tool as read-only in `NewBuiltInToolExecutor()` if appropriate.
4. Add the tool to `statusWorthyTools` in `internal/core/language.go` if it is
   long-running or non-trivial.
5. Update the baseline risk table in `internal/core/risk.go` if the tool is
   destructive or safety-relevant.
6. Add tests in `internal/core/tools_test.go`.
7. Update `AGENTS.md` cross-references if the tool changes safety boundaries or
   adds a new public interface.

## Approval and Risk

- `ApprovalGate` decides whether a tool call runs automatically.
- Modes: `yolo` (auto-approve), `auto` (heuristic auto-approve), `manual`.
- `RiskEvaluator` produces `low` / `medium` / `high` scores.
- User-configured rules can raise or lower risk.
- Path-escape checks are part of scoring.

## Context Compression

- Preserves leading system/identity prompts verbatim.
- Uses pair-aware boundaries so assistant/tool-call groups are not split across
  the summary/recent boundary.
- The generated summary is surfaced in the TUI transcript.

## Testing

- Use table-driven tests with `t.Parallel()`.
- Add regression tests for tool behavior changes.
- Fuzz targets exist for `HeuristicTokenEstimator` and `RiskEvaluator`;
  run with `make fuzz`.
- Mock `api.Store`, `api.LLMClient`, and `api.MCPClient` for unit tests.
