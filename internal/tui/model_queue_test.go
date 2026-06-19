package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ekhodzitsky/kimi-lite/internal/config"
	"github.com/ekhodzitsky/kimi-lite/internal/tui/input"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func setupQueueTest(t *testing.T) *Model {
	t.Helper()
	cfg := config.DefaultConfig()
	session := &api.Session{ID: "test", Path: "/tmp"}
	m, err := New(cfg, session, context.Background(), "")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	m.width = 120
	m.height = 40
	m.updateLayout()
	return m
}

func TestQueueMessageDuringStreaming(t *testing.T) {
	t.Parallel()

	m := setupQueueTest(t)
	m.setState(api.TurnStreaming)

	updated, _ := m.Update(input.SendMsg{Content: "next message"})
	model := updated.(*Model)

	if model.state != api.TurnStreaming {
		t.Errorf("state = %d, want TurnStreaming", model.state)
	}
	if model.queueLength() != 1 {
		t.Errorf("queue length = %d, want 1", model.queueLength())
	}
}

func TestQueueMessageDuringThinking(t *testing.T) {
	t.Parallel()

	m := setupQueueTest(t)
	m.setState(api.TurnThinking)

	updated, _ := m.Update(input.SendMsg{Content: "next message"})
	model := updated.(*Model)

	if model.state != api.TurnThinking {
		t.Errorf("state = %d, want TurnThinking", model.state)
	}
	if model.queueLength() != 1 {
		t.Errorf("queue length = %d, want 1", model.queueLength())
	}
}

func TestAutoSendOnStreamDone(t *testing.T) {
	t.Parallel()

	m := setupQueueTest(t)
	m.setState(api.TurnStreaming)
	m.input.SetValue("queued")

	m.Update(input.SendMsg{Content: "queued"})
	if m.queueLength() != 1 {
		t.Fatalf("queue length = %d, want 1", m.queueLength())
	}

	updated, cmd := m.Update(StreamChunkMsg{Chunk: api.StreamChunk{Done: true}})
	model := updated.(*Model)

	if model.state != api.TurnIdle {
		t.Errorf("state = %d, want TurnIdle", model.state)
	}
	if model.queueLength() != 0 {
		t.Errorf("queue length = %d, want 0", model.queueLength())
	}
	if cmd == nil {
		t.Fatal("expected auto-send command after stream done")
	}

	msg := cmd()
	sendMsg, ok := msg.(input.SendMsg)
	if !ok {
		t.Fatalf("expected input.SendMsg, got %T", msg)
	}
	if sendMsg.Content != "queued" {
		t.Errorf("auto-send content = %q, want %q", sendMsg.Content, "queued")
	}
}

func TestMultipleQueuedMessagesFIFO(t *testing.T) {
	t.Parallel()

	m := setupQueueTest(t)
	m.setState(api.TurnStreaming)

	m.Update(input.SendMsg{Content: "first"})
	m.Update(input.SendMsg{Content: "second"})

	if m.queueLength() != 2 {
		t.Fatalf("queue length = %d, want 2", m.queueLength())
	}

	updated, cmd := m.Update(StreamChunkMsg{Chunk: api.StreamChunk{Done: true}})
	model := updated.(*Model)

	if cmd == nil {
		t.Fatal("expected auto-send command")
	}
	sendMsg := cmd().(input.SendMsg)
	if sendMsg.Content != "first" {
		t.Errorf("first auto-send = %q, want %q", sendMsg.Content, "first")
	}

	// Simulate the queued turn running and completing.
	model.setState(api.TurnStreaming)
	updated2, cmd2 := model.Update(StreamChunkMsg{Chunk: api.StreamChunk{Done: true}})
	model2 := updated2.(*Model)

	if model2.queueLength() != 0 {
		t.Errorf("queue length = %d, want 0", model2.queueLength())
	}
	if cmd2 == nil {
		t.Fatal("expected second auto-send command")
	}
	sendMsg2 := cmd2().(input.SendMsg)
	if sendMsg2.Content != "second" {
		t.Errorf("second auto-send = %q, want %q", sendMsg2.Content, "second")
	}
}

func TestCancelStreamPreservesQueue(t *testing.T) {
	t.Parallel()

	m := setupQueueTest(t)
	m.setState(api.TurnStreaming)
	m.streamCancel = func() {}
	m.input.SetValue("draft")

	m.Update(input.SendMsg{Content: "queued"})
	if m.queueLength() != 1 {
		t.Fatalf("queue length = %d, want 1", m.queueLength())
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	model := updated.(*Model)

	if model.state != api.TurnIdle {
		t.Errorf("state = %d, want TurnIdle after cancel", model.state)
	}
	if model.queueLength() != 1 {
		t.Errorf("queue length = %d, want 1 after cancel", model.queueLength())
	}
	// Draft should remain editable after cancellation.
	if model.input.Value() != "draft" {
		t.Errorf("input value = %q, want %q", model.input.Value(), "draft")
	}
}

func TestCancelThenClearDraft(t *testing.T) {
	t.Parallel()

	m := setupQueueTest(t)
	m.setState(api.TurnStreaming)
	m.streamCancel = func() {}
	m.input.SetValue("draft")

	// First Ctrl+C cancels the stream.
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	model := updated.(*Model)
	if model.state != api.TurnIdle {
		t.Fatalf("state = %d, want TurnIdle", model.state)
	}

	// Second Ctrl+C clears the draft.
	updated2, _ := model.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	model2 := updated2.(*Model)
	if model2.input.Value() != "" {
		t.Errorf("input value = %q, want empty after second ctrl+c", model2.input.Value())
	}
}

func TestQuitWhenIdleAndEmpty(t *testing.T) {
	t.Parallel()

	m := setupQueueTest(t)
	m.setState(api.TurnIdle)

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("expected quit command when idle and empty")
	}
	found := containsQuitMsg(t, cmd())
	if !found {
		t.Errorf("expected tea.QuitMsg in command output, got %T", cmd())
	}
}

func containsQuitMsg(t *testing.T, msg tea.Msg) bool {
	t.Helper()
	switch m := msg.(type) {
	case tea.QuitMsg:
		return true
	case tea.BatchMsg:
		for _, c := range m {
			if c == nil {
				continue
			}
			if containsQuitMsg(t, c()) {
				return true
			}
		}
	}
	return false
}

func TestQueueSurvivesCompact(t *testing.T) {
	t.Parallel()

	m := setupQueueTest(t)
	m.setState(api.TurnStreaming)
	m.Update(input.SendMsg{Content: "queued"})

	m.Update(CompactResultMsg{Count: 2})

	if m.queueLength() != 1 {
		t.Errorf("queue length = %d, want 1 after compact", m.queueLength())
	}
}

func TestQueueSurvivesClear(t *testing.T) {
	t.Parallel()

	m := setupQueueTest(t)
	m.setState(api.TurnStreaming)
	m.Update(input.SendMsg{Content: "queued"})

	m.clearMessages()

	if m.queueLength() != 1 {
		t.Errorf("queue length = %d, want 1 after clear", m.queueLength())
	}
}

func TestQueueSurvivesSessionSwitch(t *testing.T) {
	t.Parallel()

	m := setupQueueTest(t)
	m.setState(api.TurnStreaming)
	m.Update(input.SendMsg{Content: "queued"})

	newSession := &api.Session{ID: "new", Path: "/home"}
	m.Update(SessionSelectedMsg{Session: newSession})

	if m.queueLength() != 1 {
		t.Errorf("queue length = %d, want 1 after session switch", m.queueLength())
	}
}
