package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
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

// ReadChunk reads and parses the next api.StreamChunk from the SSE stream.
// It returns io.EOF when the stream ends normally. The returned chunk may
// contain valid data even when err is io.EOF (e.g. a final [DONE] event).
func (r *StreamReader) ReadChunk(ctx context.Context) (api.StreamChunk, error) {
	if err := ctx.Err(); err != nil {
		return api.StreamChunk{}, err
	}
	raw, err := r.readRawChunk(ctx)
	if err != nil && err != io.EOF {
		return api.StreamChunk{}, err
	}
	if err := ctx.Err(); err != nil {
		return api.StreamChunk{}, err
	}

	chunk := api.StreamChunk{
		Content: raw.Content,
		Done:    raw.Done,
	}

	if len(raw.ToolCalls) > 0 {
		chunk.ToolCalls = make([]api.ToolCall, 0, len(raw.ToolCalls))
		for _, tc := range raw.ToolCalls {
			chunk.ToolCalls = append(chunk.ToolCalls, api.ToolCall{
				ID:        tc.ID,
				Name:      tc.Name,
				Arguments: tc.Arguments,
			})
		}
	}

	return chunk, err
}

// readRawChunk reads the next raw SSE event. It is unexported so that
// client.go can access the index field for incremental tool-call accumulation.
// The caller is responsible for ensuring the context can unblock the underlying
// reader (e.g. by closing the body on cancellation).
func (r *StreamReader) readRawChunk(ctx context.Context) (rawChunk, error) {
	var data string

	for r.scanner.Scan() {
		line := r.scanner.Text()

		// Empty line marks the end of an event.
		if line == "" {
			if data != "" {
				break
			}
			continue
		}

		// Parse field: "data: ..."
		if strings.HasPrefix(line, "data: ") {
			data = strings.TrimPrefix(line, "data: ")
			continue
		}
		// Ignore other fields like event:, id:, retry:
	}

	if err := r.scanner.Err(); err != nil {
		return rawChunk{}, fmt.Errorf("read sse stream: %w", err)
	}

	if data == "" {
		return rawChunk{}, io.EOF
	}

	if strings.TrimSpace(data) == "[DONE]" {
		return rawChunk{Done: true}, io.EOF
	}

	var payload streamPayload
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
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
