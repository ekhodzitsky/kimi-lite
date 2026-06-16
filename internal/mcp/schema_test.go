package mcp

import (
	"encoding/json"
	"testing"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func TestNormalizeToolParameters(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "already valid",
			in:   `{"type":"object","properties":{"path":{"type":"string"}}}`,
			want: `{"type":"object","properties":{"path":{"type":"string"}}}`,
		},
		{
			name: "fills missing type",
			in:   `{"type":"object","properties":{"mode":{"description":"mode"}}}`,
			want: `{"properties":{"mode":{"type":"string","description":"mode"}},"type":"object"}`,
		},
		{
			name: "type array with null",
			in:   `{"type":"object","properties":{"reason":{"type":["string","null"]}}}`,
			want: `{"type":"object","properties":{"reason":{"type":"string"}}}`,
		},
		{
			name: "infers object from properties",
			in:   `{"type":"object","properties":{"nested":{"properties":{"a":{"type":"integer"}}}}}`,
			want: `{"type":"object","properties":{"nested":{"properties":{"a":{"type":"integer"}},"type":"object"}}}`,
		},
		{
			name: "infers array from items",
			in:   `{"type":"object","properties":{"items":{"items":{"type":"string"}}}}`,
			want: `{"type":"object","properties":{"items":{"items":{"type":"string"},"type":"array"}}}`,
		},
		{
			name: "removes parent type with anyOf and types branches",
			in:   `{"type":"object","properties":{"format":{"type":"string","anyOf":[{"type":"string"},{"description":"x"}]}}}`,
			want: `{"type":"object","properties":{"format":{"anyOf":[{"type":"string"},{"type":"string","description":"x"}]}}}`,
		},
		{
			name: "strips null type",
			in:   `{"type":"object","properties":{"x":{"type":"null"}}}`,
			want: `{"type":"object","properties":{"x":{"type":"string"}}}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeToolParameters(json.RawMessage(tc.in))
			if err != nil {
				t.Fatalf("normalizeToolParameters: %v", err)
			}
			var gotObj, wantObj any
			if err := json.Unmarshal(got, &gotObj); err != nil {
				t.Fatalf("unmarshal got: %v", err)
			}
			if err := json.Unmarshal([]byte(tc.want), &wantObj); err != nil {
				t.Fatalf("unmarshal want: %v", err)
			}
			if !jsonEqual(gotObj, wantObj) {
				t.Errorf("got %s, want %s", string(got), tc.want)
			}
		})
	}
}

func TestNormalizeToolDefinitions(t *testing.T) {
	t.Parallel()
	defs := []api.ToolDefinition{
		{Name: "t1", Parameters: json.RawMessage(`{"type":"object","properties":{"x":{"type":["string","null"]}}}`)},
	}
	got, err := normalizeToolDefinitions(defs)
	if err != nil {
		t.Fatalf("normalizeToolDefinitions: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 def, got %d", len(got))
	}
	var gotObj, wantObj any
	if err := json.Unmarshal(got[0].Parameters, &gotObj); err != nil {
		t.Fatalf("unmarshal got: %v", err)
	}
	if err := json.Unmarshal([]byte(`{"type":"object","properties":{"x":{"type":"string"}}}`), &wantObj); err != nil {
		t.Fatalf("unmarshal want: %v", err)
	}
	if !jsonEqual(gotObj, wantObj) {
		t.Errorf("parameters = %s, want %s", got[0].Parameters, `{"type":"object","properties":{"x":{"type":"string"}}}`)
	}
}

func jsonEqual(a, b any) bool {
	ab, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bb, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(ab) == string(bb)
}
