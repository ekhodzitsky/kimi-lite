package api

import (
	"encoding/json"
	"strings"
	"testing"
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
