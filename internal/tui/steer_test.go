package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ekhodzitsky/kimi-lite/internal/config"
	"github.com/ekhodzitsky/kimi-lite/internal/tui/input"
	msgcomp "github.com/ekhodzitsky/kimi-lite/internal/tui/messages"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

type recordingSteerTurnManager struct {
	steerCalled bool
	steerInput  string
}

func (r *recordingSteerTurnManager) RunTurn(ctx context.Context, sessionID string, input string) (<-chan api.TurnEvent, error) {
	return nil, nil
}

func (r *recordingSteerTurnManager) RunTurnWithContentParts(ctx context.Context, sessionID string, input string, parts []api.ContentPart) (<-chan api.TurnEvent, error) {
	return nil, nil
}

func (r *recordingSteerTurnManager) RunTurnWithPlan(ctx context.Context, sessionID string, input string) (<-chan api.TurnEvent, error) {
	return nil, nil
}

func (r *recordingSteerTurnManager) RunTurnWithPlanWithContentParts(ctx context.Context, sessionID string, input string, parts []api.ContentPart) (<-chan api.TurnEvent, error) {
	return nil, nil
}

func (r *recordingSteerTurnManager) ResumeWithPlan(ctx context.Context, sessionID string, approved bool) error {
	return nil
}

func (r *recordingSteerTurnManager) ResumeWithApproval(ctx context.Context, sessionID string, requestID int64, approvals map[string]api.ApprovalDecision) error {
	return nil
}

func (r *recordingSteerTurnManager) Steer(ctx context.Context, sessionID string, input string) error {
	r.steerCalled = true
	r.steerInput = input
	return nil
}

func TestSteerKey_OpensOverlayDuringStreaming(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()
	m.setState(api.TurnStreaming)

	updated, _ := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	model := updated.(*Model)

	if !model.steerOpen {
		t.Error("expected steer overlay to open")
	}
}

func TestSteerKey_IgnoredWhenIdle(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()

	updated, _ := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	model := updated.(*Model)

	if model.steerOpen {
		t.Error("expected steer overlay to stay closed when idle")
	}
}

func TestSteerOverlay_SubmitSendsSteerMsg(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()
	m.setState(api.TurnStreaming)
	m.steerOpen = true

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	updated, _ = updated.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	updated, cmd := updated.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model := updated.(*Model)

	if model.steerOpen {
		t.Error("expected steer overlay to close after submit")
	}
	if model.steerInput != "" {
		t.Errorf("steerInput = %q, want empty", model.steerInput)
	}
	if cmd == nil {
		t.Fatal("expected command after submit")
	}
	if !findSteerMsg(cmd) {
		t.Fatalf("expected SteerMsg in command output, got %T", cmd())
	}
}

func findSteerMsg(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	msg := cmd()
	if _, ok := msg.(SteerMsg); ok {
		return true
	}
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return false
	}
	for _, c := range batch {
		if c == nil {
			continue
		}
		if _, ok := c().(SteerMsg); ok {
			return true
		}
	}
	return false
}

func TestSteerOverlay_CancelClosesOverlay(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()
	m.setState(api.TurnStreaming)
	m.steerOpen = true
	m.steerInput = "draft"

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model := updated.(*Model)

	if model.steerOpen {
		t.Error("expected steer overlay to close after cancel")
	}
	if model.steerInput != "" {
		t.Errorf("steerInput = %q, want empty", model.steerInput)
	}
	if cmd != nil {
		t.Error("expected no command after cancel")
	}
}

func TestHandleSteer_ForwardsToTurnManager(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()

	rtm := &recordingSteerTurnManager{}
	m.SetTurnManager(rtm)

	updated, cmd := m.Update(SteerMsg{Content: "be concise"})
	model := updated.(*Model)

	if cmd == nil {
		t.Fatal("expected command for SteerMsg")
	}
	msg := cmd()
	if _, ok := msg.(StateChangeMsg); !ok {
		t.Fatalf("expected StateChangeMsg, got %T", msg)
	}
	if !rtm.steerCalled {
		t.Fatal("expected Steer to be called on turn manager")
	}
	if rtm.steerInput != "be concise" {
		t.Errorf("steer input = %q, want %q", rtm.steerInput, "be concise")
	}
	_ = model
}

func TestSteeredMsg_FinalizesAssistantAndStartsNewBlock(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, _ := New(cfg, session, context.Background(), "")
	m.width = 120
	m.height = 40
	m.updateLayout()

	m.Update(input.SendMsg{Content: "hello"})
	m.Update(StreamChunkMsg{Chunk: api.StreamChunk{Content: "partial"}})

	updated, _ := m.Update(SteeredMsg{Content: "expand"})
	model := updated.(*Model)

	if !model.steeredPending {
		t.Error("expected steeredPending to be true")
	}
	if len(model.messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(model.messages))
	}
	if model.messages[0].Type != msgcomp.TypeUser || model.messages[0].Content != "hello" {
		t.Errorf("messages[0] = %+v, want user hello", model.messages[0])
	}
	if model.messages[1].Type != msgcomp.TypeAssistant || model.messages[1].Content != "partial" {
		t.Errorf("messages[1] = %+v, want assistant partial", model.messages[1])
	}
	if model.messages[2].Type != msgcomp.TypeUser || model.messages[2].Content != "expand" {
		t.Errorf("messages[2] = %+v, want user expand", model.messages[2])
	}

	updated2, _ := model.Update(StreamChunkMsg{Chunk: api.StreamChunk{Content: " continued"}})
	model2 := updated2.(*Model)

	if model2.steeredPending {
		t.Error("expected steeredPending to be reset")
	}
	if len(model2.messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(model2.messages))
	}
	if model2.messages[0].Content != "hello" {
		t.Errorf("messages[0] = %q, want hello", model2.messages[0].Content)
	}
	if model2.messages[1].Content != "partial" {
		t.Errorf("messages[1] = %q, want partial", model2.messages[1].Content)
	}
	if model2.messages[2].Content != "expand" {
		t.Errorf("messages[2] = %q, want expand", model2.messages[2].Content)
	}
	if model2.messages[3].Type != msgcomp.TypeAssistant {
		t.Errorf("messages[3].Type = %v, want assistant", model2.messages[3].Type)
	}
	if model2.messages[3].Content != " continued" {
		t.Errorf("messages[3].Content = %q, want ' continued'", model2.messages[3].Content)
	}
}
