package core

import (
	"sync"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// ApprovalMode represents the global approval behavior.
type ApprovalMode int

const (
	// ModeAuto auto-approves read-only tools based on configuration.
	ModeAuto ApprovalMode = iota
	// ModeYolo auto-approves all tools without prompting.
	ModeYolo
	// ModeManual requires manual approval for every tool call.
	ModeManual
)

// ApprovalGate decides whether a tool call requires user approval.
type ApprovalGate struct {
	mode        ApprovalMode
	autoApprove map[string]struct{}
	mu          sync.RWMutex
}

// NewApprovalGate creates a new ApprovalGate.
//
// mode controls the global approval behavior.
// autoApprove is a list of tool names that are auto-approved in ModeAuto.
func NewApprovalGate(mode ApprovalMode, autoApprove []string) *ApprovalGate {
	gate := &ApprovalGate{
		mode:        mode,
		autoApprove: make(map[string]struct{}, len(autoApprove)),
	}
	for _, name := range autoApprove {
		gate.autoApprove[name] = struct{}{}
	}
	return gate
}

// SetMode updates the approval mode safely.
func (g *ApprovalGate) SetMode(mode ApprovalMode) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.mode = mode
}

// ShouldAutoApprove returns the auto-approval decision for a tool call.
// If the tool requires manual approval, it returns (ApprovalNo, false).
func (g *ApprovalGate) ShouldAutoApprove(call api.ToolCall) (api.ApprovalDecision, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.mode == ModeYolo {
		return api.ApprovalYes, true
	}

	if g.mode == ModeManual {
		return api.ApprovalNo, false
	}

	if _, ok := g.autoApprove[call.Name]; ok {
		return api.ApprovalYes, true
	}

	return api.ApprovalNo, false
}
