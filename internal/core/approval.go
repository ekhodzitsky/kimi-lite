package core

import (
	"path"
	"sync"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// ApprovalMode represents the global approval behavior.
type ApprovalMode int

const (
	// ModeManual requires manual approval for every tool call.
	// It is the zero value so an uninitialized ApprovalGate fails safe.
	ModeManual ApprovalMode = iota
	// ModeAuto auto-approves read-only tools based on configuration.
	ModeAuto
	// ModeYolo auto-approves all tools without prompting.
	ModeYolo
)

// ApprovalGate decides whether a tool call requires user approval.
type ApprovalGate struct {
	mode          ApprovalMode
	autoApprove   map[string]struct{}
	isReadOnly    func(string) bool
	rules         []api.PermissionRule
	riskEvaluator *RiskEvaluator
	riskThreshold api.RiskLevel
	mu            sync.RWMutex
}

// NewApprovalGate creates a new ApprovalGate.
//
// mode controls the global approval behavior.
// autoApprove is a list of tool names that are auto-approved in ModeAuto.
// isReadOnly is called to verify a tool is read-only before auto-approving.
// rules is an ordered list of permission rules evaluated in ModeAuto.
func NewApprovalGate(mode ApprovalMode, autoApprove []string, isReadOnly func(string) bool, rules []api.PermissionRule) *ApprovalGate {
	if isReadOnly == nil {
		isReadOnly = func(string) bool { return false }
	}
	gate := &ApprovalGate{
		mode:          mode,
		autoApprove:   make(map[string]struct{}, len(autoApprove)),
		isReadOnly:    isReadOnly,
		rules:         rules,
		riskThreshold: api.RiskLevelMedium,
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

// GetMode returns the current approval mode safely.
func (g *ApprovalGate) GetMode() ApprovalMode {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.mode
}

// SetRiskEvaluator attaches a risk evaluator and threshold to the gate.
// A nil evaluator disables risk-aware checks. An empty threshold defaults to
// medium.
func (g *ApprovalGate) SetRiskEvaluator(eval *RiskEvaluator, threshold api.RiskLevel) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.riskEvaluator = eval
	if threshold.Valid() {
		g.riskThreshold = threshold
	}
}

// AddAutoApprove adds a session-scope allow rule for the named tool.
func (g *ApprovalGate) AddAutoApprove(name string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.rules = append(g.rules, api.PermissionRule{
		Tool:     name,
		Decision: api.PermissionAllow,
		Scope:    api.PermissionScopeSession,
	})
}

// neverAutoApprove lists tools that must never be auto-approved,
// regardless of configuration.
var neverAutoApprove = map[string]struct{}{
	"shell":            {},
	"write_file":       {},
	"str_replace_file": {},
}

// matchRule reports whether ruleTool matches name using glob semantics.
func matchRule(ruleTool, name string) bool {
	if ruleTool == name {
		return true
	}
	matched, _ := path.Match(ruleTool, name)
	return matched
}

// rulePrecedence maps scopes to precedence values (higher wins).
var rulePrecedence = map[api.PermissionScope]int{
	api.PermissionScopeTurn:    3,
	api.PermissionScopeSession: 2,
	api.PermissionScopeUser:    1,
}

// evaluateRules returns the decision of the highest-precedence matching rule.
// The second result is false if no rule matched.
func (g *ApprovalGate) evaluateRules(name string) (api.PermissionDecision, bool) {
	var best api.PermissionDecision
	bestPrecedence := -1
	matched := false
	for _, r := range g.rules {
		if !matchRule(r.Tool, name) {
			continue
		}
		matched = true
		p := rulePrecedence[r.Scope]
		if p > bestPrecedence {
			bestPrecedence = p
			best = r.Decision
		}
	}
	return best, matched
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

	if _, ok := neverAutoApprove[call.Name]; ok {
		return api.ApprovalNo, false
	}

	decision, matched := g.evaluateRules(call.Name)
	if matched {
		switch decision {
		case api.PermissionAllow:
			return api.ApprovalYes, true
		case api.PermissionDeny:
			return api.ApprovalNo, true
		case api.PermissionAsk:
			return api.ApprovalNo, false
		}
	}

	// Risk-aware check: anything riskier than the configured threshold must be
	// manually approved, even if it is on the read-only auto-approve list.
	if g.riskEvaluator != nil {
		level, _ := g.riskEvaluator.Evaluate(call)
		if riskRank(level) > riskRank(g.riskThreshold) {
			return api.ApprovalNo, false
		}
	}

	// Default read-only auto-approve behavior.
	if _, ok := g.autoApprove[call.Name]; ok && g.isReadOnly(call.Name) {
		return api.ApprovalYes, true
	}

	return api.ApprovalNo, false
}
