package core

import (
	"testing"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func TestApprovalGate_ModeYolo(t *testing.T) {
	t.Parallel()
	gate := NewApprovalGate(ModeYolo, []string{})

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
	gate := NewApprovalGate(ModeAuto, []string{"read_file", "glob"})

	decision, auto := gate.ShouldAutoApprove(api.ToolCall{Name: "read_file"})
	if !auto {
		t.Error("expected auto-approved for read-only tool")
	}
	if decision != api.ApprovalYes {
		t.Errorf("decision = %d, want ApprovalYes", decision)
	}
}

func TestApprovalGate_ModeAuto_WriteTool(t *testing.T) {
	t.Parallel()
	gate := NewApprovalGate(ModeAuto, []string{"read_file"})

	decision, auto := gate.ShouldAutoApprove(api.ToolCall{Name: "write_file"})
	if auto {
		t.Error("expected manual approval for write tool")
	}
	if decision != api.ApprovalNo {
		t.Errorf("decision = %d, want ApprovalNo", decision)
	}
}

func TestApprovalGate_ModeManual(t *testing.T) {
	t.Parallel()
	gate := NewApprovalGate(ModeManual, []string{"read_file", "glob"})

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
	gate := NewApprovalGate(ModeManual, []string{"read_file"})

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

func TestApprovalGate_ConcurrentSetMode(t *testing.T) {
	t.Parallel()
	gate := NewApprovalGate(ModeAuto, []string{"read_file"})

	// Race detector will catch issues here.
	go gate.SetMode(ModeYolo)
	go gate.SetMode(ModeManual)

	gate.ShouldAutoApprove(api.ToolCall{Name: "read_file"})
}
