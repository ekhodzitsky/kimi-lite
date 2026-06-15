package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// handleSessionPrompt runs a user prompt and streams session/update notifications.
func (s *Server) handleSessionPrompt(ctx context.Context, req jsonRPCRequest, enc *json.Encoder) error {
	var params sessionPromptParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.writeError(ctx, enc, req.ID, -32602, "invalid params", err)
		}
	}
	if params.Prompt == "" {
		return s.writeError(ctx, enc, req.ID, -32602, "invalid params", errors.New("prompt is required"))
	}

	sess, err := s.currentSession()
	if err != nil {
		return s.writeError(ctx, enc, req.ID, -32603, "no active session", err)
	}

	// Capture the cancel function before marking the prompt in-flight so a
	// concurrent session/cancel cannot observe promptInFlight without a cancel.
	promptCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.setCancel(cancel)
	defer s.setCancel(nil)

	if !s.startPrompt() {
		return s.writeError(ctx, enc, req.ID, -32603, "prompt already in flight", errors.New("a prompt is already running"))
	}
	defer s.endPrompt()

	eventCh, err := s.app.RunTurn(promptCtx, sess.ID, params.Prompt)
	if err != nil {
		return s.writeError(ctx, enc, req.ID, -32603, "failed to start turn", err)
	}

	var response string
	for event := range eventCh {
		if err := s.handleTurnEvent(ctx, enc, event); err != nil {
			return s.writeError(ctx, enc, req.ID, -32603, "turn failed", err)
		}
		if event.Type == api.TurnEventDone {
			response = event.Content
		}
	}

	return s.writeResult(ctx, enc, req.ID, sessionPromptResult{Response: response})
}

// handleTurnEvent maps an api.TurnEvent to a session/update notification or error.
func (s *Server) handleTurnEvent(ctx context.Context, enc *json.Encoder, event api.TurnEvent) error {
	switch event.Type {
	case api.TurnEventContent:
		return s.writeNotification(ctx, enc, "session/update", sessionUpdateParams{
			SessionUpdate: string(sessionUpdateAgentMessageChunk),
			Content:       event.Content,
		})

	case api.TurnEventToolResult:
		return s.writeNotification(ctx, enc, "session/update", sessionUpdateParams{
			SessionUpdate: string(sessionUpdateToolResult),
			ToolResult:    event.Result,
		})

	case api.TurnEventApprovalRequest:
		// ACP does not implement an approval-response method, so do not advertise
		// approval requests as notifications. Surface them as a turn error instead.
		return errors.New("approval requests are not supported over ACP; enable yolo or configure auto-approve")

	case api.TurnEventApprovalDiff:
		return s.writeNotification(ctx, enc, "session/update", sessionUpdateParams{
			SessionUpdate: string(sessionUpdateApprovalDiff),
			DiffCallID:    event.DiffCallID,
			DiffContent:   event.DiffContent,
		})

	case api.TurnEventDone:
		return nil

	case api.TurnEventError:
		if event.Error == nil {
			return nil
		}
		return fmt.Errorf("turn error: %w", event.Error)

	default:
		return nil
	}
}

// handleSessionCancel cancels the current prompt.
func (s *Server) handleSessionCancel(ctx context.Context, req jsonRPCRequest, enc *json.Encoder) error {
	cancelled := s.cancelCurrent()
	if req.ID != nil {
		return s.writeResult(ctx, enc, req.ID, sessionCancelResult{Cancelled: cancelled})
	}
	return nil
}
