package llm

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestStreamReader_MultiLineData_Reassembled(t *testing.T) {
	t.Parallel()

	// A single JSON payload pretty-printed across multiple data: lines.
	// When joined with \n the result is valid JSON.
	input := "data: {\n" +
		`data:   "choices": [` + "\n" +
		`data:     {"delta": {"content": "hello world"}}` + "\n" +
		`data:   ]` + "\n" +
		`data: }` + "\n\n"

	reader := NewStreamReader(io.NopCloser(strings.NewReader(input)))
	defer reader.Close()

	chunk, err := reader.readRawChunk(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chunk.Content != "hello world" {
		t.Errorf("Content = %q, want %q", chunk.Content, "hello world")
	}
}

func TestStreamReader_NoSpaceDataPrefix(t *testing.T) {
	t.Parallel()

	// data: without a space after the colon must be parsed.
	input := `data:{"choices":[{"delta":{"content":"Hello"}}]}` + "\n\n"

	reader := NewStreamReader(io.NopCloser(strings.NewReader(input)))
	defer reader.Close()

	chunk, err := reader.readRawChunk(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chunk.Content != "Hello" {
		t.Errorf("Content = %q, want %q", chunk.Content, "Hello")
	}
}

func TestStreamReader_CommentLineIgnored(t *testing.T) {
	t.Parallel()

	// A leading : comment/heartbeat line must be ignored.
	input := ":heartbeat\ndata: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n"

	reader := NewStreamReader(io.NopCloser(strings.NewReader(input)))
	defer reader.Close()

	chunk, err := reader.readRawChunk(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chunk.Content != "Hello" {
		t.Errorf("Content = %q, want %q", chunk.Content, "Hello")
	}
}

func TestStreamReader_MultiLineData_MultipleEvents(t *testing.T) {
	t.Parallel()

	// Mix of comment, multi-line data, and no-space prefix across separate events.
	input := ":comment line\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"hello world\"}}]}\n" +
		":another comment\n" +
		"\n" +
		"data:{\"choices\":[{\"delta\":{\"content\":\"!\"}}]}\n" +
		"\n" +
		"data: [DONE]\n\n"

	reader := NewStreamReader(io.NopCloser(strings.NewReader(input)))
	defer reader.Close()

	chunk1, err := reader.readRawChunk(context.Background())
	if err != nil {
		t.Fatalf("unexpected error on first chunk: %v", err)
	}
	if chunk1.Content != "hello world" {
		t.Errorf("first chunk Content = %q, want %q", chunk1.Content, "hello world")
	}

	chunk2, err := reader.readRawChunk(context.Background())
	if err != nil {
		t.Fatalf("unexpected error on second chunk: %v", err)
	}
	if chunk2.Content != "!" {
		t.Errorf("second chunk Content = %q, want %q", chunk2.Content, "!")
	}

	chunk3, err := reader.readRawChunk(context.Background())
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF on third chunk, got %v", err)
	}
	if !chunk3.Done {
		t.Error("expected Done = true on [DONE] event")
	}
}
