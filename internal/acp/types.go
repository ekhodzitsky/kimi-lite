// Package acp implements an Agent Client Protocol (ACP) server speaking
// JSON-RPC 2.0 over stdio.
package acp

import (
	"encoding/json"
)

// JSON-RPC 2.0 base types.

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type jsonRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      any           `json:"id"`
	Result  any           `json:"result,omitempty"`
	Error   *jsonRPCError `json:"error,omitempty"`
}

type jsonRPCNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// ACP-specific request/notification params and results.

// initializeParams is sent by the client during the handshake.
type initializeParams struct {
	ProtocolVersion int `json:"protocolVersion"`
}

// initializeResult is returned by the server during the handshake.
type initializeResult struct {
	ProtocolVersion int                  `json:"protocolVersion"`
	Capabilities    initializeCapability `json:"capabilities"`
	ServerInfo      serverInfo           `json:"serverInfo"`
}

type initializeCapability struct {
	SingleSession bool `json:"singleSession"`
	Streaming     bool `json:"streaming"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// sessionNewParams creates a new session in the requested working directory.
type sessionNewParams struct {
	WorkingDir string `json:"workingDir,omitempty"`
}

// sessionResult is returned by session/new and session/load.
type sessionResult struct {
	SessionID  string `json:"sessionId"`
	WorkingDir string `json:"workingDir"`
}

// sessionLoadParams resumes an existing session.
type sessionLoadParams struct {
	SessionID string `json:"sessionId"`
}

// sessionPromptParams sends a user prompt to the active session.
type sessionPromptParams struct {
	Prompt string `json:"prompt"`
}

// sessionPromptResult is returned once the turn finishes.
type sessionPromptResult struct {
	Response string `json:"response"`
}

// sessionCancelResult is returned by session/cancel.
type sessionCancelResult struct {
	Cancelled bool `json:"cancelled"`
}

// sessionUpdateType identifies the kind of session/update notification.
type sessionUpdateType string

const (
	sessionUpdateAgentMessageChunk sessionUpdateType = "agent_message_chunk"
	sessionUpdateToolResult        sessionUpdateType = "tool_result"
	sessionUpdateApprovalRequest   sessionUpdateType = "approval_request"
	sessionUpdateApprovalDiff      sessionUpdateType = "approval_diff"
)

// sessionUpdateParams is the payload of a session/update notification.
type sessionUpdateParams struct {
	SessionUpdate string `json:"sessionUpdate"`
	Content       string `json:"content,omitempty"`
	ToolResult    any    `json:"toolResult,omitempty"`
	Approval      any    `json:"approval,omitempty"`
	DiffCallID    string `json:"diffCallId,omitempty"`
	DiffContent   string `json:"diffContent,omitempty"`
}
