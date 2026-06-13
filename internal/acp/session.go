package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

const (
	currentACPVersion = 1
	serverName        = "kimi-lite"
)

// handleInitialize processes the ACP handshake.
func (s *Server) handleInitialize(ctx context.Context, req jsonRPCRequest, enc *json.Encoder) error {
	var params initializeParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.writeError(ctx, enc, req.ID, -32602, "invalid params", err)
		}
	}

	if params.ProtocolVersion != 0 && params.ProtocolVersion != currentACPVersion {
		return s.writeError(ctx, enc, req.ID, -32602, "unsupported protocol version", fmt.Errorf("want %d", currentACPVersion))
	}

	result := initializeResult{
		ProtocolVersion: currentACPVersion,
		Capabilities: initializeCapability{
			SingleSession: true,
			Streaming:     true,
		},
		ServerInfo: serverInfo{
			Name:    serverName,
			Version: "dev",
		},
	}
	return s.writeResult(ctx, enc, req.ID, result)
}

// handleSessionNew creates a new session.
func (s *Server) handleSessionNew(ctx context.Context, req jsonRPCRequest, enc *json.Encoder) error {
	var params sessionNewParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.writeError(ctx, enc, req.ID, -32602, "invalid params", err)
		}
	}

	if err := changeWorkingDir(params.WorkingDir); err != nil {
		return s.writeError(ctx, enc, req.ID, -32603, "invalid working directory", err)
	}

	sess, err := s.app.StartSession(ctx)
	if err != nil {
		return s.writeError(ctx, enc, req.ID, -32603, "failed to create session", err)
	}

	s.setSession(sess)
	return s.writeResult(ctx, enc, req.ID, sessionResult{
		SessionID:  sess.ID,
		WorkingDir: sess.Path,
	})
}

// handleSessionLoad resumes an existing session.
func (s *Server) handleSessionLoad(ctx context.Context, req jsonRPCRequest, enc *json.Encoder) error {
	var params sessionLoadParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.writeError(ctx, enc, req.ID, -32602, "invalid params", err)
		}
	}
	if params.SessionID == "" {
		return s.writeError(ctx, enc, req.ID, -32602, "invalid params", errors.New("sessionId is required"))
	}

	sess, err := s.app.ResumeSession(ctx, params.SessionID)
	if err != nil {
		return s.writeError(ctx, enc, req.ID, -32603, "failed to load session", err)
	}

	s.setSession(sess)
	return s.writeResult(ctx, enc, req.ID, sessionResult{
		SessionID:  sess.ID,
		WorkingDir: sess.Path,
	})
}
