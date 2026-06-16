package mcp

import (
	"encoding/json"
	"fmt"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// normalizeToolParameters adapts a JSON Schema from an MCP server to the
// stricter "Moonshot flavored JSON Schema" used by the Kimi API. It:
//   - ensures every property (and nested property) defines a scalar type;
//   - converts type arrays such as ["string", "null"] to a scalar type;
//   - removes type from schemas that use anyOf/oneOf and ensures each branch
//     has its own type;
//   - strips "null" as a type.
func normalizeToolParameters(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return raw, nil
	}
	var schema any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, fmt.Errorf("unmarshal schema: %w", err)
	}
	schema = normalizeSchema(schema, true)
	out, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("marshal normalized schema: %w", err)
	}
	return out, nil
}

// normalizeToolDefinitions applies schema normalization to every tool definition.
func normalizeToolDefinitions(defs []api.ToolDefinition) ([]api.ToolDefinition, error) {
	out := make([]api.ToolDefinition, len(defs))
	for i, d := range defs {
		params, err := normalizeToolParameters(d.Parameters)
		if err != nil {
			return nil, fmt.Errorf("normalize %s parameters: %w", d.Name, err)
		}
		out[i] = d
		out[i].Parameters = params
	}
	return out, nil
}

// normalizeSchema recursively normalizes a JSON Schema value.
func normalizeSchema(v any, requireType bool) any {
	switch x := v.(type) {
	case map[string]any:
		return normalizeObjectSchema(x, requireType)
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = normalizeSchema(item, requireType)
		}
		return out
	default:
		return v
	}
}

func normalizeObjectSchema(schema map[string]any, requireType bool) map[string]any {
	out := make(map[string]any, len(schema))
	for k, v := range schema {
		out[k] = v
	}

	// Recursively normalize nested schemas.
	for _, key := range []string{"properties", "patternProperties"} {
		if m, ok := out[key].(map[string]any); ok {
			normalized := make(map[string]any, len(m))
			for prop, pv := range m {
				normalized[prop] = normalizeSchema(pv, true)
			}
			out[key] = normalized
		}
	}
	if items, ok := out["items"]; ok {
		out["items"] = normalizeSchema(items, true)
	}
	if additional, ok := out["additionalProperties"]; ok {
		// additionalProperties can be a boolean or a schema.
		if _, isBool := additional.(bool); !isBool {
			out["additionalProperties"] = normalizeSchema(additional, true)
		}
	}
	for _, key := range []string{"anyOf", "oneOf", "allOf"} {
		if arr, ok := out[key].([]any); ok {
			normalized := make([]any, len(arr))
			for i, item := range arr {
				normalized[i] = normalizeSchema(item, true)
			}
			out[key] = normalized
		}
	}

	// Kimi rejects a parent type combined with anyOf/oneOf.
	if hasAnyOfOrOneOf(out) {
		delete(out, "type")
	}

	// Normalize the type field itself.
	if t, ok := out["type"]; ok {
		out["type"] = normalizeType(t)
	}

	// Ensure type is present when required.
	if requireType {
		if _, ok := out["type"]; !ok && !hasAnyOfOrOneOf(out) {
			out["type"] = inferType(out)
		}
	}

	return out
}

func hasAnyOfOrOneOf(schema map[string]any) bool {
	_, hasAny := schema["anyOf"]
	_, hasOne := schema["oneOf"]
	return hasAny || hasOne
}

func normalizeType(t any) string {
	arr, ok := t.([]any)
	if !ok {
		s, ok := t.(string)
		if ok && s != "null" {
			return s
		}
		// Unknown scalar or "null" -> default to string.
		return "string"
	}
	// Prefer object/array over scalar to preserve structure, then the first
	// non-null scalar type.
	var firstScalar string
	for _, item := range arr {
		s, ok := item.(string)
		if !ok {
			continue
		}
		switch s {
		case "object", "array":
			return s
		case "null":
			continue
		default:
			if firstScalar == "" {
				firstScalar = s
			}
		}
	}
	if firstScalar != "" {
		return firstScalar
	}
	return "string"
}

func inferType(schema map[string]any) string {
	if _, ok := schema["properties"]; ok {
		return "object"
	}
	if _, ok := schema["additionalProperties"]; ok {
		return "object"
	}
	if _, ok := schema["items"]; ok {
		return "array"
	}
	if _, ok := schema["enum"]; ok {
		return "string"
	}
	return "string"
}
