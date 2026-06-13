package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func TestNewClient(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		cfg         api.LLMConfig
		httpClient  *http.Client
		wantBase    string
		wantModel   string
		wantTimeout time.Duration
	}{
		{
			name: "default timeout",
			cfg: api.LLMConfig{
				BaseURL: "https://api.moonshot.cn/v1",
				APIKey:  "test-key",
				Model:   "kimi-k2.5",
			},
			wantBase:    "https://api.moonshot.cn/v1",
			wantModel:   "kimi-k2.5",
			wantTimeout: defaultTimeout,
		},
		{
			name: "custom timeout",
			cfg: api.LLMConfig{
				BaseURL: "https://api.openai.com/v1",
				APIKey:  "test-key",
				Model:   "gpt-4o",
				Timeout: 30 * time.Second,
			},
			wantBase:    "https://api.openai.com/v1",
			wantModel:   "gpt-4o",
			wantTimeout: 30 * time.Second,
		},
		{
			name:        "with custom http client",
			cfg:         api.LLMConfig{BaseURL: "http://localhost", Model: "test"},
			httpClient:  &http.Client{Timeout: 5 * time.Second},
			wantBase:    "http://localhost",
			wantModel:   "test",
			wantTimeout: defaultTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := NewClient(tt.cfg, tt.httpClient)
			if client.baseURL != tt.wantBase {
				t.Errorf("baseURL = %q, want %q", client.baseURL, tt.wantBase)
			}
			if client.model != tt.wantModel {
				t.Errorf("model = %q, want %q", client.model, tt.wantModel)
			}
			if client.timeout != tt.wantTimeout {
				t.Errorf("timeout = %v, want %v", client.timeout, tt.wantTimeout)
			}
			if tt.httpClient != nil && client.httpClient != tt.httpClient {
				t.Error("httpClient was not set correctly")
			}
		})
	}
}

func TestClientChat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		response         any
		statusCode       int
		messages         []api.Message
		tools            []api.ToolDefinition
		wantContent      string
		wantToolCalls    []api.ToolCall
		wantErr          bool
		wantErrContain   string
		wantAPIErrStatus int
	}{
		{
			name: "simple response",
			response: chatCompletionResponse{
				Choices: []struct {
					Message struct {
						Role      string     `json:"role"`
						Content   string     `json:"content"`
						ToolCalls []toolCall `json:"tool_calls,omitempty"`
					} `json:"message"`
					FinishReason string `json:"finish_reason"`
				}{
					{
						Message: struct {
							Role      string     `json:"role"`
							Content   string     `json:"content"`
							ToolCalls []toolCall `json:"tool_calls,omitempty"`
						}{
							Role:    "assistant",
							Content: "Hello, world!",
						},
						FinishReason: "stop",
					},
				},
			},
			statusCode:  http.StatusOK,
			messages:    []api.Message{{Role: api.RoleUser, Content: "Hi"}},
			wantContent: "Hello, world!",
		},
		{
			name: "response with tool calls",
			response: chatCompletionResponse{
				Choices: []struct {
					Message struct {
						Role      string     `json:"role"`
						Content   string     `json:"content"`
						ToolCalls []toolCall `json:"tool_calls,omitempty"`
					} `json:"message"`
					FinishReason string `json:"finish_reason"`
				}{
					{
						Message: struct {
							Role      string     `json:"role"`
							Content   string     `json:"content"`
							ToolCalls []toolCall `json:"tool_calls,omitempty"`
						}{
							Role:    "assistant",
							Content: "",
							ToolCalls: []toolCall{
								{
									ID:   "call_123",
									Type: "function",
									Function: function{
										Name:      "read_file",
										Arguments: `{"path":"/tmp/test.txt"}`,
									},
								},
							},
						},
						FinishReason: "tool_calls",
					},
				},
			},
			statusCode: http.StatusOK,
			messages:   []api.Message{{Role: api.RoleUser, Content: "Read a file"}},
			wantToolCalls: []api.ToolCall{
				{ID: "call_123", Name: "read_file", Arguments: `{"path":"/tmp/test.txt"}`},
			},
		},
		{
			name:             "client error",
			response:         map[string]string{"error": "invalid request"},
			statusCode:       http.StatusBadRequest,
			messages:         []api.Message{{Role: api.RoleUser, Content: "Hi"}},
			wantErr:          true,
			wantAPIErrStatus: http.StatusBadRequest,
		},
		{
			name:             "server error then success",
			response:         map[string]string{"error": "overloaded"},
			statusCode:       http.StatusServiceUnavailable,
			messages:         []api.Message{{Role: api.RoleUser, Content: "Hi"}},
			wantErr:          true,
			wantAPIErrStatus: http.StatusServiceUnavailable,
		},
		{
			name: "empty choices",
			response: chatCompletionResponse{Choices: []struct {
				Message struct {
					Role      string     `json:"role"`
					Content   string     `json:"content"`
					ToolCalls []toolCall `json:"tool_calls,omitempty"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			}{}},
			statusCode:     http.StatusOK,
			messages:       []api.Message{{Role: api.RoleUser, Content: "Hi"}},
			wantErr:        true,
			wantErrContain: "empty response from API",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			callCount := atomic.Int32{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				callCount.Add(1)

				if r.Method != http.MethodPost {
					t.Errorf("method = %q, want POST", r.Method)
				}
				if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
					t.Errorf("Authorization = %q, want Bearer test-key", auth)
				}

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				_ = json.NewEncoder(w).Encode(tt.response)
			}))
			defer server.Close()

			client := NewClient(api.LLMConfig{
				BaseURL: server.URL,
				APIKey:  "test-key",
				Model:   "test-model",
				Timeout: 5 * time.Second,
			}, server.Client())
			client.maxRetries = 2

			msg, err := client.Chat(context.Background(), tt.messages, tt.tools)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tt.wantErrContain != "" && !strings.Contains(err.Error(), tt.wantErrContain) {
					t.Errorf("error = %q, want containing %q", err.Error(), tt.wantErrContain)
				}
				if tt.wantAPIErrStatus != 0 {
					var apiErr *api.APIError
					if !errors.As(err, &apiErr) {
						t.Errorf("expected *api.APIError in error chain, got %T", err)
					} else if apiErr.StatusCode != tt.wantAPIErrStatus {
						t.Errorf("apiErr.StatusCode = %d, want %d", apiErr.StatusCode, tt.wantAPIErrStatus)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if msg.Content != tt.wantContent {
				t.Errorf("content = %q, want %q", msg.Content, tt.wantContent)
			}
			if msg.Role != api.RoleAssistant {
				t.Errorf("role = %q, want assistant", msg.Role)
			}
			if len(msg.ToolCalls) != len(tt.wantToolCalls) {
				t.Fatalf("toolCalls = %d, want %d", len(msg.ToolCalls), len(tt.wantToolCalls))
			}
			for i, want := range tt.wantToolCalls {
				if msg.ToolCalls[i] != want {
					t.Errorf("toolCalls[%d] = %+v, want %+v", i, msg.ToolCalls[i], want)
				}
			}
		})
	}
}

func TestClientChatRetrySuccess(t *testing.T) {
	t.Parallel()

	callCount := atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := callCount.Add(1)
		if count < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"overloaded"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(chatCompletionResponse{
			Choices: []struct {
				Message struct {
					Role      string     `json:"role"`
					Content   string     `json:"content"`
					ToolCalls []toolCall `json:"tool_calls,omitempty"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			}{
				{
					Message: struct {
						Role      string     `json:"role"`
						Content   string     `json:"content"`
						ToolCalls []toolCall `json:"tool_calls,omitempty"`
					}{Role: "assistant", Content: "Recovered!"},
					FinishReason: "stop",
				},
			},
		})
	}))
	defer server.Close()

	client := NewClient(api.LLMConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "test-model",
		Timeout: 5 * time.Second,
	}, server.Client())
	client.maxRetries = 3

	msg, err := client.Chat(context.Background(), []api.Message{{Role: api.RoleUser, Content: "Hi"}}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.Content != "Recovered!" {
		t.Errorf("content = %q, want %q", msg.Content, "Recovered!")
	}
	if callCount.Load() != 3 {
		t.Errorf("callCount = %d, want 3", callCount.Load())
	}
}

func TestClientChatContextCancellation(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow response
		select {
		case <-time.After(2 * time.Second):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		case <-r.Context().Done():
			// Request cancelled
		}
	}))
	defer server.Close()

	client := NewClient(api.LLMConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "test-model",
		Timeout: 10 * time.Second,
	}, server.Client())

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := client.Chat(ctx, []api.Message{{Role: api.RoleUser, Content: "Hi"}}, nil)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
	if !strings.Contains(err.Error(), "context") && !strings.Contains(err.Error(), "canceled") && !strings.Contains(err.Error(), "deadline") {
		t.Errorf("error = %q, want context-related error", err.Error())
	}
}

func TestClientChatStream(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		streamData       string
		wantContents     []string
		wantDone         bool
		wantErr          bool
		wantErrContain   string
		wantAPIErrStatus int
	}{
		{
			name: "simple stream",
			streamData: `data: {"choices":[{"delta":{"content":"Hello"}}]}

data: {"choices":[{"delta":{"content":" world"}}]}

data: {"choices":[{"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`,
			wantContents: []string{"Hello", " world"},
			wantDone:     true,
		},
		{
			name: "stream with tool calls",
			streamData: `data: {"choices":[{"delta":{"content":"Let me"}}]}

data: {"choices":[{"delta":{"content":" check"}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read_file"}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{"}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"path\":\"test.txt"}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"}"}}]}}]}

data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`,
			wantContents: []string{"Let me", " check"},
			wantDone:     true,
		},
		{
			name:             "stream server error",
			streamData:       ``,
			wantErr:          true,
			wantAPIErrStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.wantErr {
					w.WriteHeader(http.StatusUnauthorized)
					_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
					return
				}

				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(tt.streamData))
			}))
			defer server.Close()

			client := NewClient(api.LLMConfig{
				BaseURL: server.URL,
				APIKey:  "test-key",
				Model:   "test-model",
				Timeout: 5 * time.Second,
			}, server.Client())

			ch, err := client.ChatStream(context.Background(), []api.Message{{Role: api.RoleUser, Content: "Hi"}}, nil)
			if err != nil {
				if !tt.wantErr {
					t.Fatalf("unexpected error: %v", err)
				}
				if tt.wantErrContain != "" && !strings.Contains(err.Error(), tt.wantErrContain) {
					t.Errorf("error = %q, want containing %q", err.Error(), tt.wantErrContain)
				}
				if tt.wantAPIErrStatus != 0 {
					var apiErr *api.APIError
					if !errors.As(err, &apiErr) {
						t.Errorf("expected *api.APIError in error chain, got %T", err)
					} else if apiErr.StatusCode != tt.wantAPIErrStatus {
						t.Errorf("apiErr.StatusCode = %d, want %d", apiErr.StatusCode, tt.wantAPIErrStatus)
					}
				}
				return
			}
			if tt.wantErr {
				t.Fatal("expected error, got nil")
			}

			var contents []string
			var done bool
			var finalToolCalls []api.ToolCall
			var streamErr error

			for chunk := range ch {
				if chunk.Error != nil {
					streamErr = chunk.Error
					break
				}
				if chunk.Content != "" {
					contents = append(contents, chunk.Content)
				}
				if chunk.Done {
					done = true
					finalToolCalls = chunk.ToolCalls
				}
			}

			if streamErr != nil {
				t.Fatalf("stream error: %v", streamErr)
			}

			if !done {
				t.Error("expected Done = true")
			}

			if len(contents) != len(tt.wantContents) {
				t.Fatalf("contents = %v, want %v", contents, tt.wantContents)
			}
			for i, want := range tt.wantContents {
				if contents[i] != want {
					t.Errorf("contents[%d] = %q, want %q", i, contents[i], want)
				}
			}

			if tt.name == "stream with tool calls" {
				if len(finalToolCalls) != 1 {
					t.Fatalf("finalToolCalls = %d, want 1", len(finalToolCalls))
				}
				if finalToolCalls[0].ID != "call_1" {
					t.Errorf("toolCall.ID = %q, want call_1", finalToolCalls[0].ID)
				}
				if finalToolCalls[0].Name != "read_file" {
					t.Errorf("toolCall.Name = %q, want read_file", finalToolCalls[0].Name)
				}
				if !strings.Contains(finalToolCalls[0].Arguments, "path") {
					t.Errorf("toolCall.Arguments = %q, want containing path", finalToolCalls[0].Arguments)
				}
			}
		})
	}
}

func TestClientChatStreamContextCancellation(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected http.Flusher")
		}

		// Send first chunk, then block until client cancels
		_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n")
		flusher.Flush()

		<-r.Context().Done()
	}))
	defer server.Close()

	client := NewClient(api.LLMConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "test-model",
		Timeout: 10 * time.Second,
	}, server.Client())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := client.ChatStream(ctx, []api.Message{{Role: api.RoleUser, Content: "Hi"}}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var contents []string
	for chunk := range ch {
		if chunk.Error != nil {
			break
		}
		if chunk.Content != "" {
			contents = append(contents, chunk.Content)
			cancel() // Cancel context after receiving first chunk
		}
	}

	// Should have received the first chunk but not the second
	if len(contents) == 0 {
		t.Error("expected at least one chunk before cancellation")
	}
	if len(contents) > 1 {
		t.Errorf("expected at most one chunk, got %d", len(contents))
	}
}

func TestClientModels(t *testing.T) {
	t.Parallel()

	client := NewClient(api.LLMConfig{
		BaseURL: "https://api.moonshot.cn/v1",
		APIKey:  "test",
		Model:   "kimi-k2.5",
	}, nil)

	models := client.Models()
	if len(models) == 0 {
		t.Fatal("expected non-empty models list")
	}

	foundMoonshot := false
	foundOpenAI := false
	for _, m := range models {
		if m.Provider == "moonshot" {
			foundMoonshot = true
		}
		if m.Provider == "openai" {
			foundOpenAI = true
		}
	}
	if !foundMoonshot {
		t.Error("expected moonshot models")
	}
	if !foundOpenAI {
		t.Error("expected openai models")
	}
}

func TestClientBuildChatRequest(t *testing.T) {
	t.Parallel()

	client := NewClient(api.LLMConfig{
		BaseURL: "https://api.test.com/v1",
		APIKey:  "key",
		Model:   "test-model",
	}, nil)

	messages := []api.Message{
		{Role: api.RoleSystem, Content: "You are helpful"},
		{Role: api.RoleUser, Content: "Hello"},
		{Role: api.RoleAssistant, Content: "Hi!", ToolCalls: []api.ToolCall{
			{ID: "call_1", Name: "foo", Arguments: `{}`},
		}},
		{Role: api.RoleTool, Content: "result", ID: "msg-4", ToolCallID: "call_1"},
	}

	tools := []api.ToolDefinition{
		{
			Name:        "foo",
			Description: "A foo tool",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		},
	}

	req := client.buildChatRequest(messages, tools, true)

	if req.Model != "test-model" {
		t.Errorf("model = %q, want test-model", req.Model)
	}
	if !req.Stream {
		t.Error("stream = false, want true")
	}
	if len(req.Messages) != 4 {
		t.Fatalf("messages = %d, want 4", len(req.Messages))
	}

	// Check system message
	if req.Messages[0].Role != "system" {
		t.Errorf("messages[0].role = %q, want system", req.Messages[0].Role)
	}

	// Check assistant tool calls
	if len(req.Messages[2].ToolCalls) != 1 {
		t.Errorf("messages[2].tool_calls = %d, want 1", len(req.Messages[2].ToolCalls))
	}
	if req.Messages[2].ToolCalls[0].ID != "call_1" {
		t.Errorf("messages[2].tool_calls[0].id = %q, want call_1", req.Messages[2].ToolCalls[0].ID)
	}

	// Check tool message
	if req.Messages[3].Role != "tool" {
		t.Errorf("messages[3].role = %q, want tool", req.Messages[3].Role)
	}
	if req.Messages[3].ToolCallID != "call_1" {
		t.Errorf("messages[3].tool_call_id = %q, want call_1", req.Messages[3].ToolCallID)
	}

	// Check tools
	if len(req.Tools) != 1 {
		t.Fatalf("tools = %d, want 1", len(req.Tools))
	}
	if req.Tools[0].Function.Name != "foo" {
		t.Errorf("tools[0].name = %q, want foo", req.Tools[0].Function.Name)
	}
}

func TestStreamReader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		input        string
		wantChunks   []rawChunk
		wantErr      bool
		wantErrAfter int // number of successful chunks before error
	}{
		{
			name: "simple chunks",
			input: `data: {"choices":[{"delta":{"content":"Hello"}}]}

data: {"choices":[{"delta":{"content":" world"}}]}

data: [DONE]

`,
			wantChunks: []rawChunk{
				{Content: "Hello"},
				{Content: " world"},
				{Done: true},
			},
		},
		{
			name: "done via finish_reason",
			input: `data: {"choices":[{"delta":{"content":"Done"}}]}

data: {"choices":[{"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`,
			wantChunks: []rawChunk{
				{Content: "Done"},
				{Done: true},
				{Done: true},
			},
		},
		{
			name: "empty input",
			input: `data: [DONE]

`,
			wantChunks: []rawChunk{
				{Done: true},
			},
		},
		{
			name: "invalid json",
			input: `data: {invalid}

`,
			wantErr:      true,
			wantErrAfter: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reader := NewStreamReader(io.NopCloser(strings.NewReader(tt.input)))
			defer reader.Close()

			var chunks []rawChunk
			for {
				chunk, err := reader.readRawChunk(context.Background())
				if err == io.EOF {
					if chunk.Done || chunk.Content != "" {
						chunks = append(chunks, chunk)
					}
					break
				}
				if err != nil {
					if !tt.wantErr {
						t.Fatalf("unexpected error: %v", err)
					}
					if len(chunks) != tt.wantErrAfter {
						t.Fatalf("got %d chunks before error, want %d", len(chunks), tt.wantErrAfter)
					}
					return
				}
				chunks = append(chunks, chunk)
			}

			if tt.wantErr {
				t.Fatal("expected error, got nil")
			}

			if len(chunks) != len(tt.wantChunks) {
				t.Fatalf("chunks = %d, want %d", len(chunks), len(tt.wantChunks))
			}
			for i, want := range tt.wantChunks {
				if chunks[i].Content != want.Content {
					t.Errorf("chunks[%d].Content = %q, want %q", i, chunks[i].Content, want.Content)
				}
				if chunks[i].Done != want.Done {
					t.Errorf("chunks[%d].Done = %v, want %v", i, chunks[i].Done, want.Done)
				}
			}
		})
	}
}

func TestBackoff(t *testing.T) {
	t.Parallel()

	client := NewClient(api.LLMConfig{}, nil)

	tests := []struct {
		attempt int
		wantMax time.Duration
	}{
		{attempt: 1, wantMax: 1 * time.Second},
		{attempt: 2, wantMax: 2 * time.Second},
		{attempt: 3, wantMax: 4 * time.Second},
		{attempt: 5, wantMax: 30 * time.Second},
		{attempt: 10, wantMax: 30 * time.Second},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("attempt_%d", tt.attempt), func(t *testing.T) {
			t.Parallel()
			for i := 0; i < 20; i++ {
				delay := client.backoff(tt.attempt)
				if delay > tt.wantMax {
					t.Errorf("backoff(%d) = %v, want <= %v", tt.attempt, delay, tt.wantMax)
				}
				if delay < 0 {
					t.Errorf("backoff(%d) = %v, want >= 0", tt.attempt, delay)
				}
			}
		})
	}
}

func TestIsRetryableError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"context canceled", context.Canceled, false},
		{"context deadline exceeded", context.DeadlineExceeded, true},
		{"network timeout", &net.DNSError{IsTimeout: true}, true},
		{"dns not found", &net.DNSError{IsNotFound: true}, false},
		{"connection refused", &net.OpError{Err: syscall.ECONNREFUSED}, false},
		{"connection reset", &net.OpError{Err: syscall.ECONNRESET}, true},
		{"unexpected EOF", io.ErrUnexpectedEOF, true},
		{"random error", fmt.Errorf("random"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isRetryableError(tt.err)
			if got != tt.want {
				t.Errorf("isRetryableError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestLookupModel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		model        string
		wantName     string
		wantProvider string
	}{
		{"moonshot kimi-k2.5", "kimi-k2.5", "kimi-k2.5", "moonshot"},
		{"openai gpt-4o", "gpt-4o", "gpt-4o", "openai"},
		{"unknown model", "custom-model", "custom-model", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			info := LookupModel(tt.model)
			if info.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", info.Name, tt.wantName)
			}
			if info.Provider != tt.wantProvider {
				t.Errorf("Provider = %q, want %q", info.Provider, tt.wantProvider)
			}
		})
	}
}

func TestBuildChatRequest_ToolCallID(t *testing.T) {
	t.Parallel()

	c := NewClient(api.LLMConfig{BaseURL: "https://example.com", APIKey: "key", Model: "m"}, nil)

	messages := []api.Message{
		{ID: "msg-1", Role: api.RoleAssistant, Content: "let me check", ToolCalls: []api.ToolCall{{ID: "call-abc", Name: "read_file", Arguments: `{}`}}},
		{ID: "msg-2", Role: api.RoleTool, Content: "hello", ToolCallID: "call-abc"},
		{ID: "msg-3", Role: api.RoleTool, Content: "world", ToolCallID: ""},
	}

	req := c.buildChatRequest(messages, nil, false)

	if len(req.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(req.Messages))
	}

	// Assistant message should NOT have tool_call_id.
	if req.Messages[0].ToolCallID != "" {
		t.Errorf("assistant message tool_call_id = %q, want empty", req.Messages[0].ToolCallID)
	}

	// Tool message with matching call ID must forward it.
	if req.Messages[1].ToolCallID != "call-abc" {
		t.Errorf("tool message tool_call_id = %q, want call-abc", req.Messages[1].ToolCallID)
	}

	// Tool message with empty call ID should remain empty.
	if req.Messages[2].ToolCallID != "" {
		t.Errorf("tool message tool_call_id = %q, want empty", req.Messages[2].ToolCallID)
	}
}

func TestSortedToolCalls(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		accumulator map[int]*rawToolCall
		want        []api.ToolCall
	}{
		{
			name:        "empty",
			accumulator: map[int]*rawToolCall{},
			want:        nil,
		},
		{
			name: "contiguous indices",
			accumulator: map[int]*rawToolCall{
				0: {Index: 0, ID: "call_1", Name: "read_file"},
				1: {Index: 1, ID: "call_2", Name: "write_file"},
			},
			want: []api.ToolCall{
				{ID: "call_1", Name: "read_file"},
				{ID: "call_2", Name: "write_file"},
			},
		},
		{
			name: "non-contiguous indices",
			accumulator: map[int]*rawToolCall{
				0: {Index: 0, ID: "call_1", Name: "read_file"},
				2: {Index: 2, ID: "call_3", Name: "edit_file"},
			},
			want: []api.ToolCall{
				{ID: "call_1", Name: "read_file"},
				{ID: "call_3", Name: "edit_file"},
			},
		},
		{
			name: "reverse order insertion",
			accumulator: map[int]*rawToolCall{
				2: {Index: 2, ID: "call_3", Name: "edit_file"},
				0: {Index: 0, ID: "call_1", Name: "read_file"},
				1: {Index: 1, ID: "call_2", Name: "write_file"},
			},
			want: []api.ToolCall{
				{ID: "call_1", Name: "read_file"},
				{ID: "call_2", Name: "write_file"},
				{ID: "call_3", Name: "edit_file"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := sortedToolCalls(tt.accumulator)
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("[%d] = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestStreamReader_LargePayload(t *testing.T) {
	t.Parallel()

	// Build a payload larger than the default 64 KB scanner limit.
	largeContent := strings.Repeat("x", 128*1024)
	payload := fmt.Sprintf(`{"choices":[{"delta":{"content":%q}}]}`, largeContent)
	input := "data: " + payload + "\n\ndata: [DONE]\n\n"

	reader := NewStreamReader(io.NopCloser(strings.NewReader(input)))
	defer reader.Close()

	chunk, err := reader.readRawChunk(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chunk.Content != largeContent {
		t.Errorf("content length = %d, want %d", len(chunk.Content), len(largeContent))
	}

	chunk, err = reader.readRawChunk(context.Background())
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
	if !chunk.Done {
		t.Error("expected Done = true")
	}
}

func TestClientChatRetryAfterHeader(t *testing.T) {
	t.Parallel()

	callCount := atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := callCount.Add(1)
		if count == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate limited"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(chatCompletionResponse{
			Choices: []struct {
				Message struct {
					Role      string     `json:"role"`
					Content   string     `json:"content"`
					ToolCalls []toolCall `json:"tool_calls,omitempty"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			}{{
				Message: struct {
					Role      string     `json:"role"`
					Content   string     `json:"content"`
					ToolCalls []toolCall `json:"tool_calls,omitempty"`
				}{Role: "assistant", Content: "OK"},
			}},
		})
	}))
	defer server.Close()

	client := NewClient(api.LLMConfig{BaseURL: server.URL, APIKey: "test-key", Model: "test"}, server.Client())

	start := time.Now()
	msg, err := client.Chat(context.Background(), []api.Message{{Role: api.RoleUser, Content: "Hi"}}, nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if msg.Content != "OK" {
		t.Errorf("content = %q, want OK", msg.Content)
	}
	if callCount.Load() != 2 {
		t.Errorf("callCount = %d, want 2", callCount.Load())
	}
	if elapsed < 900*time.Millisecond {
		t.Errorf("elapsed = %v, want >= 900ms", elapsed)
	}
}

func TestClientChatRetryAfterHTTPDate(t *testing.T) {
	t.Parallel()

	callCount := atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := callCount.Add(1)
		if count == 1 {
			w.Header().Set("Retry-After", time.Now().UTC().Add(500*time.Millisecond).Format(http.TimeFormat))
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"overloaded"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(chatCompletionResponse{
			Choices: []struct {
				Message struct {
					Role      string     `json:"role"`
					Content   string     `json:"content"`
					ToolCalls []toolCall `json:"tool_calls,omitempty"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			}{{
				Message: struct {
					Role      string     `json:"role"`
					Content   string     `json:"content"`
					ToolCalls []toolCall `json:"tool_calls,omitempty"`
				}{Role: "assistant", Content: "OK"},
			}},
		})
	}))
	defer server.Close()

	client := NewClient(api.LLMConfig{BaseURL: server.URL, APIKey: "test-key", Model: "test"}, server.Client())

	msg, err := client.Chat(context.Background(), []api.Message{{Role: api.RoleUser, Content: "Hi"}}, nil)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if msg.Content != "OK" {
		t.Errorf("content = %q, want OK", msg.Content)
	}
	if callCount.Load() != 2 {
		t.Errorf("callCount = %d, want 2", callCount.Load())
	}
}

func TestClientChatStream_FinishReasonLength(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"length\"}]}\n\n"))
	}))
	defer server.Close()

	client := NewClient(api.LLMConfig{BaseURL: server.URL, APIKey: "test-key", Model: "test"}, server.Client())

	stream, err := client.ChatStream(context.Background(), []api.Message{{Role: api.RoleUser, Content: "Hi"}}, nil)
	if err != nil {
		t.Fatalf("chat stream: %v", err)
	}

	var lastChunk api.StreamChunk
	for chunk := range stream {
		lastChunk = chunk
	}

	if !lastChunk.Done {
		t.Fatal("expected last chunk to be Done")
	}
	if lastChunk.FinishReason != "length" {
		t.Errorf("FinishReason = %q, want length", lastChunk.FinishReason)
	}
}

func TestClientChat_FinishReasonStop(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(chatCompletionResponse{
			Choices: []struct {
				Message struct {
					Role      string     `json:"role"`
					Content   string     `json:"content"`
					ToolCalls []toolCall `json:"tool_calls,omitempty"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			}{{
				Message: struct {
					Role      string     `json:"role"`
					Content   string     `json:"content"`
					ToolCalls []toolCall `json:"tool_calls,omitempty"`
				}{Role: "assistant", Content: "OK"},
				FinishReason: "stop",
			}},
		})
	}))
	defer server.Close()

	client := NewClient(api.LLMConfig{BaseURL: server.URL, APIKey: "test-key", Model: "test"}, server.Client())

	msg, err := client.Chat(context.Background(), []api.Message{{Role: api.RoleUser, Content: "Hi"}}, nil)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if msg.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want stop", msg.FinishReason)
	}
}

func TestNewClient_TrailingSlashBaseURL(t *testing.T) {
	t.Parallel()

	var reqPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(chatCompletionResponse{
			Choices: []struct {
				Message struct {
					Role      string     `json:"role"`
					Content   string     `json:"content"`
					ToolCalls []toolCall `json:"tool_calls,omitempty"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			}{{
				Message: struct {
					Role      string     `json:"role"`
					Content   string     `json:"content"`
					ToolCalls []toolCall `json:"tool_calls,omitempty"`
				}{Role: "assistant", Content: "OK"},
			}},
		})
	}))
	defer server.Close()

	client := NewClient(api.LLMConfig{BaseURL: server.URL + "/", APIKey: "test-key", Model: "test"}, server.Client())
	_, err := client.Chat(context.Background(), []api.Message{{Role: api.RoleUser, Content: "Hi"}}, nil)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if reqPath != "/chat/completions" {
		t.Errorf("request path = %q, want /chat/completions", reqPath)
	}
}

func TestBuildChatRequest_MaxTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		model         string
		wantMaxTokens int
	}{
		{
			name:          "moonshot-v1-8k",
			model:         "moonshot-v1-8k",
			wantMaxTokens: 4096,
		},
		{
			name:          "kimi-k2.5",
			model:         "kimi-k2.5",
			wantMaxTokens: 8192,
		},
		{
			name:          "unknown model defaults to 4096",
			model:         "unknown-model",
			wantMaxTokens: 4096,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := NewClient(api.LLMConfig{
				BaseURL: "https://api.test.com/v1",
				APIKey:  "key",
				Model:   tt.model,
			}, nil)

			req := client.buildChatRequest([]api.Message{{Role: api.RoleUser, Content: "Hi"}}, nil, false)

			if req.MaxTokens != tt.wantMaxTokens {
				t.Errorf("MaxTokens = %d, want %d", req.MaxTokens, tt.wantMaxTokens)
			}

			// Verify JSON serialization includes max_tokens for known models.
			body, err := json.Marshal(req)
			if err != nil {
				t.Fatalf("marshal request: %v", err)
			}

			var raw map[string]any
			if err := json.Unmarshal(body, &raw); err != nil {
				t.Fatalf("unmarshal raw: %v", err)
			}

			if tt.wantMaxTokens > 0 {
				if raw["max_tokens"] == nil {
					t.Fatalf("expected max_tokens in serialized request")
				}
				if int(raw["max_tokens"].(float64)) != tt.wantMaxTokens {
					t.Errorf("serialized max_tokens = %v, want %d", raw["max_tokens"], tt.wantMaxTokens)
				}
			}
		})
	}
}
