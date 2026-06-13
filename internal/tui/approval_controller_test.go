package tui

import (
	"testing"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func TestApprovalController_StartRequest(t *testing.T) {
	t.Parallel()

	ac := newApprovalController()
	calls := []api.ToolCall{
		{ID: "a", Name: "read_file"},
		{ID: "b", Name: "write_file"},
	}

	ac.startRequest(calls, 42)

	if !ac.isActive() {
		t.Error("expected controller to be active after startRequest")
	}
	if got := ac.requestID(); got != 42 {
		t.Errorf("requestID() = %d, want 42", got)
	}
	if got := len(ac.pending()); got != 2 {
		t.Errorf("len(pending()) = %d, want 2", got)
	}
	if got := ac.currentIndex(); got != 0 {
		t.Errorf("currentIndex() = %d, want 0", got)
	}
}

func TestApprovalController_ApproveCurrent(t *testing.T) {
	t.Parallel()

	ac := newApprovalController()
	ac.startRequest([]api.ToolCall{{ID: "a", Name: "read_file"}}, 1)

	resp, ok := ac.approveCurrent(api.ApprovalYes)
	if !ok {
		t.Fatal("expected approveCurrent to succeed")
	}
	if resp.Decision != api.ApprovalYes {
		t.Errorf("Decision = %v, want ApprovalYes", resp.Decision)
	}
	if resp.CallID != "a" {
		t.Errorf("CallID = %q, want a", resp.CallID)
	}

	// After handling the response, there is no next call.
	ac.handleResponse(resp)
	_, ok = ac.approveCurrent(api.ApprovalYes)
	if ok {
		t.Error("expected approveCurrent to fail when no active call")
	}
}

func TestApprovalController_HandleResponse_PerCallYesNo(t *testing.T) {
	t.Parallel()

	ac := newApprovalController()
	ac.startRequest([]api.ToolCall{
		{ID: "a", Name: "read_file"},
		{ID: "b", Name: "write_file"},
	}, 1)

	// First call approved.
	done, _, _ := ac.handleResponse(ApprovalResponseMsg{Decision: api.ApprovalYes, CallID: "a"})
	if done {
		t.Fatal("expected not done after first response")
	}
	if ac.currentIndex() != 1 {
		t.Errorf("currentIndex() = %d, want 1", ac.currentIndex())
	}

	// Second call denied.
	done, approvals, alwaysAll := ac.handleResponse(ApprovalResponseMsg{Decision: api.ApprovalNo, CallID: "b"})
	if !done {
		t.Fatal("expected done after second response")
	}
	if alwaysAll {
		t.Error("expected alwaysAll=false for per-call decisions")
	}
	if approvals["a"] != api.ApprovalYes {
		t.Errorf("approvals[a] = %v, want ApprovalYes", approvals["a"])
	}
	if approvals["b"] != api.ApprovalNo {
		t.Errorf("approvals[b] = %v, want ApprovalNo", approvals["b"])
	}
}

func TestApprovalController_HandleResponse_ApprovalAlways(t *testing.T) {
	t.Parallel()

	ac := newApprovalController()
	ac.startRequest([]api.ToolCall{
		{ID: "a", Name: "read_file"},
		{ID: "b", Name: "write_file"},
	}, 1)

	done, approvals, alwaysAll := ac.handleResponse(ApprovalResponseMsg{Decision: api.ApprovalAlways, CallID: "a"})
	if !done {
		t.Fatal("expected done immediately for ApprovalAlways")
	}
	if !alwaysAll {
		t.Error("expected alwaysAll=true for ApprovalAlways")
	}
	if approvals["a"] != api.ApprovalYes {
		t.Errorf("approvals[a] = %v, want ApprovalYes", approvals["a"])
	}
	if approvals["b"] != api.ApprovalYes {
		t.Errorf("approvals[b] = %v, want ApprovalYes", approvals["b"])
	}
}

func TestApprovalController_HandleResponse_DefaultsMissingToNo(t *testing.T) {
	t.Parallel()

	ac := newApprovalController()
	ac.startRequest([]api.ToolCall{
		{ID: "a", Name: "read_file"},
		{ID: "b", Name: "write_file"},
	}, 1)

	// Approve only the first call; the second response is somehow skipped.
	done, approvals, _ := ac.handleResponse(ApprovalResponseMsg{Decision: api.ApprovalYes, CallID: "a"})
	if done {
		t.Fatal("expected not done after first response")
	}

	// Simulate an out-of-band finalization by advancing index manually.
	ac.index++
	done, approvals, _ = ac.handleResponse(ApprovalResponseMsg{Decision: api.ApprovalYes, CallID: "b"})
	if !done {
		t.Fatal("expected done")
	}
	if approvals["b"] != api.ApprovalYes {
		t.Errorf("approvals[b] = %v, want ApprovalYes", approvals["b"])
	}

	// Now test the defaulting behavior on a fresh controller where a decision
	// is truly missing for a call ID.
	ac2 := newApprovalController()
	ac2.startRequest([]api.ToolCall{
		{ID: "x", Name: "read_file"},
		{ID: "y", Name: "write_file"},
	}, 2)
	ac2.decisions["x"] = api.ApprovalYes
	ac2.index = 2
	done, approvals, _ = ac2.handleResponse(ApprovalResponseMsg{Decision: api.ApprovalYes, CallID: "y"})
	if !done {
		t.Fatal("expected done")
	}
	if approvals["y"] != api.ApprovalYes {
		t.Errorf("approvals[y] = %v, want ApprovalYes", approvals["y"])
	}
}

func TestApprovalController_HandleResponse_ApprovalDiff(t *testing.T) {
	t.Parallel()

	ac := newApprovalController()
	ac.startRequest([]api.ToolCall{
		{ID: "a", Name: "write_file"},
	}, 1)

	done, approvals, alwaysAll := ac.handleResponse(ApprovalResponseMsg{Decision: api.ApprovalDiff, CallID: "a"})
	if done {
		t.Error("expected not done for ApprovalDiff")
	}
	if approvals != nil {
		t.Errorf("expected nil approvals, got %v", approvals)
	}
	if alwaysAll {
		t.Error("expected alwaysAll=false for ApprovalDiff")
	}
	if ac.currentIndex() != 0 {
		t.Errorf("currentIndex = %d, want 0", ac.currentIndex())
	}
	if _, ok := ac.decisions["a"]; ok {
		t.Error("ApprovalDiff should not be recorded as a final decision")
	}

	// A subsequent yes should finalize.
	done, approvals, _ = ac.handleResponse(ApprovalResponseMsg{Decision: api.ApprovalYes, CallID: "a"})
	if !done {
		t.Error("expected done after ApprovalYes")
	}
	if approvals["a"] != api.ApprovalYes {
		t.Errorf("approvals[a] = %v, want ApprovalYes", approvals["a"])
	}
}

func TestApprovalController_Clear(t *testing.T) {
	t.Parallel()

	ac := newApprovalController()
	ac.startRequest([]api.ToolCall{{ID: "a", Name: "read_file"}}, 1)
	ac.clear()

	if ac.isActive() {
		t.Error("expected controller to be inactive after clear")
	}
	if ac.requestID() != 0 {
		t.Errorf("requestID() = %d, want 0", ac.requestID())
	}
	if len(ac.pending()) != 0 {
		t.Errorf("len(pending()) = %d, want 0", len(ac.pending()))
	}
}

func TestApprovalController_CurrentCall(t *testing.T) {
	t.Parallel()

	ac := newApprovalController()
	_, ok := ac.currentCall()
	if ok {
		t.Error("expected no current call on fresh controller")
	}

	ac.startRequest([]api.ToolCall{
		{ID: "a", Name: "read_file"},
		{ID: "b", Name: "write_file"},
	}, 1)

	call, ok := ac.currentCall()
	if !ok || call.ID != "a" {
		t.Errorf("currentCall() = (%+v, %v), want (a, true)", call, ok)
	}
}

func TestApprovalController_HandleResponse_MismatchedCallID(t *testing.T) {
	t.Parallel()

	ac := newApprovalController()
	ac.startRequest([]api.ToolCall{
		{ID: "a", Name: "read_file"},
		{ID: "b", Name: "write_file"},
	}, 1)

	// A decision for a non-current call must be ignored and must not advance.
	done, _, _ := ac.handleResponse(ApprovalResponseMsg{Decision: api.ApprovalYes, CallID: "b"})
	if done {
		t.Fatal("expected not done for mismatched CallID")
	}
	if ac.currentIndex() != 0 {
		t.Errorf("currentIndex = %d, want 0", ac.currentIndex())
	}
	if _, ok := ac.decisions["b"]; ok {
		t.Error("decision for non-current call should not be recorded")
	}

	// An unknown CallID is also ignored.
	done, _, _ = ac.handleResponse(ApprovalResponseMsg{Decision: api.ApprovalYes, CallID: "unknown"})
	if done {
		t.Fatal("expected not done for unknown CallID")
	}
}
