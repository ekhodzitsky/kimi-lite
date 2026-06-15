package core

import (
	"sync"
	"testing"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func testIsReadOnly(name string) bool {
	switch name {
	case "read_file", "glob", "grep", "fetch_url", "list_directory":
		return true
	default:
		return false
	}
}

func TestApprovalGate_ModeYolo(t *testing.T) {
	t.Parallel()
	gate := NewApprovalGate(ModeYolo, []string{}, testIsReadOnly, nil)

	decision, auto := gate.ShouldAutoApprove(api.ToolCall{Name: "write_file"})
	if !auto {
		t.Error("expected auto-approved in yolo mode")
	}
	if decision != api.ApprovalYes {
		t.Errorf("decision = %d, want ApprovalYes", decision)
	}
}

func TestApprovalGate_ModeAuto_ReadOnly(t *testing.T) {
	t.Parallel()
	gate := NewApprovalGate(ModeAuto, []string{"read_file", "glob"}, testIsReadOnly, nil)

	decision, auto := gate.ShouldAutoApprove(api.ToolCall{Name: "read_file"})
	if !auto {
		t.Error("expected auto-approved for read-only tool")
	}
	if decision != api.ApprovalYes {
		t.Errorf("decision = %d, want ApprovalYes", decision)
	}
}

func TestApprovalGate_ModeAuto_ListDirectory(t *testing.T) {
	g := NewApprovalGate(ModeAuto, []string{"list_directory"}, testIsReadOnly, nil)
	decision, ok := g.ShouldAutoApprove(api.ToolCall{Name: "list_directory"})
	if !ok {
		t.Fatal("expected auto-approval for list_directory")
	}
	if decision != api.ApprovalYes {
		t.Fatalf("expected ApprovalYes, got %v", decision)
	}
}

func TestApprovalGate_ModeAuto_WriteTool(t *testing.T) {
	t.Parallel()
	gate := NewApprovalGate(ModeAuto, []string{"read_file"}, testIsReadOnly, nil)

	decision, auto := gate.ShouldAutoApprove(api.ToolCall{Name: "write_file"})
	if auto {
		t.Error("expected manual approval for write tool")
	}
	if decision != api.ApprovalNo {
		t.Errorf("decision = %d, want ApprovalNo", decision)
	}
}

func TestApprovalGate_ModeAuto_WriteToolInList(t *testing.T) {
	t.Parallel()
	gate := NewApprovalGate(ModeAuto, []string{"write_file"}, testIsReadOnly, nil)

	decision, auto := gate.ShouldAutoApprove(api.ToolCall{Name: "write_file"})
	if auto {
		t.Error("expected manual approval for write tool even if in autoApprove list")
	}
	if decision != api.ApprovalNo {
		t.Errorf("decision = %d, want ApprovalNo", decision)
	}
}

func TestApprovalGate_ModeAuto_MCPTool(t *testing.T) {
	t.Parallel()
	gate := NewApprovalGate(ModeAuto, []string{"mcp_some_tool"}, func(name string) bool {
		return false
	}, nil)

	decision, auto := gate.ShouldAutoApprove(api.ToolCall{Name: "mcp_some_tool"})
	if auto {
		t.Error("expected manual approval for mcp tool")
	}
	if decision != api.ApprovalNo {
		t.Errorf("decision = %d, want ApprovalNo", decision)
	}
}

func TestApprovalGate_ModeAuto_ReadOnlyMCPTool(t *testing.T) {
	t.Parallel()
	gate := NewApprovalGate(ModeAuto, []string{"mcp_read_file"}, func(name string) bool {
		return name == "mcp_read_file"
	}, nil)

	decision, auto := gate.ShouldAutoApprove(api.ToolCall{Name: "mcp_read_file"})
	if !auto {
		t.Fatal("expected auto-approval for read-only mcp tool")
	}
	if decision != api.ApprovalYes {
		t.Errorf("decision = %d, want ApprovalYes", decision)
	}
}

func TestApprovalGate_ModeManual(t *testing.T) {
	t.Parallel()
	gate := NewApprovalGate(ModeManual, []string{"read_file", "glob"}, testIsReadOnly, nil)

	decision, auto := gate.ShouldAutoApprove(api.ToolCall{Name: "read_file"})
	if auto {
		t.Error("expected manual approval in manual mode")
	}
	if decision != api.ApprovalNo {
		t.Errorf("decision = %d, want ApprovalNo", decision)
	}
}

func TestApprovalGate_SetMode(t *testing.T) {
	t.Parallel()
	gate := NewApprovalGate(ModeManual, []string{"read_file"}, testIsReadOnly, nil)

	decision, auto := gate.ShouldAutoApprove(api.ToolCall{Name: "read_file"})
	if auto {
		t.Fatal("expected manual approval")
	}

	gate.SetMode(ModeAuto)
	decision, auto = gate.ShouldAutoApprove(api.ToolCall{Name: "read_file"})
	if !auto {
		t.Fatal("expected auto-approved after mode change")
	}
	if decision != api.ApprovalYes {
		t.Errorf("decision = %d, want ApprovalYes", decision)
	}
}

func TestApprovalGate_AddAutoApprove(t *testing.T) {
	t.Parallel()
	gate := NewApprovalGate(ModeAuto, []string{}, testIsReadOnly, nil)

	// Before adding, read_file is not auto-approved.
	decision, auto := gate.ShouldAutoApprove(api.ToolCall{Name: "read_file"})
	if auto {
		t.Error("expected manual approval before AddAutoApprove")
	}
	if decision != api.ApprovalNo {
		t.Errorf("decision = %d, want ApprovalNo", decision)
	}

	gate.AddAutoApprove("read_file")

	// After adding, read_file is auto-approved.
	decision, auto = gate.ShouldAutoApprove(api.ToolCall{Name: "read_file"})
	if !auto {
		t.Error("expected auto-approved after AddAutoApprove")
	}
	if decision != api.ApprovalYes {
		t.Errorf("decision = %d, want ApprovalYes", decision)
	}
}

func TestApprovalGate_ConcurrentSetMode(t *testing.T) {
	t.Parallel()
	gate := NewApprovalGate(ModeAuto, []string{"read_file"}, testIsReadOnly, nil)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		gate.SetMode(ModeYolo)
	}()
	go func() {
		defer wg.Done()
		gate.SetMode(ModeManual)
	}()

	wg.Wait()
	gate.ShouldAutoApprove(api.ToolCall{Name: "read_file"})
}

func TestApprovalGate_UserRule_DenyReadFile(t *testing.T) {
	t.Parallel()
	rules := []api.PermissionRule{
		{Tool: "read_file", Decision: api.PermissionDeny, Scope: api.PermissionScopeUser},
	}
	gate := NewApprovalGate(ModeAuto, []string{"read_file"}, testIsReadOnly, rules)
	decision, auto := gate.ShouldAutoApprove(api.ToolCall{Name: "read_file"})
	if !auto {
		t.Fatal("expected auto decision for deny rule")
	}
	if decision != api.ApprovalNo {
		t.Fatalf("expected ApprovalNo, got %v", decision)
	}
}

func TestApprovalGate_SessionRule_OverridesUser(t *testing.T) {
	t.Parallel()
	rules := []api.PermissionRule{
		{Tool: "read_file", Decision: api.PermissionDeny, Scope: api.PermissionScopeUser},
		{Tool: "read_file", Decision: api.PermissionAllow, Scope: api.PermissionScopeSession},
	}
	gate := NewApprovalGate(ModeAuto, []string{}, testIsReadOnly, rules)
	decision, auto := gate.ShouldAutoApprove(api.ToolCall{Name: "read_file"})
	if !auto {
		t.Fatal("expected auto-approval from session rule")
	}
	if decision != api.ApprovalYes {
		t.Fatalf("expected ApprovalYes, got %v", decision)
	}
}

func TestApprovalGate_AddAutoApprove_CreatesSessionRule(t *testing.T) {
	t.Parallel()
	gate := NewApprovalGate(ModeAuto, []string{}, testIsReadOnly, nil)
	gate.AddAutoApprove("read_file")
	decision, auto := gate.ShouldAutoApprove(api.ToolCall{Name: "read_file"})
	if !auto {
		t.Fatal("expected auto-approval after AddAutoApprove")
	}
	if decision != api.ApprovalYes {
		t.Fatalf("expected ApprovalYes, got %v", decision)
	}
}

func TestApprovalGate_UserRule_AskReadFile(t *testing.T) {
	t.Parallel()
	rules := []api.PermissionRule{
		{Tool: "read_file", Decision: api.PermissionAsk, Scope: api.PermissionScopeUser},
	}
	gate := NewApprovalGate(ModeAuto, []string{"read_file"}, testIsReadOnly, rules)
	decision, auto := gate.ShouldAutoApprove(api.ToolCall{Name: "read_file"})
	if auto {
		t.Fatal("expected manual approval for ask rule")
	}
	if decision != api.ApprovalNo {
		t.Fatalf("expected ApprovalNo, got %v", decision)
	}
}

func TestApprovalGate_EditTool_NeverAutoApprove(t *testing.T) {
	t.Parallel()
	gate := NewApprovalGate(ModeAuto, []string{"edit"}, func(name string) bool { return name == "edit" }, nil)
	decision, auto := gate.ShouldAutoApprove(api.ToolCall{Name: "edit"})
	if auto {
		t.Error("expected manual approval for edit tool")
	}
	if decision != api.ApprovalNo {
		t.Errorf("decision = %d, want ApprovalNo", decision)
	}
}

func TestApprovalGate_AddAutoApprove_Dedupes(t *testing.T) {
	t.Parallel()
	gate := NewApprovalGate(ModeAuto, []string{}, func(name string) bool { return name == "read_file" }, nil)
	gate.AddAutoApprove("read_file")
	gate.AddAutoApprove("read_file")

	rules := 0
	for _, r := range gate.rules {
		if r.Tool == "read_file" && r.Decision == api.PermissionAllow && r.Scope == api.PermissionScopeSession {
			rules++
		}
	}
	if rules != 1 {
		t.Errorf("expected 1 session allow rule for read_file, got %d", rules)
	}
}

func TestApprovalGate_GlobRule(t *testing.T) {
	t.Parallel()
	rules := []api.PermissionRule{
		{Tool: "mcp_*", Decision: api.PermissionAllow, Scope: api.PermissionScopeUser},
	}
	gate := NewApprovalGate(ModeAuto, []string{}, func(string) bool { return true }, rules)
	decision, auto := gate.ShouldAutoApprove(api.ToolCall{Name: "mcp_some_tool"})
	if !auto {
		t.Fatal("expected glob rule to match")
	}
	if decision != api.ApprovalYes {
		t.Fatalf("expected ApprovalYes, got %v", decision)
	}
}

func TestApprovalGate_RiskEvaluator_HighRisk(t *testing.T) {
	t.Parallel()

	gate := NewApprovalGate(ModeAuto, []string{"write_file"}, func(string) bool { return true }, nil)
	gate.SetRiskEvaluator(NewRiskEvaluator(nil, "/tmp"), api.RiskLevelMedium)

	decision, auto := gate.ShouldAutoApprove(api.ToolCall{Name: "write_file", Arguments: `{"path":"/etc/passwd","content":"x"}`})
	if auto {
		t.Fatal("expected write_file to require approval")
	}
	if decision != api.ApprovalNo {
		t.Fatalf("expected ApprovalNo, got %v", decision)
	}
}

func TestApprovalGate_RiskEvaluator_LowRiskReadOnly(t *testing.T) {
	t.Parallel()

	gate := NewApprovalGate(ModeAuto, []string{"read_file"}, func(name string) bool { return name == "read_file" }, nil)
	gate.SetRiskEvaluator(NewRiskEvaluator(nil, "/tmp"), api.RiskLevelMedium)

	decision, auto := gate.ShouldAutoApprove(api.ToolCall{Name: "read_file", Arguments: `{"path":"/tmp/file.txt"}`})
	if !auto || decision != api.ApprovalYes {
		t.Fatalf("expected auto-approval, got decision=%v auto=%v", decision, auto)
	}
}

func TestApprovalGate_RiskEvaluator_Yolo(t *testing.T) {
	t.Parallel()

	gate := NewApprovalGate(ModeYolo, []string{}, func(string) bool { return false }, nil)
	gate.SetRiskEvaluator(NewRiskEvaluator(nil, "/tmp"), api.RiskLevelMedium)

	decision, auto := gate.ShouldAutoApprove(api.ToolCall{Name: "shell", Arguments: `{"command":"rm -rf /"}`})
	if !auto || decision != api.ApprovalYes {
		t.Fatalf("expected yolo approval, got decision=%v auto=%v", decision, auto)
	}
}

func TestApprovalGate_RiskEvaluator_ReadOnlyTool_HighRiskRule(t *testing.T) {
	t.Parallel()
	rules := []api.RiskRule{{Tool: "read_file", Level: api.RiskLevelHigh, Message: "custom high"}}
	gate := NewApprovalGate(ModeAuto, []string{"read_file"}, func(name string) bool { return name == "read_file" }, nil)
	gate.SetRiskEvaluator(NewRiskEvaluator(rules, "/tmp"), api.RiskLevelMedium)

	decision, auto := gate.ShouldAutoApprove(api.ToolCall{Name: "read_file", Arguments: `{"path":"/tmp/file.txt"}`})
	if auto {
		t.Fatal("expected high-risk read_file to require approval")
	}
	if decision != api.ApprovalNo {
		t.Errorf("decision = %v, want ApprovalNo", decision)
	}
}

func TestApprovalGate_GetMode(t *testing.T) {
	t.Parallel()
	gate := NewApprovalGate(ModeManual, nil, testIsReadOnly, nil)
	if got := gate.GetMode(); got != ModeManual {
		t.Errorf("GetMode() = %v, want ModeManual", got)
	}
	gate.SetMode(ModeAuto)
	if got := gate.GetMode(); got != ModeAuto {
		t.Errorf("GetMode() = %v, want ModeAuto", got)
	}
}

func TestApprovalGate_NewApprovalGate_NilIsReadOnly(t *testing.T) {
	t.Parallel()
	gate := NewApprovalGate(ModeAuto, []string{"read_file"}, nil, nil)
	// nil isReadOnly defaults to "always false", so read_file should not auto-approve.
	decision, auto := gate.ShouldAutoApprove(api.ToolCall{Name: "read_file"})
	if auto {
		t.Error("expected manual approval with nil isReadOnly")
	}
	if decision != api.ApprovalNo {
		t.Errorf("decision = %v, want ApprovalNo", decision)
	}
}

func TestApprovalGate_evaluateRules_LowerPrecedenceDoesNotOverride(t *testing.T) {
	t.Parallel()
	rules := []api.PermissionRule{
		{Tool: "read_file", Decision: api.PermissionAllow, Scope: api.PermissionScopeTurn},
		{Tool: "read_file", Decision: api.PermissionDeny, Scope: api.PermissionScopeUser},
	}
	gate := NewApprovalGate(ModeAuto, []string{}, testIsReadOnly, rules)
	decision, auto := gate.ShouldAutoApprove(api.ToolCall{Name: "read_file"})
	if !auto {
		t.Fatal("expected auto decision")
	}
	if decision != api.ApprovalYes {
		t.Errorf("decision = %v, want ApprovalYes", decision)
	}
}

func TestApprovalGate_evaluateRules_GlobMismatch(t *testing.T) {
	t.Parallel()
	rules := []api.PermissionRule{
		{Tool: "mcp_*", Decision: api.PermissionAllow, Scope: api.PermissionScopeUser},
	}
	gate := NewApprovalGate(ModeAuto, []string{}, func(string) bool { return true }, rules)
	// "other" does not match the glob rule.
	decision, auto := gate.ShouldAutoApprove(api.ToolCall{Name: "other"})
	if auto {
		t.Fatal("expected no rule match")
	}
	if decision != api.ApprovalNo {
		t.Errorf("decision = %v, want ApprovalNo", decision)
	}
}

func TestApprovalGate_SetRiskEvaluator_InvalidThresholdDefaults(t *testing.T) {
	t.Parallel()
	gate := NewApprovalGate(ModeAuto, []string{"read_file"}, testIsReadOnly, nil)
	gate.SetRiskEvaluator(NewRiskEvaluator(nil, "/tmp"), api.RiskLevel("invalid"))
	if gate.riskThreshold != api.RiskLevelMedium {
		t.Errorf("riskThreshold = %v, want medium", gate.riskThreshold)
	}
}

func TestApprovalGate_NewApprovalGate_CopiesRules(t *testing.T) {
	t.Parallel()
	rules := []api.PermissionRule{
		{Tool: "read_file", Decision: api.PermissionAllow, Scope: api.PermissionScopeUser},
	}
	gate := NewApprovalGate(ModeAuto, nil, testIsReadOnly, rules)
	rules[0].Decision = api.PermissionDeny

	decision, auto := gate.ShouldAutoApprove(api.ToolCall{Name: "read_file"})
	if !auto || decision != api.ApprovalYes {
		t.Fatalf("expected copied allow rule to remain, got decision=%v auto=%v", decision, auto)
	}
}

func TestApprovalGate_AddAutoApprove_ReplacesSessionDeny(t *testing.T) {
	t.Parallel()
	rules := []api.PermissionRule{
		{Tool: "read_file", Decision: api.PermissionDeny, Scope: api.PermissionScopeSession},
	}
	gate := NewApprovalGate(ModeAuto, nil, testIsReadOnly, rules)
	gate.AddAutoApprove("read_file")

	decision, auto := gate.ShouldAutoApprove(api.ToolCall{Name: "read_file"})
	if !auto || decision != api.ApprovalYes {
		t.Fatalf("expected session allow to replace session deny, got decision=%v auto=%v", decision, auto)
	}
}

func TestApprovalGate_matchRule_InvalidGlob(t *testing.T) {
	t.Parallel()
	if matchRule("[", "read_file") {
		t.Error("expected invalid glob pattern to not match")
	}
}
