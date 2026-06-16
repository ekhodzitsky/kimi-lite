// Package api provides public types for kimi-lite.
// These types are used across all internal packages and may be consumed
// by external tools.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
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

// ContentPartType identifies the kind of content in a multi-modal message.
type ContentPartType string

const (
	// ContentPartText is plain text content.
	ContentPartText ContentPartType = "text"
	// ContentPartImageURL is an image referenced by URL or data URL.
	ContentPartImageURL ContentPartType = "image_url"
)

// ImageURL describes an image for a content part.
type ImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

// ContentPart represents a single part of a multi-modal message.
type ContentPart struct {
	Type     ContentPartType `json:"type"`
	Text     string          `json:"text,omitempty"`
	ImageURL *ImageURL       `json:"image_url,omitempty"`
}

// Message represents a single message in a conversation.
type Message struct {
	ID           string        `json:"id"`
	Role         Role          `json:"role"`
	Content      string        `json:"content"`
	ContentParts []ContentPart `json:"content_parts,omitempty"`
	ToolCallID   string        `json:"tool_call_id,omitempty"`
	ToolCalls    []ToolCall    `json:"tool_calls,omitempty"`
	FinishReason string        `json:"finish_reason,omitempty"`
	CreatedAt    time.Time     `json:"created_at"`
}

// ToolCall represents a single tool invocation requested by the LLM.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON-encoded arguments
}

// ToolResult represents the result of executing a tool call.
type ToolResult struct {
	CallID       string        `json:"call_id"`
	Name         string        `json:"name"`
	Output       string        `json:"output"`
	Error        string        `json:"error,omitempty"`
	ContentParts []ContentPart `json:"content_parts,omitempty"`
}

// ToolAnnotations carries optional behavioral metadata about a tool.
type ToolAnnotations struct {
	ReadOnlyHint bool `json:"readOnlyHint,omitempty"`
}

// ToolDefinition describes a tool available to the LLM.
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"` // JSON Schema
	Annotations ToolAnnotations `json:"annotations,omitempty"`
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
	return []byte(strconv.Quote(s.String())), nil
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
	parsed := TurnState(num)
	if parsed < TurnIdle || parsed > TurnError {
		return fmt.Errorf("invalid legacy turn state: %d", num)
	}
	*s = parsed
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
	// TurnEventApprovalDiff carries a diff preview for a pending tool call.
	TurnEventApprovalDiff
	// TurnEventStatus carries a transient status message for the TUI.
	TurnEventStatus
)

// TurnEvent is emitted by TurnManager.RunTurn to report streaming progress.
type TurnEvent struct {
	Type        TurnEventType
	Content     string
	Error       error
	ToolCalls   []ToolCall
	RequestID   int64      // only used for TurnEventApprovalRequest
	Result      ToolResult // only used for TurnEventToolResult
	DiffCallID  string     // only used for TurnEventApprovalDiff
	DiffContent string     // only used for TurnEventApprovalDiff
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
	ListAllSessions(ctx context.Context, limit int) ([]Session, error)
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
	// IsReadOnly reports whether the named tool is read-only and therefore
	// safe to auto-approve in ModeAuto without user confirmation.
	IsReadOnly(name string) bool
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
	// ApprovalDiff requests a diff preview before deciding.
	ApprovalDiff
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
	case ApprovalDiff:
		return "diff"
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
	// Branch returns the current branch name, or "HEAD" when detached.
	Branch(ctx context.Context) (string, error)
}

// ConfigProvider provides access to application configuration.
type ConfigProvider interface {
	Get() *Config
}

// Config holds the complete application configuration.
type Config struct {
	LLM             LLMConfig                  `mapstructure:"llm"`
	Behavior        BehaviorConfig             `mapstructure:"behavior"`
	Permission      PermissionConfig           `mapstructure:"permission"`
	Session         SessionConfig              `mapstructure:"session"`
	MCP             MCPConfig                  `mapstructure:"mcp"`
	WebSearch       WebSearchConfig            `mapstructure:"web_search"`
	UI              UIConfig                   `mapstructure:"ui"`
	Keybindings     KeybindingConfig           `mapstructure:"keybindings"`
	Hooks           []HookConfig               `mapstructure:"hooks"`
	MCPServers      map[string]MCPServerConfig `mapstructure:"mcp_servers"`
	Providers       map[string]ProviderConfig  `mapstructure:"providers"`
	Models          map[string]ModelAlias      `mapstructure:"models"`
	DefaultProvider string                     `mapstructure:"default_provider"`
	DefaultModel    string                     `mapstructure:"default_model"`
	GitTimeout      time.Duration              `mapstructure:"git_timeout"`
	PprofAddr       string                     `mapstructure:"pprof_addr"`
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

// ProviderType identifies the protocol adapter for an LLM provider.
type ProviderType string

const (
	// ProviderTypeOpenAI is the OpenAI chat completions API.
	ProviderTypeOpenAI ProviderType = "openai"
	// ProviderTypeAnthropic is the Anthropic Messages API.
	ProviderTypeAnthropic ProviderType = "anthropic"
	// ProviderTypeKimi is the Moonshot/Kimi OpenAI-compatible API.
	ProviderTypeKimi ProviderType = "kimi"
	// ProviderTypeGoogleGenAI is the Google GenAI API.
	ProviderTypeGoogleGenAI ProviderType = "google-genai"
	// ProviderTypeOpenAIResponses is the OpenAI Responses API.
	ProviderTypeOpenAIResponses ProviderType = "openai_responses"
	// ProviderTypeVertexAI is the Google Vertex AI API.
	ProviderTypeVertexAI ProviderType = "vertexai"
)

// ProviderConfig holds configuration for a single LLM provider.
type ProviderConfig struct {
	Type          ProviderType      `mapstructure:"type"`
	APIKey        string            `json:"-" mapstructure:"api_key"`
	BaseURL       string            `mapstructure:"base_url"`
	DefaultModel  string            `mapstructure:"default_model"`
	CustomHeaders map[string]string `mapstructure:"custom_headers"`
	Env           map[string]string `mapstructure:"env"`
}

// ModelAlias maps a short alias to a concrete provider/model pair.
type ModelAlias struct {
	Provider       string   `mapstructure:"provider"`
	Model          string   `mapstructure:"model"`
	MaxContextSize int      `mapstructure:"max_context_size"`
	MaxOutputSize  int      `mapstructure:"max_output_size"`
	Capabilities   []string `mapstructure:"capabilities"`
	DisplayName    string   `mapstructure:"display_name"`
	ReasoningKey   string   `mapstructure:"reasoning_key"`
}

// BehaviorConfig holds behavior settings.
type BehaviorConfig struct {
	AutoApprove       []string      `mapstructure:"auto_approve"`
	ShellTimeout      time.Duration `mapstructure:"shell_timeout"`
	MaxTurns          int           `mapstructure:"max_turns"`
	MaxToolRounds     int           `mapstructure:"max_tool_rounds"`
	AllowShell        bool          `mapstructure:"allow_shell"`
	CompactKeepRecent int           `mapstructure:"compact_keep_recent"`
	PassEnv           bool          `mapstructure:"pass_env"`
	Skills            []string      `mapstructure:"skills"`
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
	Rules         []PermissionRule `mapstructure:"rules"`
	RiskThreshold RiskLevel        `mapstructure:"risk_threshold"`
	RiskRules     []RiskRule       `mapstructure:"risk_rules"`
}

// RiskLevel describes the risk of a tool call.
type RiskLevel string

// Risk level values.
const (
	RiskLevelLow    RiskLevel = "low"
	RiskLevelMedium RiskLevel = "medium"
	RiskLevelHigh   RiskLevel = "high"
)

// Valid reports whether the risk level is one of the known values.
func (r RiskLevel) Valid() bool {
	switch r {
	case RiskLevelLow, RiskLevelMedium, RiskLevelHigh:
		return true
	}
	return false
}

// RiskRule overrides the default risk level for a matching tool call.
type RiskRule struct {
	Tool    string    `mapstructure:"tool" json:"tool"`
	Path    string    `mapstructure:"path" json:"path,omitempty"`
	Level   RiskLevel `mapstructure:"level" json:"level"`
	Message string    `mapstructure:"message" json:"message,omitempty"`
}

// SessionConfig holds session persistence settings.
type SessionConfig struct {
	DBPath     string `mapstructure:"db_path"`
	MaxHistory int    `mapstructure:"max_history"`
}

// MCPTransport identifies the transport protocol for an MCP server.
type MCPTransport string

// MCPTransport values.
const (
	// MCPTransportStdio uses a local subprocess over stdin/stdout.
	MCPTransportStdio MCPTransport = "stdio"
	// MCPTransportHTTP uses JSON-RPC over HTTP POST.
	MCPTransportHTTP MCPTransport = "http"
	// MCPTransportSSE uses JSON-RPC over Server-Sent Events.
	MCPTransportSSE MCPTransport = "sse"
)

// MCPServerConfig holds direct configuration for a single MCP server.
type MCPServerConfig struct {
	// Common fields.
	Enabled          bool         `mapstructure:"enabled"`
	Transport        MCPTransport `mapstructure:"transport"`
	StartupTimeoutMs int          `mapstructure:"startup_timeout_ms"`
	ToolTimeoutMs    int          `mapstructure:"tool_timeout_ms"`
	EnabledTools     []string     `mapstructure:"enabled_tools"`
	DisabledTools    []string     `mapstructure:"disabled_tools"`

	// Stdio transport fields.
	Command string            `mapstructure:"command"`
	Args    []string          `mapstructure:"args"`
	Env     map[string]string `mapstructure:"env"`
	CWD     string            `mapstructure:"cwd"`

	// HTTP transport fields.
	URL               string            `mapstructure:"url"`
	Headers           map[string]string `mapstructure:"headers"`
	BearerTokenEnvVar string            `mapstructure:"bearer_token_env_var"`
}

// MCPConfig holds MCP integration settings.
type MCPConfig struct {
	GuardCommand string `mapstructure:"guard_command"`
	GuardConfig  string `mapstructure:"guard_config"`
}

// WebSearchConfig holds web search provider settings.
type WebSearchConfig struct {
	Endpoint string        `mapstructure:"endpoint"`
	APIKey   string        `json:"-" mapstructure:"api_key"`
	Timeout  time.Duration `mapstructure:"timeout"`
}

// WebSearchResult represents a single web search result.
type WebSearchResult struct {
	Title   string `json:"title"`
	Date    string `json:"date,omitempty"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
	Content string `json:"content,omitempty"`
}

// WebSearchOptions controls the behavior of a web search query.
type WebSearchOptions struct {
	Limit          int
	IncludeContent bool
}

// WebSearcher performs web searches on behalf of the built-in web_search tool.
type WebSearcher interface {
	// Search runs a web search for the given query and returns matching results.
	Search(ctx context.Context, query string, opts WebSearchOptions) ([]WebSearchResult, error)
}

// UIConfig holds UI settings.
type UIConfig struct {
	Theme          string `mapstructure:"theme"`
	ShowTokenCount bool   `mapstructure:"show_token_count"`
	Editor         string `mapstructure:"editor"`
}

// SubagentType identifies a built-in subagent.
type SubagentType string

// Built-in subagent types.
const (
	SubagentCoder   SubagentType = "coder"
	SubagentExplore SubagentType = "explore"
	SubagentPlan    SubagentType = "plan"
)

// SubagentRequest describes a single subagent invocation.
type SubagentRequest struct {
	Type         SubagentType  `json:"type"`
	Prompt       string        `json:"prompt"`
	Timeout      time.Duration `json:"timeout"`
	SandboxRoot  string        `json:"sandbox_root"`
	AllowedTools []string      `json:"allowed_tools,omitempty"`
	MaxRounds    int           `json:"max_rounds"`
}

// SubagentResult is the outcome of a subagent run.
type SubagentResult struct {
	Output   string        `json:"output"`
	Error    string        `json:"error,omitempty"`
	Rounds   int           `json:"rounds"`
	Duration time.Duration `json:"duration"`
}

// SubagentRunner executes subagent requests.
type SubagentRunner interface {
	// Run executes a subagent and returns its result.
	Run(ctx context.Context, req SubagentRequest) (*SubagentResult, error)
}

// HookEvent identifies a point in the agent lifecycle where a hook may run.
type HookEvent string

// Hook events.
const (
	HookSessionStart     HookEvent = "session_start"
	HookSessionEnd       HookEvent = "session_end"
	HookTurnStart        HookEvent = "turn_start"
	HookTurnEnd          HookEvent = "turn_end"
	HookToolCall         HookEvent = "tool_call"
	HookToolResult       HookEvent = "tool_result"
	HookApprovalRequest  HookEvent = "approval_request"
	HookApprovalDecision HookEvent = "approval_decision"
	HookTurnInterrupt    HookEvent = "turn_interrupt"
)

// String returns the string representation of the hook event.
func (e HookEvent) String() string { return string(e) }

// HookConfig configures a single lifecycle hook.
type HookConfig struct {
	Event           HookEvent         `mapstructure:"event"`
	Command         string            `mapstructure:"command"`
	Args            []string          `mapstructure:"args"`
	Env             map[string]string `mapstructure:"env"`
	Timeout         time.Duration     `mapstructure:"timeout"`
	ContinueOnError bool              `mapstructure:"continue_on_error"`
}

// HookData is the payload passed to a hook command.
type HookData struct {
	Event      HookEvent
	SessionID  string
	TurnID     string
	ToolName   string
	ToolArgs   string
	ToolResult string
	Decision   string
	Error      string
}

// HookRunner executes lifecycle hooks for a given event.
type HookRunner interface {
	Run(ctx context.Context, data HookData) error
}

// MetricsCollector collects counters and latency observations.
type MetricsCollector interface {
	// IncCounter increments a counter metric.
	IncCounter(name string, tags ...string)
	// RecordLatency records a latency observation.
	RecordLatency(name string, d time.Duration, tags ...string)
	// RecordError increments an error counter.
	RecordError(name string)
}

// NoopMetricsCollector is a MetricsCollector that discards all observations.
type NoopMetricsCollector struct{}

// IncCounter does nothing.
func (NoopMetricsCollector) IncCounter(name string, _ ...string) { _ = name }

// RecordLatency does nothing.
func (NoopMetricsCollector) RecordLatency(name string, d time.Duration, _ ...string) {
	_ = name
	_ = d
}

// RecordError does nothing.
func (NoopMetricsCollector) RecordError(name string) { _ = name }

// TokenEstimator estimates the number of tokens consumed by a message list.
type TokenEstimator interface {
	Estimate(messages []Message) int
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
	ApproveDiff    string `mapstructure:"approve_diff"`
	ExternalEditor string `mapstructure:"external_editor"`
}
