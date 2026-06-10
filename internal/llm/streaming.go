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
	return &StreamReader{
		scanner: bufio.NewScanner(rc),
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
	raw, err := r.readRawChunk(ctx)
	if err != nil && err != io.EOF {
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
func (r *StreamReader) readRawChunk(ctx context.Context) (rawChunk, error) {
	type result struct {
		chunk rawChunk
		err   error
	}

	done := make(chan result, 1)
	go func() {
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
			done <- result{err: fmt.Errorf("read sse stream: %w", err)}
			return
		}

		if data == "" {
			done <- result{err: io.EOF}
			return
		}

		if strings.TrimSpace(data) == "[DONE]" {
			done <- result{chunk: rawChunk{Done: true}, err: io.EOF}
			return
		}

		var payload streamPayload
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			done <- result{err: fmt.Errorf("parse sse payload: %w", err)}
			return
		}

		if len(payload.Choices) == 0 {
			done <- result{chunk: rawChunk{}}
			return
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

		done <- result{chunk: chunk}
	}()

	select {
	case <-ctx.Done():
		return rawChunk{}, ctx.Err()
	case res := <-done:
		return res.chunk, res.err
	}
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
