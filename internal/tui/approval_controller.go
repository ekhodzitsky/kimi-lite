package tui

import "github.com/ekhodzitsky/kimi-lite/pkg/api"

// approvalController owns the approval state machine for pending tool calls.
// It is intentionally UI-agnostic: the Model drives it with ApprovalResponseMsg
// events and consumes the aggregated decisions when all calls have been processed.
type approvalController struct {
	reqID     int64
	calls     []api.ToolCall
	index     int
	decisions map[string]api.ApprovalDecision
}

func newApprovalController() *approvalController {
	return &approvalController{}
}

// startRequest initializes the controller with a new batch of calls to approve.
func (ac *approvalController) startRequest(calls []api.ToolCall, requestID int64) {
	ac.reqID = requestID
	ac.calls = calls
	ac.index = 0
	ac.decisions = make(map[string]api.ApprovalDecision)
}

// isActive reports whether there is a pending approval request in progress.
func (ac *approvalController) isActive() bool {
	return ac.index >= 0 && ac.index < len(ac.calls)
}

// pending returns the current batch of pending tool calls.
func (ac *approvalController) pending() []api.ToolCall {
	return ac.calls
}

// currentIndex returns the index of the call awaiting user input.
func (ac *approvalController) currentIndex() int {
	return ac.index
}

// currentCall returns the call currently awaiting approval, if any.
func (ac *approvalController) currentCall() (api.ToolCall, bool) {
	if !ac.isActive() {
		return api.ToolCall{}, false
	}
	return ac.calls[ac.index], true
}

// requestID returns the ID of the active approval request.
func (ac *approvalController) requestID() int64 {
	return ac.reqID
}

// approveCurrent produces a response message for the current call.
// The second result is false when there is no active call.
func (ac *approvalController) approveCurrent(decision api.ApprovalDecision) (ApprovalResponseMsg, bool) {
	call, ok := ac.currentCall()
	if !ok {
		return ApprovalResponseMsg{}, false
	}
	return ApprovalResponseMsg{Decision: decision, CallID: call.ID}, true
}

// handleResponse records one approval decision and advances the state machine.
// It returns done=true when all calls have been processed, along with the
// aggregated approvals map. alwaysAll is true only when the user chose "always"
// for the first call, meaning every call should be approved.
// ApprovalDiff is treated as a non-final request for more information and does
// not advance the state machine.
func (ac *approvalController) handleResponse(resp ApprovalResponseMsg) (done bool, approvals map[string]api.ApprovalDecision, alwaysAll bool) {
	if resp.Decision == api.ApprovalAlways {
		approvals = make(map[string]api.ApprovalDecision)
		for _, call := range ac.calls {
			approvals[call.ID] = api.ApprovalYes
		}
		return true, approvals, true
	}

	if resp.Decision == api.ApprovalDiff {
		return false, nil, false
	}

	ac.decisions[resp.CallID] = resp.Decision
	ac.index++

	if ac.index >= len(ac.calls) {
		approvals = make(map[string]api.ApprovalDecision)
		for _, call := range ac.calls {
			if d, ok := ac.decisions[call.ID]; ok {
				approvals[call.ID] = d
			} else {
				approvals[call.ID] = api.ApprovalNo
			}
		}
		return true, approvals, false
	}

	return false, nil, false
}

// clear resets the controller so it no longer reports active.
func (ac *approvalController) clear() {
	ac.reqID = 0
	ac.calls = nil
	ac.index = 0
	ac.decisions = nil
}
