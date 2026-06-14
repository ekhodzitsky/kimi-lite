package llm

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

type mockLLM struct {
	chatStreamFunc   func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error)
	chatFunc         func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (*api.Message, error)
	modelsFunc       func() []api.ModelInfo
	setMetricsFunc   func(api.MetricsCollector)
	metricsCollector api.MetricsCollector
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
	if m.modelsFunc != nil {
		return m.modelsFunc()
	}
	return nil
}

func (m *mockLLM) SetMetricsCollector(collector api.MetricsCollector) {
	m.metricsCollector = collector
	if m.setMetricsFunc != nil {
		m.setMetricsFunc(collector)
	}
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

func TestFallbackClient_Models(t *testing.T) {
	t.Parallel()

	primary := &mockLLM{
		modelsFunc: func() []api.ModelInfo {
			return []api.ModelInfo{
				{Name: "model-a", Provider: "primary"},
				{Name: "model-b", Provider: "primary"},
			}
		},
	}
	fallback := &mockLLM{
		modelsFunc: func() []api.ModelInfo {
			return []api.ModelInfo{
				{Name: "model-b", Provider: "fallback"},
				{Name: "model-c", Provider: "fallback"},
			}
		},
	}

	client := NewFallbackClient(primary, fallback)
	models := client.Models()

	if len(models) != 3 {
		t.Fatalf("expected 3 models, got %d", len(models))
	}
	if models[0].Name != "model-a" {
		t.Errorf("models[0].Name = %q, want model-a", models[0].Name)
	}
	if models[1].Name != "model-b" {
		t.Errorf("models[1].Name = %q, want model-b", models[1].Name)
	}
	if models[2].Name != "model-c" {
		t.Errorf("models[2].Name = %q, want model-c", models[2].Name)
	}
}

func TestFallbackClient_Chat_PrimaryError_FallbackInvoked(t *testing.T) {
	t.Parallel()

	want := &api.Message{Role: api.RoleAssistant, Content: "fallback"}
	primaryErr := errors.New("primary chat error")
	primary := &mockLLM{
		chatFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (*api.Message, error) {
			return nil, primaryErr
		},
	}
	fallback := &mockLLM{
		chatFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (*api.Message, error) {
			return want, nil
		},
	}

	client := NewFallbackClient(primary, fallback)
	got, err := client.Chat(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Content != want.Content {
		t.Errorf("content = %q, want %q", got.Content, want.Content)
	}
}

func TestFallbackClient_Chat_PrimarySuccess_FallbackNotCalled(t *testing.T) {
	t.Parallel()

	want := &api.Message{Role: api.RoleAssistant, Content: "primary"}
	primary := &mockLLM{
		chatFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (*api.Message, error) {
			return want, nil
		},
	}
	fallback := &mockLLM{
		chatFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (*api.Message, error) {
			t.Error("fallback Chat should not be called")
			return nil, errors.New("should not reach")
		},
	}

	client := NewFallbackClient(primary, fallback)
	got, err := client.Chat(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Content != want.Content {
		t.Errorf("content = %q, want %q", got.Content, want.Content)
	}
}

func TestFallbackClient_Chat_NilFallback_ErrorPropagates(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("primary chat error")
	primary := &mockLLM{
		chatFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (*api.Message, error) {
			return nil, wantErr
		},
	}

	client := NewFallbackClient(primary, nil)
	_, err := client.Chat(context.Background(), nil, nil)
	if err != wantErr {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestFallbackClient_Chat_PrimaryClientError_NoFailover(t *testing.T) {
	t.Parallel()

	wantErr := &api.APIError{StatusCode: 400, Message: "bad request"}
	primary := &mockLLM{
		chatFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (*api.Message, error) {
			return nil, wantErr
		},
	}
	fallback := &mockLLM{
		chatFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (*api.Message, error) {
			t.Error("fallback Chat should not be called for 4xx")
			return &api.Message{Role: api.RoleAssistant, Content: "fallback"}, nil
		},
	}

	client := NewFallbackClient(primary, fallback)
	_, err := client.Chat(context.Background(), nil, nil)
	if err != wantErr {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestFallbackClient_ChatStream_CancelsPrimaryOnFallback(t *testing.T) {
	t.Parallel()

	primaryCtxCanceled := make(chan struct{})
	primary := &mockLLM{
		chatStreamFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			ch := make(chan api.StreamChunk, 1)
			ch <- api.StreamChunk{Error: errors.New("primary pre-content error")}
			close(ch)
			go func() {
				<-ctx.Done()
				close(primaryCtxCanceled)
			}()
			return ch, nil
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

	select {
	case <-primaryCtxCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("primary context was not canceled after fallback")
	}
}

func TestFallbackClient_SetMetricsCollector(t *testing.T) {
	t.Parallel()

	primary := &mockLLM{}
	fallback := &mockLLM{}
	client := NewFallbackClient(primary, fallback)

	collector := api.NoopMetricsCollector{}
	client.SetMetricsCollector(collector)

	if primary.metricsCollector != collector {
		t.Error("primary metrics collector was not set")
	}
	if fallback.metricsCollector != collector {
		t.Error("fallback metrics collector was not set")
	}

	// Nil fallback should not panic.
	client = NewFallbackClient(primary, nil)
	client.SetMetricsCollector(collector)

	// Primary without SetMetricsCollector method should not panic.
	nonMetricsPrimary := &bareMockLLM{}
	client = NewFallbackClient(nonMetricsPrimary, fallback)
	client.SetMetricsCollector(collector)
}

// bareMockLLM implements api.LLMClient without a SetMetricsCollector method.
type bareMockLLM struct{}

func (b *bareMockLLM) Chat(context.Context, []api.Message, []api.ToolDefinition) (*api.Message, error) {
	return &api.Message{Role: api.RoleAssistant, Content: "ok"}, nil
}

func (b *bareMockLLM) ChatStream(context.Context, []api.Message, []api.ToolDefinition) (<-chan api.StreamChunk, error) {
	ch := make(chan api.StreamChunk, 1)
	ch <- api.StreamChunk{Done: true}
	close(ch)
	return ch, nil
}

func (b *bareMockLLM) Models() []api.ModelInfo { return nil }
