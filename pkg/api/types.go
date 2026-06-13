// Package api provides public types for kimi-lite.
// These types are used across all internal packages and may be consumed
// by external tools.
package api

import (
	"context"
	"encoding/json"
	"fmt"
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
	ID           string     `json:"id"`
	Role         Role       `json:"role"`
	Content      string     `json:"content"`
	ToolCallID   string     `json:"tool_call_id,omitempty"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	FinishReason string     `json:"finish_reason,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
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
	Messages  []Message `json:"messages,omitempty"`
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

// ShortString returns an abbreviated display label for the turn state.
func (s TurnState) ShortString() string {
	switch s {
	case TurnIdle:
		return "idle"
	case TurnThinking:
		return "thinking"
	case TurnStreaming:
		return "streaming"
	case TurnToolCalls:
		return "tools"
	case TurnWaitingApproval:
		return "approval"
	case TurnError:
		return "error"
	default:
		return "unknown"
	}
}

// ParseTurnState parses a turn state from its string representation.
// Returns an error for unrecognized state strings instead of silently
// defaulting to TurnIdle.
func ParseTurnState(s string) (TurnState, error) {
	switch s {
	case "idle":
		return TurnIdle, nil
	case "thinking":
		return TurnThinking, nil
	case "streaming":
		return TurnStreaming, nil
	case "tool_calls":
		return TurnToolCalls, nil
	case "waiting_approval":
		return TurnWaitingApproval, nil
	case "error":
		return TurnError, nil
	default:
		return TurnIdle, fmt.Errorf("unknown turn state: %q", s)
	}
}

// MarshalJSON returns the JSON-quoted string representation of the turn state.
func (s TurnState) MarshalJSON() ([]byte, error) {
	b, err := json.Marshal(s.String())
	if err != nil {
		return nil, fmt.Errorf("marshal turn state: %w", err)
	}
	return b, nil
}

// UnmarshalJSON parses a TurnState from its JSON string representation.
// It also accepts legacy integer values for backward compatibility.
func (s *TurnState) UnmarshalJSON(b []byte) error {
	var str string
	if err := json.Unmarshal(b, &str); err == nil {
		parsed, err := ParseTurnState(str)
		if err != nil {
			return err
		}
		*s = parsed
		return nil
	}

	// Fallback to legacy integer unmarshaling.
	var num int
	if err := json.Unmarshal(b, &num); err != nil {
		return fmt.Errorf("invalid turn state: %w", err)
	}
	*s = TurnState(num)
	return nil
}

// Turn represents a single user input → LLM response cycle.
type Turn struct {
	ID        string       `json:"id"`
	State     TurnState    `json:"state"`
	Input     string       `json:"input"`
	Response  string       `json:"response"`
	ToolCalls []ToolCall   `json:"tool_calls,omitempty"`
	Results   []ToolResult `json:"results,omitempty"`
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
// It is an in-process streaming message type; Error is deliberately
// excluded from JSON and JSON/ACP consumers must not rely on it to
// detect stream failure.
type StreamChunk struct {
	Content      string     `json:"content"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	Done         bool       `json:"done"`
	FinishReason string     `json:"finish_reason,omitempty"`
	// Error is an in-process field excluded from JSON serialization.
	// External consumers should detect failure via other means.
	Error error `json:"-"`
}

// TurnEventType identifies the kind of event emitted during a turn.
type TurnEventType int

const (
	// TurnEventContent carries a text fragment from the LLM stream.
	TurnEventContent TurnEventType = iota
	// TurnEventDone signals that the turn has completed.
	TurnEventDone
	// TurnEventError signals that the turn failed.
	TurnEventError
	// TurnEventApprovalRequest signals that manual approval is required for pending tool calls.
	TurnEventApprovalRequest
	// TurnEventToolResult signals that a tool call has produced a result.
	TurnEventToolResult
)

// TurnEvent is emitted by TurnManager.RunTurn to report streaming progress.
type TurnEvent struct {
	Type      TurnEventType
	Content   string
	Error     error
	ToolCalls []ToolCall
	RequestID int64      // only used for TurnEventApprovalRequest
	Result    ToolResult // only used for TurnEventToolResult
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
	CountTurns(ctx context.Context, sessionID string, state TurnState) (int, error)
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
	Definitions(ctx context.Context) []ToolDefinition
}

// ApprovalDecision represents the user's decision on a tool call.
type ApprovalDecision int

const (
	// ApprovalNo rejects the tool call. It is the zero value so that
	// uninitialized ApprovalDecision defaults to the safest choice.
	ApprovalNo ApprovalDecision = iota
	// ApprovalYes approves the tool call.
	ApprovalYes
	// ApprovalAlways always approves this tool.
	ApprovalAlways
)

// String returns the human-readable name of the approval decision.
func (d ApprovalDecision) String() string {
	switch d {
	case ApprovalNo:
		return "no"
	case ApprovalYes:
		return "yes"
	case ApprovalAlways:
		return "always"
	default:
		return "unknown"
	}
}

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
	CallTool(ctx context.Context, name string, args map[string]any) (string, error)
	// Close closes the connection.
	Close() error
}

// GitProvider provides git status and diff information.
type GitProvider interface {
	// Status returns the current git status as a string.
	Status(ctx context.Context) (string, error)
	// Diff returns the diff for a specific file.
	Diff(ctx context.Context, path string) (string, error)
	// Commit creates a git commit with the given message.
	Commit(ctx context.Context, message string) error
	// IsRepo returns true if the current directory is inside a git work tree.
	// A genuine non-repository returns (false, nil); execution errors return
	// a non-nil error so callers can distinguish them from "not a repo".
	IsRepo(ctx context.Context) (bool, error)
}

// ConfigProvider provides access to application configuration.
type ConfigProvider interface {
	Get() *Config
}

// Config holds the complete application configuration.
type Config struct {
	LLM         LLMConfig        `mapstructure:"llm"`
	Behavior    BehaviorConfig   `mapstructure:"behavior"`
	Permission  PermissionConfig `mapstructure:"permission"`
	Session     SessionConfig    `mapstructure:"session"`
	MCP         MCPConfig        `mapstructure:"mcp"`
	UI          UIConfig         `mapstructure:"ui"`
	Keybindings KeybindingConfig `mapstructure:"keybindings"`
}

// LLMConfig holds LLM provider configuration.
type LLMConfig struct {
	Provider string        `mapstructure:"provider"`
	APIKey   string        `json:"-" mapstructure:"api_key"`
	Model    string        `mapstructure:"model"`
	BaseURL  string        `mapstructure:"base_url"`
	Timeout  time.Duration `mapstructure:"timeout"`
	Fallback *LLMConfig    `mapstructure:"fallback"`
}

// BehaviorConfig holds behavior settings.
type BehaviorConfig struct {
	AutoApprove       []string      `mapstructure:"auto_approve"`
	ShellTimeout      time.Duration `mapstructure:"shell_timeout"`
	MaxTurns          int           `mapstructure:"max_turns"`
	MaxToolRounds     int           `mapstructure:"max_tool_rounds"`
	AllowShell        bool          `mapstructure:"allow_shell"`
	CompactKeepRecent int           `mapstructure:"compact_keep_recent"`
}

// PermissionDecision is the action a permission rule takes.
type PermissionDecision string

// Permission decision values.
const (
	PermissionAllow PermissionDecision = "allow"
	PermissionDeny  PermissionDecision = "deny"
	PermissionAsk   PermissionDecision = "ask"
)

// PermissionScope defines how long a permission rule remains in effect.
type PermissionScope string

// Permission scope values.
const (
	PermissionScopeUser    PermissionScope = "user"
	PermissionScopeSession PermissionScope = "session"
	PermissionScopeTurn    PermissionScope = "turn"
)

// PermissionRule configures a single tool permission.
type PermissionRule struct {
	Tool     string             `mapstructure:"tool"`
	Decision PermissionDecision `mapstructure:"decision"`
	Scope    PermissionScope    `mapstructure:"scope"`
}

// PermissionConfig holds the permission rule list.
type PermissionConfig struct {
	Rules []PermissionRule `mapstructure:"rules"`
}

// SessionConfig holds session persistence settings.
type SessionConfig struct {
	DBPath     string `mapstructure:"db_path"`
	MaxHistory int    `mapstructure:"max_history"`
}

// MCPConfig holds MCP integration settings.
type MCPConfig struct {
	GuardCommand string `mapstructure:"guard_command"`
	GuardConfig  string `mapstructure:"guard_config"`
}

// UIConfig holds UI settings.
type UIConfig struct {
	Theme          string `mapstructure:"theme"`
	ShowTokenCount bool   `mapstructure:"show_token_count"`
	Editor         string `mapstructure:"editor"`
}

// KeybindingConfig holds keybinding settings.
type KeybindingConfig struct {
	Send           string `mapstructure:"send"`
	Newline        string `mapstructure:"newline"`
	Cancel         string `mapstructure:"cancel"`
	Quit           string `mapstructure:"quit"`
	Yolo           string `mapstructure:"yolo"`
	ToggleSidebar  string `mapstructure:"toggle_sidebar"`
	FocusNext      string `mapstructure:"focus_next"`
	FocusPrev      string `mapstructure:"focus_prev"`
	ApproveYes     string `mapstructure:"approve_yes"`
	ApproveNo      string `mapstructure:"approve_no"`
	ApproveAlways  string `mapstructure:"approve_always"`
	ExternalEditor string `mapstructure:"external_editor"`
}
