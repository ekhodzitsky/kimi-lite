package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"path/filepath"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// baselineRisk maps built-in tool names to their default risk level.
var baselineRisk = map[string]api.RiskLevel{
	"read_file":         api.RiskLevelLow,
	"glob":              api.RiskLevelLow,
	"grep":              api.RiskLevelLow,
	"fetch_url":         api.RiskLevelLow,
	"web_search":        api.RiskLevelLow,
	"list_directory":    api.RiskLevelLow,
	"TodoList":          api.RiskLevelLow,
	"read_video":        api.RiskLevelLow,
	"write_file":        api.RiskLevelMedium,
	"str_replace_file":  api.RiskLevelMedium,
	"edit":              api.RiskLevelMedium,
	"dispatch_subagent": api.RiskLevelMedium,
	"shell":             api.RiskLevelHigh,
}

// RiskEvaluator scores tool calls based on built-in baselines, path safety, and
// user-configured rules.
type RiskEvaluator struct {
	rules          []api.RiskRule
	sandboxRoot    string
	protectedPaths []string
}

// NewRiskEvaluator creates a risk evaluator with the given rules and sandbox
// directory. A nil or empty rules list is valid and uses baseline risks only.
func NewRiskEvaluator(rules []api.RiskRule, sandboxRoot string) *RiskEvaluator {
	return &RiskEvaluator{
		rules:          append([]api.RiskRule(nil), rules...),
		sandboxRoot:    sandboxRoot,
		protectedPaths: nil,
	}
}

// SetProtectedPaths configures additional paths that are treated as escapes
// when a tool targets them. A defensive copy is made.
func (e *RiskEvaluator) SetProtectedPaths(paths []string) {
	e.protectedPaths = append([]string(nil), paths...)
}

// Evaluate returns the risk level for a tool call and a human-readable reason.
func (e *RiskEvaluator) Evaluate(call api.ToolCall) (api.RiskLevel, string) {
	args, parseErr := e.parseArgs(call.Arguments)
	filePath := e.filePath(args)

	level := e.baseline(call.Name)
	reason := fmt.Sprintf("baseline risk for %s is %s", call.Name, level)

	if parseErr != nil {
		return level, fmt.Sprintf("baseline risk for %s is %s (failed to parse arguments: %v)", call.Name, level, parseErr)
	}

	if filePath != "" {
		if e.pathEscapes(filePath) {
			level = api.RiskLevelHigh
			reason = fmt.Sprintf("path %q escapes sandbox %q", filePath, e.sandboxRoot)
		}
	}

	for _, rule := range e.rules {
		if !matchRule(rule.Tool, call.Name) {
			continue
		}
		if rule.Path != "" && !pathMatches(rule.Path, filePath) {
			continue
		}
		if rule.Level.Valid() {
			level = rule.Level
			if rule.Message != "" {
				reason = rule.Message
			} else {
				reason = fmt.Sprintf("rule %q set risk to %s", rule.Tool, level)
			}
		}
	}

	// Path escape wins over any rule that tries to downplay an unsafe path.
	if filePath != "" && e.pathEscapes(filePath) {
		level = api.RiskLevelHigh
		reason = fmt.Sprintf("path %q escapes sandbox %q", filePath, e.sandboxRoot)
	}

	return level, reason
}

// parseArgs decodes the tool-call arguments JSON into a generic map.
func (e *RiskEvaluator) parseArgs(raw string) (map[string]any, error) {
	var args map[string]any
	if raw == "" {
		return args, nil
	}
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return nil, fmt.Errorf("unmarshal arguments: %w", err)
	}
	return args, nil
}

// filePath extracts a filesystem path from common tool arguments.
func (e *RiskEvaluator) filePath(args map[string]any) string {
	for _, key := range []string{"path", "file", "filename", "target"} {
		if v, ok := args[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// baseline returns the built-in risk for a tool, defaulting to medium.
func (e *RiskEvaluator) baseline(name string) api.RiskLevel {
	if level, ok := baselineRisk[name]; ok {
		return level
	}
	return api.RiskLevelMedium
}

// pathEscapes reports whether the given path resolves outside the sandbox root
// or is blocked by ValidateFilePath (sensitive paths, secret trees, protected
// paths). It expands "~" and resolves symlinks on both the path and the root
// so that escapes via relative segments or symlinks are detected.
func (e *RiskEvaluator) pathEscapes(p string) bool {
	_, err := validateFilePath(p, e.sandboxRoot, e.protectedPaths, nil)
	if err == nil {
		return false
	}
	return errors.Is(err, ErrSandboxViolation)
}

// pathMatches reports whether value matches pattern using glob semantics.
// Both paths are normalized to forward slashes before matching so separators
// are consistent across operating systems.
func pathMatches(pattern, value string) bool {
	if pattern == value {
		return true
	}
	matched, _ := path.Match(filepath.ToSlash(pattern), filepath.ToSlash(value))
	return matched
}

// riskRank maps a risk level to a numeric rank for comparison.
func riskRank(level api.RiskLevel) int {
	switch level {
	case api.RiskLevelLow:
		return 1
	case api.RiskLevelMedium:
		return 2
	case api.RiskLevelHigh:
		return 3
	}
	return 0
}
