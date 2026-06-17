package core

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func TestTurnManager_Steer_Validation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	tm := newTestTurnManager(t, &mockLLMClient{}, &mockToolExecutor{}, &mockApprovalGate{}, store, nil)

	if err := tm.Steer(ctx, sess.ID, "steer"); err == nil {
		t.Error("expected error when no active turn")
	}

	// Start a turn so there is an active turn.
	llm := &mockLLMClient{
		chatStreamFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			ch := make(chan api.StreamChunk)
			go func() {
				defer close(ch)
				select {
				case ch <- api.StreamChunk{Content: "hello"}:
				case <-ctx.Done():
					return
				}
				// Keep channel open until the consumer stops reading or context ends.
				ticker := time.NewTicker(10 * time.Millisecond)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						select {
						case ch <- api.StreamChunk{Content: "more"}:
						default:
							return
						}
					}
				}
			}()
			return ch, nil
		},
	}
	tm = newTestTurnManager(t, llm, &mockToolExecutor{}, &mockApprovalGate{}, store, nil)
	outCh, err := tm.RunTurn(ctx, sess.ID, "Hi")
	if err != nil {
		t.Fatalf("run turn: %v", err)
	}
	defer func() {
		for range outCh {
		}
	}()

	for i := 0; i < 100; i++ {
		turn := tm.CurrentTurn()
		if turn != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := tm.Steer(ctx, "wrong-session", "steer"); err == nil {
		t.Error("expected error for sessionID mismatch")
	}
	if err := tm.Steer(ctx, sess.ID, "   "); err == nil {
		t.Error("expected error for empty steer input")
	}

	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	if err := tm.Steer(cancelCtx, sess.ID, "steer"); !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}

	if err := tm.Steer(ctx, sess.ID, "please be concise"); err != nil {
		t.Errorf("steer failed: %v", err)
	}

	tm.CancelAll()
}

func TestTurnManager_RunTurn_Steered(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	steerSignal := make(chan struct{})
	var mu sync.Mutex
	callCount := 0
	llm := &mockLLMClient{
		chatStreamFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			mu.Lock()
			callCount++
			count := callCount
			mu.Unlock()

			ch := make(chan api.StreamChunk)
			go func() {
				defer close(ch)
				if count == 1 {
					select {
					case ch <- api.StreamChunk{Content: "Partial"}:
					case <-ctx.Done():
						return
					}
					// Wait until the test signals that steering happened.
					select {
					case <-steerSignal:
					case <-ctx.Done():
					}
					return
				}
				select {
				case ch <- api.StreamChunk{Content: " continued"}:
				case <-ctx.Done():
					return
				}
				select {
				case ch <- api.StreamChunk{Done: true}:
				case <-ctx.Done():
					return
				}
			}()
			return ch, nil
		},
	}

	tm := newTestTurnManager(t, llm, &mockToolExecutor{}, &mockApprovalGate{}, store, nil)

	outCh, err := tm.RunTurn(ctx, sess.ID, "Hi")
	if err != nil {
		t.Fatalf("run turn: %v", err)
	}

	// Wait for the first content chunk.
	var contents []string
	var steered bool
	done := make(chan struct{})
	go func() {
		for e := range outCh {
			switch e.Type {
			case api.TurnEventContent:
				mu.Lock()
				contents = append(contents, e.Content)
				mu.Unlock()
			case api.TurnEventSteered:
				mu.Lock()
				steered = true
				mu.Unlock()
			}
		}
		close(done)
	}()

	for i := 0; i < 100; i++ {
		mu.Lock()
		hasContent := len(contents) > 0
		mu.Unlock()
		if hasContent {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := tm.Steer(ctx, sess.ID, "expand on that"); err != nil {
		t.Fatalf("steer: %v", err)
	}
	close(steerSignal)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for turn to complete")
	}

	mu.Lock()
	contentStr := strings.Join(contents, "")
	steeredCopy := steered
	mu.Unlock()

	if !steeredCopy {
		t.Error("expected TurnEventSteered event")
	}
	if contentStr != "Partial continued" {
		t.Errorf("content = %q, want %q", contentStr, "Partial continued")
	}

	turn := tm.CurrentTurn()
	if turn == nil || turn.State != api.TurnIdle {
		t.Fatalf("expected turn to complete, got state %v", turn)
	}

	msgs, _ := store.GetMessages(ctx, sess.ID, 0)
	// user + assistant(partial) + user(steer) + assistant(continued)
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d: %+v", len(msgs), msgs)
	}
	if msgs[1].Role != api.RoleAssistant || msgs[1].Content != "Partial" {
		t.Errorf("msg[1] = %+v, want assistant Partial", msgs[1])
	}
	if msgs[2].Role != api.RoleUser || msgs[2].Content != "expand on that" {
		t.Errorf("msg[2] = %+v, want user steer", msgs[2])
	}
	if msgs[3].Role != api.RoleAssistant || msgs[3].Content != " continued" {
		t.Errorf("msg[3] = %+v, want assistant continued", msgs[3])
	}
}

func TestTurnManager_RunTurn_SteeredBeforeFirstChunk(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMockStore()
	sess, _ := store.CreateSession(ctx, "/tmp/proj")

	steerSignal := make(chan struct{})
	var mu sync.Mutex
	callCount := 0
	llm := &mockLLMClient{
		chatStreamFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			mu.Lock()
			callCount++
			count := callCount
			mu.Unlock()

			ch := make(chan api.StreamChunk)
			go func() {
				defer close(ch)
				if count == 1 {
					// Wait until the test signals that steering happened.
					select {
					case <-steerSignal:
					case <-ctx.Done():
					}
					return
				}
				select {
				case ch <- api.StreamChunk{Done: true}:
				case <-ctx.Done():
					return
				}
			}()
			return ch, nil
		},
	}

	tm := newTestTurnManager(t, llm, &mockToolExecutor{}, &mockApprovalGate{}, store, nil)

	outCh, err := tm.RunTurn(ctx, sess.ID, "Hi")
	if err != nil {
		t.Fatalf("run turn: %v", err)
	}

	var steered bool
	done := make(chan struct{})
	go func() {
		for e := range outCh {
			if e.Type == api.TurnEventSteered {
				steered = true
			}
		}
		close(done)
	}()

	// Give RunTurn time to start the stream.
	time.Sleep(50 * time.Millisecond)

	if err := tm.Steer(ctx, sess.ID, "be brief"); err != nil {
		t.Fatalf("steer: %v", err)
	}
	close(steerSignal)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for turn to complete")
	}

	if !steered {
		t.Error("expected TurnEventSteered event")
	}

	msgs, _ := store.GetMessages(ctx, sess.ID, 0)
	// user + user(steer); the turn ends with no assistant content because the
	// LLM never produced any chunks after steering.
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d: %+v", len(msgs), msgs)
	}
	if msgs[1].Role != api.RoleUser || msgs[1].Content != "be brief" {
		t.Errorf("msg[1] = %+v, want user steer", msgs[1])
	}
}
