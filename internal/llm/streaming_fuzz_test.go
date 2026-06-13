package llm

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

// FuzzReadChunk feeds arbitrary bytes into StreamReader.readRawChunk and
// ensures the parser never panics, regardless of malformed SSE framing,
// invalid JSON, huge tokens, or truncated input.
func FuzzReadChunk(f *testing.F) {
	seeds := [][]byte{
		// Empty stream.
		[]byte(""),
		// Single data event.
		[]byte("data: hello\n\n"),
		// OpenAI-style JSON payload.
		[]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"\"}]}\n\n"),
		// Payload with finish_reason.
		[]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"),
		// [DONE] sentinel.
		[]byte("data: [DONE]\n\n"),
		// Multi-line data.
		[]byte("data: line1\ndata: line2\n\n"),
		// Comment / heartbeat lines.
		[]byte(": ping\ndata: ok\n\n"),
		// No trailing newline.
		[]byte("data: no-newline"),
		// Invalid JSON inside data field.
		[]byte("data: {not json}\n\n"),
		// Tool-call delta payload.
		[]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"read_file\",\"arguments\":\"{}\"}}]},\"finish_reason\":\"\"}]}\n\n"),
		// Mixed framing noise.
		[]byte("event: message\nid: 42\nretry: 1000\ndata: mixed\n\n"),
		// Truncated in the middle of a data line.
		[]byte("data: {\"choices\":["),
		// Two consecutive terminal markers, as emitted by OpenAI-style streams.
		[]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Done\"}}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"),
		// Oversized data line (exceeds the default 64 KB scanner limit).
		[]byte("data: " + strings.Repeat("x", 128*1024) + "\n\n"),
	}

	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		r := NewStreamReader(io.NopCloser(bytes.NewReader(data)))
		defer r.Close()

		// Drain the reader until EOF or any error. The parser is allowed to
		// return errors for malformed payloads, but it must never panic.
		for {
			_, err := r.readRawChunk(context.Background())
			if err != nil {
				return
			}
		}
	})
}
