package subagents

import "github.com/ekhodzitsky/kimi-lite/pkg/api"

// filterDefinitions returns the definitions whose names are in allowed.
// If allowed is nil, all definitions are returned. An explicitly empty
// allowlist returns an empty slice.
func filterDefinitions(all []api.ToolDefinition, allowed []string) []api.ToolDefinition {
	if allowed == nil {
		return append([]api.ToolDefinition(nil), all...)
	}
	want := make(map[string]struct{}, len(allowed))
	for _, n := range allowed {
		want[n] = struct{}{}
	}
	out := make([]api.ToolDefinition, 0, len(allowed))
	for _, d := range all {
		if _, ok := want[d.Name]; ok {
			out = append(out, d)
		}
	}
	return out
}
