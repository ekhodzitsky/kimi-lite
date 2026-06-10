package app

import (
	"testing"

	"github.com/ekhodzitsky/kimi-lite/internal/core"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func TestApp_SetYolo(t *testing.T) {
	t.Parallel()

	app := &App{
		approvalGate: core.NewApprovalGate(core.ModeAuto, []string{"read_file"}),
	}

	call := api.ToolCall{Name: "write_file"}
	decision, auto := app.approvalGate.ShouldAutoApprove(call)
	if auto || decision != api.ApprovalNo {
		t.Fatal("expected manual approval for write_file in auto mode")
	}

	app.SetYolo(true)
	decision, auto = app.approvalGate.ShouldAutoApprove(call)
	if !auto || decision != api.ApprovalYes {
		t.Fatal("expected auto-approval for write_file in yolo mode")
	}
}

func TestApp_SetAutoApprove(t *testing.T) {
	t.Parallel()

	app := &App{
		approvalGate: core.NewApprovalGate(core.ModeManual, []string{"read_file"}),
	}

	call := api.ToolCall{Name: "read_file"}
	_, auto := app.approvalGate.ShouldAutoApprove(call)
	if auto {
		t.Fatal("expected no auto-approval in manual mode")
	}

	app.SetAutoApprove(true)
	_, auto = app.approvalGate.ShouldAutoApprove(call)
	if !auto {
		t.Fatal("expected auto-approval after switching to auto mode")
	}
}

func TestConfigProvider_Get(t *testing.T) {
	t.Parallel()

	cfg := &api.Config{LLM: api.LLMConfig{Model: "test-model"}}
	p := &configProvider{cfg: cfg}

	if got := p.Get(); got != cfg {
		t.Fatal("expected same config pointer")
	}
}
