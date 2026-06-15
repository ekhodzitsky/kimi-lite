// fake_guard is a minimal MCP server used by integration tests. It speaks
// line-delimited JSON-RPC over stdin/stdout, ignoring the optional config
// file argument.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func main() {
	dec := json.NewDecoder(os.Stdin)
	enc := json.NewEncoder(os.Stdout)

	for {
		var req request
		if err := dec.Decode(&req); err != nil {
			if err == io.EOF {
				return
			}
			fmt.Fprintf(os.Stderr, "decode: %v\n", err)
			return
		}

		if req.ID == nil {
			// Notification: no response required.
			continue
		}

		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": "2024-11-05",
				"serverInfo": map[string]any{
					"name":    "fake-guard",
					"version": "0.0.1",
				},
				"capabilities": map[string]any{},
			}
		case "tools/list":
			result = map[string]any{
				"tools": []map[string]any{
					{
						"name":        "fake_echo",
						"description": "Echoes the input",
						"inputSchema": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"message": map[string]any{"type": "string"},
							},
						},
					},
				},
			}
		case "tools/call":
			result = map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": "pong"},
				},
			}
		default:
			if err := enc.Encode(response{
				JSONRPC: "2.0",
				ID:      *req.ID,
				Error: &jsonRPCError{
					Code:    -32601,
					Message: "method not found: " + req.Method,
				},
			}); err != nil {
				fmt.Fprintf(os.Stderr, "encode: %v\n", err)
				return
			}
			continue
		}

		resultBytes, err := json.Marshal(result)
		if err != nil {
			fmt.Fprintf(os.Stderr, "marshal: %v\n", err)
			return
		}

		if err := enc.Encode(response{
			JSONRPC: "2.0",
			ID:      *req.ID,
			Result:  resultBytes,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "encode: %v\n", err)
			return
		}
	}
}
