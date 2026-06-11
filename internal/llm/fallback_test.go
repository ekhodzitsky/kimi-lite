package llm

import (
	"context"
	"errors"
	"testing"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

type mockLLM struct {
	chatStreamFunc func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error)
	chatFunc       func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (*api.Message, error)
}

func (m *mockLLM) Chat(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (*api.Message, error) {
	if m.chatFunc != nil {
		return m.chatFunc(ctx, messages, tools)
	}
	return &api.Message{Role: api.RoleAssistant, Content: "hello"}, nil
}

func (m *mockLLM) ChatStream(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
	if m.chatStreamFunc != nil {
		return m.chatStreamFunc(ctx, messages, tools)
	}
	ch := make(chan api.StreamChunk, 1)
	ch <- api.StreamChunk{Done: true}
	close(ch)
	return ch, nil
}

func (m *mockLLM) Models() []api.ModelInfo {
	return nil
}

func TestFallbackClient_ChatStream_PreContentError(t *testing.T) {

	primaryErr := errors.New("primary pre-content error")
	primary := &mockLLM{
		chatStreamFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			ch := make(chan api.StreamChunk, 1)
			ch <- api.StreamChunk{Error: primaryErr}
			close(ch)
			return ch, nil
		},
	}
	fallback := &mockLLM{
		chatStreamFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			ch := make(chan api.StreamChunk, 2)
			ch <- api.StreamChunk{Content: "fallback content"}
			ch <- api.StreamChunk{Done: true}
			close(ch)
			return ch, nil
		},
	}

	client := NewFallbackClient(primary, fallback)
	ch, err := client.ChatStream(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var contents []string
	var done bool
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("unexpected stream error: %v", chunk.Error)
		}
		if chunk.Done {
			done = true
			break
		}
		if chunk.Content != "" {
			contents = append(contents, chunk.Content)
		}
	}

	if !done {
		t.Fatal("expected done")
	}
	if len(contents) != 1 || contents[0] != "fallback content" {
		t.Fatalf("unexpected contents: %v", contents)
	}
}

func TestFallbackClient_ChatStream_OpenError(t *testing.T) {

	primary := &mockLLM{
		chatStreamFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			return nil, errors.New("connection refused")
		},
	}
	fallback := &mockLLM{
		chatStreamFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			ch := make(chan api.StreamChunk, 2)
			ch <- api.StreamChunk{Content: "fallback"}
			ch <- api.StreamChunk{Done: true}
			close(ch)
			return ch, nil
		},
	}

	client := NewFallbackClient(primary, fallback)
	ch, err := client.ChatStream(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got string
	for chunk := range ch {
		if chunk.Done {
			break
		}
		got = chunk.Content
	}
	if got != "fallback" {
		t.Fatalf("expected fallback content, got %q", got)
	}
}

func TestFallbackClient_ChatStream_HappyPath(t *testing.T) {

	primary := &mockLLM{
		chatStreamFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			ch := make(chan api.StreamChunk, 2)
			ch <- api.StreamChunk{Content: "primary"}
			ch <- api.StreamChunk{Done: true}
			close(ch)
			return ch, nil
		},
	}
	fallback := &mockLLM{
		chatStreamFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			t.Error("fallback should not be called")
			return nil, errors.New("should not reach")
		},
	}

	client := NewFallbackClient(primary, fallback)
	ch, err := client.ChatStream(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got string
	for chunk := range ch {
		if chunk.Done {
			break
		}
		got = chunk.Content
	}
	if got != "primary" {
		t.Fatalf("expected primary content, got %q", got)
	}
}
