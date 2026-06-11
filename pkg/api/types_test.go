package api

import (
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
