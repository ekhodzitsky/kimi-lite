# Interrupt Hook Event Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `turn_interrupt` hook event that fires when the user interrupts a running turn.

**Architecture:** Extend the `api.HookEvent` enum, trigger the hook from `TurnManager` cancellation paths, update the hook runner to allow the new event, and add tests.

**Tech Stack:** Go, existing `internal/core` hook infrastructure.

---

## Task 1: Add the new hook event constant

**Files:**
- Modify: `pkg/api/types.go:633-650`
- Test: `pkg/api/types_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestHookEvent_Interrupt(t *testing.T) {
	if api.HookTurnInterrupt.String() != "turn_interrupt" {
		t.Fatalf("unexpected event string: %s", api.HookTurnInterrupt)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test -count=1 -run TestHookEvent_Interrupt ./pkg/api/...
```

Expected: FAIL — `HookTurnInterrupt` undefined.

- [ ] **Step 3: Add the constant**

In `pkg/api/types.go`, add `HookTurnInterrupt HookEvent = "turn_interrupt"` after `HookApprovalDecision`.

- [ ] **Step 4: Run test to verify it passes**

```bash
go test -count=1 -run TestHookEvent_Interrupt ./pkg/api/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/api/types.go pkg/api/types_test.go
git commit -m "feat(api): add HookTurnInterrupt event constant"
```

## Task 2: Trigger the interrupt hook on turn cancellation

**Files:**
- Modify: `internal/core/turn.go:750-770` (near runHooks)
- Test: `internal/core/turn_test.go`

- [ ] **Step 1: Write the failing test**

Add a test that sets a hook runner recording `HookTurnInterrupt`, starts a turn, calls `CancelAll`, and asserts the event was recorded with the correct session/turn IDs.

- [ ] **Step 2: Run test to verify it fails**

```bash
go test -count=1 -run TestTurnManager_InterruptHook ./internal/core/...
```

Expected: FAIL — hook not called.

- [ ] **Step 3: Fire the hook from CancelAll**

In `internal/core/turn.go`, in `CancelAll` (or the stream interruption path), call:

```go
tm.runHooks(ctx, api.HookTurnInterrupt, sessionID, turnID, "")
```

Use the current turn's session/turn IDs. Ensure it runs even if the turn is already cancelling.

- [ ] **Step 4: Run test to verify it passes**

```bash
go test -count=1 -run TestTurnManager_InterruptHook ./internal/core/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/core/turn.go internal/core/turn_test.go
git commit -m "feat(core): fire turn_interrupt hook on cancellation"
```

## Task 3: Update hook runner event validation

**Files:**
- Modify: `internal/core/hooks/runner.go` (if it validates allowed events)
- Test: `internal/core/hooks/runner_test.go`

- [ ] **Step 1: Check runner validation**

Search `internal/core/hooks/runner.go` for a list/map of allowed events. If none exists, skip to Task 4.

- [ ] **Step 2: Add turn_interrupt to allowed set**

Add `api.HookTurnInterrupt` to the allowed events list/map.

- [ ] **Step 3: Add/update test**

Write a test that creates a hook config with `event: turn_interrupt` and verifies the runner accepts it.

- [ ] **Step 4: Run tests**

```bash
go test -count=1 ./internal/core/hooks/...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/core/hooks/
git commit -m "feat(hooks): allow turn_interrupt in hook runner"
```

## Task 4: Update system prompt guidance

**Files:**
- Modify: `internal/app/prompt.go` (if it lists hook events)

- [ ] **Step 1: Find hook event references**

Search `internal/app/prompt.go` for existing hook event names.

- [ ] **Step 2: Add turn_interrupt**

If hook events are documented in the system prompt, add `turn_interrupt` to the list with a one-line description.

- [ ] **Step 3: Run tests**

```bash
go test -count=1 ./internal/app/...
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/app/prompt.go
git commit -m "docs(prompt): mention turn_interrupt hook event"
```

## Task 5: Full verification

- [ ] **Step 1: Run full test suite**

```bash
go test -race -count=1 ./...
```

Expected: all packages PASS.

- [ ] **Step 2: Run linter**

```bash
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 run ./...
```

Expected: 0 issues.

- [ ] **Step 3: Update CHANGELOG**

Add under `[Unreleased]`:

```markdown
- feat: add `turn_interrupt` hook event fired when the user cancels a turn.
```

- [ ] **Step 4: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs: changelog for turn_interrupt hook"
```

---

## Self-review

- Spec coverage: design doc section "2. Interrupt hook event" is fully covered.
- No placeholders: every step has concrete code/commands.
- Type consistency: `api.HookTurnInterrupt` is used everywhere.
