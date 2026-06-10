// Package api provides public types for kimi-lite.
// These types are used across all internal packages and may be consumed
// by external tools or future ACP (Agent Communication Protocol) implementations.
package api

import (
	"context"
	"encoding/json"
	"time"
)

// Role represents the role of a message sender.
type Role string

const (
	// RoleSystem is the system role.
	RoleSystem Role = "system"
	// RoleUser is the user role.
	RoleUser Role = "user"
	// RoleAssistant is the assistant role.
	RoleAssistant Role = "assistant"
	// RoleTool is the tool role.
	RoleTool Role = "tool"
)

// Message represents a single message in a conversation.
type Message struct {
	ID         string     `json:"id"`
	Role       Role       `json:"role"`
	Content    string     `json:"content"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// ToolCall represents a single tool invocation requested by the LLM.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON-encoded arguments
}

// ToolResult represents the result of executing a tool call.
type ToolResult struct {
	CallID string `json:"call_id"`
	Name   string `json:"name"`
	Output string `json:"output"`
	Error  string `json:"error,omitempty"`
}

// ToolDefinition describes a tool available to the LLM.
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"` // JSON Schema
}

// Session represents a conversation session.
type Session struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Path      string    `json:"path"` // working directory
	Messages  []Message `json:"messages"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TurnState represents the current state of a single turn.
type TurnState int

const (
	// TurnIdle indicates no turn is in progress.
	TurnIdle TurnState = iota
	// TurnThinking indicates the LLM is generating a response.
	TurnThinking
	// TurnStreaming indicates the response is being streamed.
	TurnStreaming
	// TurnToolCalls indicates tool calls are being executed.
	TurnToolCalls
	// TurnWaitingApproval indicates waiting for user approval.
	TurnWaitingApproval
	// TurnError indicates a turn error occurred.
	TurnError
)

// String returns the human-readable name of the turn state.
func (s TurnState) String() string {
	switch s {
	case TurnIdle:
		return "idle"
	case TurnThinking:
		return "thinking"
	case TurnStreaming:
		return "streaming"
	case TurnToolCalls:
		return "tool_calls"
	case TurnWaitingApproval:
		return "waiting_approval"
	case TurnError:
		return "error"
	default:
		return "unknown"
	}
}

// ParseTurnState parses a turn state from its string representation.
func ParseTurnState(s string) TurnState {
	switch s {
	case "idle":
		return TurnIdle
	case "thinking":
		return TurnThinking
	case "streaming":
		return TurnStreaming
	case "tool_calls":
		return TurnToolCalls
	case "waiting_approval":
		return TurnWaitingApproval
	case "error":
		return TurnError
	default:
		return TurnIdle
	}
}

// Turn represents a single user input → LLM response cycle.
type Turn struct {
	ID        string       `json:"id"`
	State     TurnState    `json:"state"`
	Input     string       `json:"input"`
	Response  string       `json:"response"`
	ToolCalls []ToolCall   `json:"tool_calls"`
	Results   []ToolResult `json:"results"`
	Error     string       `json:"error,omitempty"`
	StartedAt time.Time    `json:"started_at"`
	EndedAt   *time.Time   `json:"ended_at,omitempty"`
}

// LLMClient is the interface for LLM API interactions.
type LLMClient interface {
	// Chat sends messages and returns the complete response.
	Chat(ctx context.Context, messages []Message, tools []ToolDefinition) (*Message, error)
	// ChatStream sends messages and streams the response.
	ChatStream(ctx context.Context, messages []Message, tools []ToolDefinition) (<-chan StreamChunk, error)
	// Models returns available model configurations.
	Models() []ModelInfo
}

// StreamChunk represents a single chunk from a streaming LLM response.
type StreamChunk struct {
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	Done      bool       `json:"done"`
	Error     error      `json:"-"`
}

// ModelInfo describes a configured LLM model.
type ModelInfo struct {
	Name          string `json:"name"`
	Provider      string `json:"provider"`
	MaxTokens     int    `json:"max_tokens"`
	ContextWindow int    `json:"context_window"`
}

// SessionStore is the interface for session persistence.
type SessionStore interface {
	CreateSession(ctx context.Context, path string) (*Session, error)
	GetSession(ctx context.Context, id string) (*Session, error)
	GetLastSession(ctx context.Context, path string) (*Session, error)
	ListSessions(ctx context.Context, path string, limit int) ([]Session, error)
	UpdateSession(ctx context.Context, session *Session) error
	DeleteSession(ctx context.Context, id string) error
}

// MessageStore is the interface for message persistence.
type MessageStore interface {
	AppendMessage(ctx context.Context, sessionID string, msg Message) error
	GetMessages(ctx context.Context, sessionID string, limit int) ([]Message, error)
	ClearMessages(ctx context.Context, sessionID string) error
	ReplaceMessages(ctx context.Context, sessionID string, msgs []Message) error
}

// TurnStore is the interface for turn persistence.
type TurnStore interface {
	SaveTurn(ctx context.Context, sessionID string, turn Turn) error
	GetTurns(ctx context.Context, sessionID string, limit int) ([]Turn, error)
}

// Store is the composite interface for all persistence operations.
type Store interface {
	SessionStore
	MessageStore
	TurnStore
	// Close closes the store.
	Close() error
}

// ToolExecutor is the interface for executing tools.
type ToolExecutor interface {
	// Execute runs a tool call and returns the result.
	Execute(ctx context.Context, call ToolCall) (ToolResult, error)
	// Definitions returns all available tool definitions.
	Definitions() []ToolDefinition
	// IsReadOnly returns true if the tool does not modify state.
	IsReadOnly(name string) bool
}

// ApprovalDecision represents the user's decision on a tool call.
type ApprovalDecision int

const (
	// ApprovalYes approves the tool call.
	ApprovalYes ApprovalDecision = iota
	// ApprovalNo rejects the tool call.
	ApprovalNo
	// ApprovalAlways always approves this tool.
	ApprovalAlways
	// ApprovalDiff requests a diff preview (unimplemented; treated as ApprovalNo).
	ApprovalDiff
)

// ApprovalGate decides whether a tool call requires user approval.
type ApprovalGate interface {
	// ShouldApprove returns the auto-approval decision for a tool call.
	// If the tool requires manual approval, returns (ApprovalNo, false).
	ShouldAutoApprove(call ToolCall) (ApprovalDecision, bool)
}

// MCPClient is the interface for MCP (Model Context Protocol) interactions.
type MCPClient interface {
	// Connect establishes connection to mcp-guard.
	Connect(ctx context.Context) error
	// ListTools returns available MCP tools.
	ListTools(ctx context.Context) ([]ToolDefinition, error)
	// CallTool invokes an MCP tool.
	CallTool(ctx context.Context, name string, args map[string]interface{}) (string, error)
	// Close closes the connection.
	Close() error
}

// GitProvider provides git status and diff information.
type GitProvider interface {
	// Status returns the current git status as a string.
	Status(ctx context.Context) (string, error)
	// Diff returns the diff for a specific file.
	Diff(ctx context.Context, path string) (string, error)
	// IsRepo returns true if the current directory is a git repository.
	IsRepo(ctx context.Context) bool
}

// ConfigProvider provides access to application configuration.
type ConfigProvider interface {
	Get() *Config
}

// Config holds the complete application configuration.
type Config struct {
	LLM         LLMConfig
	Behavior    BehaviorConfig
	Session     SessionConfig
	MCP         MCPConfig
	UI          UIConfig
	Keybindings KeybindingConfig
}

// LLMConfig holds LLM provider configuration.
type LLMConfig struct {
	Provider string
	APIKey   string `json:"-"`
	Model    string
	BaseURL  string
	Timeout  time.Duration
	Fallback *LLMConfig
}

// BehaviorConfig holds behavior settings.
type BehaviorConfig struct {
	AutoApprove  []string
	ShellTimeout time.Duration
	MaxTurns     int
}

// SessionConfig holds session persistence settings.
type SessionConfig struct {
	DBPath     string
	MaxHistory int
}

// MCPConfig holds MCP integration settings.
type MCPConfig struct {
	GuardCommand string
	GuardConfig  string
}

// UIConfig holds UI settings.
type UIConfig struct {
	Theme          string
	ShowTokenCount bool
	Editor         string
}

// KeybindingConfig holds keybinding settings.
type KeybindingConfig struct {
	Send     string
	Newline  string
	Cancel   string
	Quit     string
	PlanMode string
	Yolo     string
}
