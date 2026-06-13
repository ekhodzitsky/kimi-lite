package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// StreamReader parses an SSE (Server-Sent Events) stream into chunks.
type StreamReader struct {
	scanner *bufio.Scanner
	reader  io.ReadCloser
}

// NewStreamReader creates a new StreamReader from an io.ReadCloser.
func NewStreamReader(rc io.ReadCloser) *StreamReader {
	scanner := bufio.NewScanner(rc)
	// Raise the per-token limit from the default 64 KB to 1 MB so that
	// large SSE JSON payloads (e.g. tool-call definitions) do not cause
	// bufio.ErrTooLong.
	const maxTokenSize = 1024 * 1024
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, maxTokenSize)
	return &StreamReader{
		scanner: scanner,
		reader:  rc,
	}
}

// Close closes the underlying reader.
func (r *StreamReader) Close() error {
	return r.reader.Close()
}

// readRawChunk reads the next raw SSE event. It is unexported so that
// client.go can access the index field for incremental tool-call accumulation.
// The caller is responsible for ensuring the context can unblock the underlying
// reader (e.g. by closing the body on cancellation).
//
// Quirk: OpenAI-style streams may signal the end of the stream twice: once
// through a payload with a non-empty finish_reason, and again through the
// "[DONE]" sentinel. Both cases set Done=true, so consumers must tolerate
// duplicate terminal markers.
func (r *StreamReader) readRawChunk(_ context.Context) (rawChunk, error) {
	var data strings.Builder

	for r.scanner.Scan() {
		line := r.scanner.Text()

		// Empty line marks the end of an event.
		if line == "" {
			if data.Len() > 0 {
				break
			}
			continue
		}

		// Skip comment/heartbeat lines.
		if strings.HasPrefix(line, ":") {
			continue
		}

		// Parse field: "data: ..." (space after colon is optional per SSE spec).
		if strings.HasPrefix(line, "data:") {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			value := strings.TrimPrefix(line, "data:")
			// Strip optional single leading space, but preserve any additional spaces.
			if len(value) > 0 && value[0] == ' ' {
				value = value[1:]
			}
			data.WriteString(value)
			continue
		}
		// Ignore other fields like event:, id:, retry:
	}

	if err := r.scanner.Err(); err != nil {
		return rawChunk{}, fmt.Errorf("read sse stream: %w", err)
	}

	if data.Len() == 0 {
		return rawChunk{}, io.EOF
	}

	dataStr := data.String()

	if strings.TrimSpace(dataStr) == "[DONE]" {
		return rawChunk{Done: true}, io.EOF
	}

	var payload streamPayload
	if err := json.Unmarshal([]byte(dataStr), &payload); err != nil {
		return rawChunk{}, fmt.Errorf("parse sse payload: %w", err)
	}

	if len(payload.Choices) == 0 {
		return rawChunk{}, nil
	}

	delta := payload.Choices[0].Delta
	chunk := rawChunk{
		Content:      delta.Content,
		Done:         payload.Choices[0].FinishReason != "",
		FinishReason: payload.Choices[0].FinishReason,
	}

	if len(delta.ToolCalls) > 0 {
		chunk.ToolCalls = make([]rawToolCall, 0, len(delta.ToolCalls))
		for _, tc := range delta.ToolCalls {
			chunk.ToolCalls = append(chunk.ToolCalls, rawToolCall{
				Index:     tc.Index,
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			})
		}
	}

	return chunk, nil
}

// rawChunk is the internal representation of a streaming chunk before
// conversion to the public api.StreamChunk type.
type rawChunk struct {
	Content      string
	ToolCalls    []rawToolCall
	Done         bool
	FinishReason string
}

type rawToolCall struct {
	Index     int
	ID        string
	Name      string
	Arguments string
}

// streamPayload represents the JSON structure of a streaming chunk.
type streamPayload struct {
	Choices []streamChoice `json:"choices"`
}

type streamChoice struct {
	Delta        streamDelta `json:"delta"`
	FinishReason string      `json:"finish_reason"`
}

type streamDelta struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolCalls []streamToolCall `json:"tool_calls,omitempty"`
}

type streamToolCall struct {
	Index    int            `json:"index"`
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Function streamFunction `json:"function"`
}

type streamFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}
