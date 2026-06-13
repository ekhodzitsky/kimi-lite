package subagents

import "github.com/ekhodzitsky/kimi-lite/pkg/api"

// filterDefinitions returns the definitions whose names are in allowed.
// If allowed is empty, all definitions are returned.
func filterDefinitions(all []api.ToolDefinition, allowed []string) []api.ToolDefinition {
	if len(allowed) == 0 {
		return all
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
