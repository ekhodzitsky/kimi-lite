package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestParseTurnState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    TurnState
		wantErr bool
	}{
		{"idle", "idle", TurnIdle, false},
		{"thinking", "thinking", TurnThinking, false},
		{"streaming", "streaming", TurnStreaming, false},
		{"tool_calls", "tool_calls", TurnToolCalls, false},
		{"waiting_approval", "waiting_approval", TurnWaitingApproval, false},
		{"error", "error", TurnError, false},
		{"empty", "", TurnIdle, true},
		{"unknown", "bogus", TurnIdle, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseTurnState(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseTurnState(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ParseTurnState(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestTurnState_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state TurnState
		want  string
	}{
		{TurnIdle, "idle"},
		{TurnThinking, "thinking"},
		{TurnStreaming, "streaming"},
		{TurnToolCalls, "tool_calls"},
		{TurnWaitingApproval, "waiting_approval"},
		{TurnError, "error"},
		{TurnState(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			if got := tt.state.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTurnState_ShortString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state TurnState
		want  string
	}{
		{TurnIdle, "idle"},
		{TurnThinking, "thinking"},
		{TurnStreaming, "streaming"},
		{TurnToolCalls, "tools"},
		{TurnWaitingApproval, "approval"},
		{TurnError, "error"},
		{TurnState(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			if got := tt.state.ShortString(); got != tt.want {
				t.Errorf("ShortString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTurnState_MarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state TurnState
		want  string
	}{
		{TurnIdle, `"idle"`},
		{TurnThinking, `"thinking"`},
		{TurnStreaming, `"streaming"`},
		{TurnToolCalls, `"tool_calls"`},
		{TurnWaitingApproval, `"waiting_approval"`},
		{TurnError, `"error"`},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			got, err := json.Marshal(tt.state)
			if err != nil {
				t.Fatalf("MarshalJSON() error = %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("MarshalJSON() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestTurnState_UnmarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    TurnState
		wantErr bool
	}{
		{"idle", `"idle"`, TurnIdle, false},
		{"thinking", `"thinking"`, TurnThinking, false},
		{"streaming", `"streaming"`, TurnStreaming, false},
		{"tool_calls", `"tool_calls"`, TurnToolCalls, false},
		{"waiting_approval", `"waiting_approval"`, TurnWaitingApproval, false},
		{"error", `"error"`, TurnError, false},
		{"legacy integer", `3`, TurnToolCalls, false},
		{"legacy integer idle", `0`, TurnIdle, false},
		{"invalid integer", `99`, TurnIdle, true},
		{"negative integer", `-1`, TurnIdle, true},
		{"unknown string", `"bogus"`, TurnIdle, true},
		{"invalid", `true`, TurnIdle, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got TurnState
			err := json.Unmarshal([]byte(tt.input), &got)
			if (err != nil) != tt.wantErr {
				t.Fatalf("UnmarshalJSON(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("UnmarshalJSON(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestTurnState_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	turn := Turn{
		ID:    "turn-1",
		State: TurnToolCalls,
		Input: "test input",
	}
	data, err := json.Marshal(turn)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if !strings.Contains(string(data), `"state":"tool_calls"`) {
		t.Errorf("expected state to be serialized as string, got %s", string(data))
	}

	var parsed Turn
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if parsed.State != TurnToolCalls {
		t.Errorf("parsed.State = %v, want %v", parsed.State, TurnToolCalls)
	}
}

func TestApprovalDecision_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		decision ApprovalDecision
		want     string
	}{
		{ApprovalNo, "no"},
		{ApprovalYes, "yes"},
		{ApprovalAlways, "always"},
		{ApprovalDiff, "diff"},
		{ApprovalDecision(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			if got := tt.decision.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTurn_OmitEmpty(t *testing.T) {
	t.Parallel()

	turn := Turn{
		ID:    "turn-1",
		State: TurnIdle,
		Input: "test",
	}
	data, err := json.Marshal(turn)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if strings.Contains(string(data), `"tool_calls"`) {
		t.Error("expected empty ToolCalls to be omitted")
	}
	if strings.Contains(string(data), `"results"`) {
		t.Error("expected empty Results to be omitted")
	}
}

func TestSession_OmitEmptyMessages(t *testing.T) {
	t.Parallel()

	sess := Session{
		ID:   "sess-1",
		Name: "test",
		Path: "/tmp",
	}
	data, err := json.Marshal(sess)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if strings.Contains(string(data), `"messages"`) {
		t.Error("expected empty Messages to be omitted")
	}
}

func TestSessionExport_VersionConstant(t *testing.T) {
	t.Parallel()

	export := SessionExport{
		Version: SessionExportVersion,
	}
	data, err := json.Marshal(export)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if !strings.Contains(string(data), `"version":"1.0"`) {
		t.Errorf("expected version 1.0 in export JSON, got %s", string(data))
	}
}

func TestNoopMetricsCollector(t *testing.T) {
	t.Parallel()

	var c MetricsCollector = NoopMetricsCollector{}
	// All no-op methods must be safe to call with any arguments.
	c.IncCounter("requests", "tool:read_file")
	c.IncCounter("requests") // no tags
	c.RecordLatency("llm", time.Millisecond, "provider:openai")
	c.RecordLatency("llm", 0) // zero duration
	c.RecordError("timeout")
}

func TestHookEvent_String(t *testing.T) {
	t.Parallel()
	if HookToolCall.String() != "tool_call" {
		t.Fatalf("unexpected string: %q", HookToolCall.String())
	}
	if HookTurnInterrupt.String() != "turn_interrupt" {
		t.Fatalf("unexpected string: %q", HookTurnInterrupt.String())
	}
}

func TestSubagentResult_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	original := SubagentResult{
		Output:   "the answer",
		Error:    "",
		Rounds:   3,
		Duration: time.Second,
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var parsed SubagentResult
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if parsed.Output != original.Output {
		t.Errorf("Output = %q, want %q", parsed.Output, original.Output)
	}
	if parsed.Rounds != original.Rounds {
		t.Errorf("Rounds = %d, want %d", parsed.Rounds, original.Rounds)
	}
	if parsed.Duration != original.Duration {
		t.Errorf("Duration = %v, want %v", parsed.Duration, original.Duration)
	}
}

func TestRiskLevel_Valid(t *testing.T) {
	t.Parallel()

	for _, lvl := range []RiskLevel{RiskLevelLow, RiskLevelMedium, RiskLevelHigh} {
		if !lvl.Valid() {
			t.Errorf("expected %q to be valid", lvl)
		}
	}
	if (RiskLevel("extreme")).Valid() {
		t.Error("expected invalid level to be false")
	}
}

func TestRiskRule_RoundTrip(t *testing.T) {
	t.Parallel()

	rule := RiskRule{
		Tool:    "write_file",
		Path:    "*.go",
		Level:   RiskLevelMedium,
		Message: "writing Go source",
	}
	data, err := json.Marshal(rule)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got RiskRule
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got != rule {
		t.Errorf("round-trip failed: %+v != %+v", got, rule)
	}
}

func TestSecretFields_ExcludedFromJSON(t *testing.T) {
	t.Parallel()

	llm := LLMConfig{
		Provider: "openai",
		APIKey:   "secret-key",
		Model:    "gpt-4",
	}
	data, err := json.Marshal(llm)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "secret-key") {
		t.Errorf("LLMConfig.APIKey with json:\"-\" leaked into JSON: %s", data)
	}

	provider := ProviderConfig{
		Type:         ProviderTypeOpenAI,
		APIKey:       "provider-secret",
		BaseURL:      "https://api.openai.com/v1",
		DefaultModel: "gpt-4",
	}
	data, err = json.Marshal(provider)
	if err != nil {
		t.Fatalf("marshal provider: %v", err)
	}
	if strings.Contains(string(data), "provider-secret") {
		t.Errorf("ProviderConfig.APIKey with json:\"-\" leaked into JSON: %s", data)
	}

	web := WebSearchConfig{
		Endpoint: "https://search.example.com",
		APIKey:   "web-secret",
	}
	data, err = json.Marshal(web)
	if err != nil {
		t.Fatalf("marshal web search: %v", err)
	}
	if strings.Contains(string(data), "web-secret") {
		t.Errorf("WebSearchConfig.APIKey with json:\"-\" leaked into JSON: %s", data)
	}
}

func TestSessionExport_RoundTrip(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	export := SessionExport{
		Version:    SessionExportVersion,
		ExportedAt: now,
		Session: Session{
			ID:        "sess-1",
			Name:      "test session",
			Path:      "/tmp",
			CreatedAt: now,
			UpdatedAt: now,
		},
		Messages: []Message{
			{
				ID:        "msg-1",
				Role:      RoleUser,
				Content:   "hello",
				CreatedAt: now,
			},
		},
		Turns: []Turn{
			{
				ID:        "turn-1",
				State:     TurnThinking,
				Input:     "hello",
				Response:  "hi",
				StartedAt: now,
			},
		},
	}

	data, err := json.Marshal(export)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"version":"1.0"`) {
		t.Errorf("missing version in export JSON: %s", data)
	}

	var parsed SessionExport
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed.Version != export.Version {
		t.Errorf("Version = %q, want %q", parsed.Version, export.Version)
	}
	if parsed.Session.ID != export.Session.ID {
		t.Errorf("Session.ID = %q, want %q", parsed.Session.ID, export.Session.ID)
	}
	if len(parsed.Messages) != len(export.Messages) {
		t.Errorf("len(Messages) = %d, want %d", len(parsed.Messages), len(export.Messages))
	}
	if len(parsed.Turns) != len(export.Turns) {
		t.Errorf("len(Turns) = %d, want %d", len(parsed.Turns), len(export.Turns))
	}
	if parsed.Turns[0].State != export.Turns[0].State {
		t.Errorf("Turns[0].State = %v, want %v", parsed.Turns[0].State, export.Turns[0].State)
	}
}
