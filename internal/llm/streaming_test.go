package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
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

func TestStreamReader_ScannerError(t *testing.T) {
	t.Parallel()

	// A single data line that exceeds the 1 MB scanner limit.
	input := "data: " + strings.Repeat("x", 2*1024*1024) + "\n\n"

	reader := NewStreamReader(io.NopCloser(strings.NewReader(input)))
	defer reader.Close()

	_, err := reader.readRawChunk(context.Background())
	if err == nil {
		t.Fatal("expected scanner error")
	}
	if !strings.Contains(err.Error(), "read sse stream") {
		t.Errorf("error = %q, want read sse stream", err.Error())
	}
}

func TestChatStream_ErrorDuringStream(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected http.Flusher")
		}
		_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n")
		flusher.Flush()
		_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"invalid\",\"tool_calls\":\"bad\"}}]}\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	client := NewClient(api.LLMConfig{BaseURL: server.URL, APIKey: "key", Model: "m"}, server.Client())
	stream, err := client.ChatStream(context.Background(), []api.Message{{Role: api.RoleUser, Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var gotErr error
	for chunk := range stream {
		if chunk.Error != nil {
			gotErr = chunk.Error
			break
		}
	}
	if gotErr == nil {
		t.Fatal("expected stream error")
	}
}

func TestStreamReader_EmptyChoices(t *testing.T) {
	t.Parallel()

	input := "data: {\"choices\":[]}\n\ndata: [DONE]\n\n"

	reader := NewStreamReader(io.NopCloser(strings.NewReader(input)))
	defer reader.Close()

	chunk, err := reader.readRawChunk(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chunk.Content != "" || chunk.Done || len(chunk.ToolCalls) != 0 {
		t.Errorf("chunk = %+v, want empty", chunk)
	}
}

func TestChatStream_IdleTimeout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected http.Flusher")
		}
		_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n")
		flusher.Flush()
		// Stop sending; the client should hit its idle timeout.
		<-r.Context().Done()
	}))
	defer server.Close()

	client := NewClient(api.LLMConfig{BaseURL: server.URL, APIKey: "key", Model: "m", Timeout: 100 * time.Millisecond}, server.Client())

	stream, err := client.ChatStream(context.Background(), []api.Message{{Role: api.RoleUser, Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var contents []string
	var idleErr error
	for chunk := range stream {
		if chunk.Error != nil {
			idleErr = chunk.Error
			break
		}
		if chunk.Content != "" {
			contents = append(contents, chunk.Content)
		}
	}

	if len(contents) != 1 || contents[0] != "hello" {
		t.Fatalf("contents = %v, want [hello]", contents)
	}
	if idleErr == nil {
		t.Fatal("expected idle timeout error")
	}
	if !strings.Contains(idleErr.Error(), "idle timeout") {
		t.Errorf("error = %q, want idle timeout", idleErr.Error())
	}
}
