package llm

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func TestIdleSimple(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	primary := &mockLLM{
		chatStreamFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			ch := make(chan api.StreamChunk)
			go func() {
				<-ctx.Done()
				close(ch)
			}()
			return ch, nil
		},
	}
	fallback := &mockLLM{
		chatStreamFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			ch := make(chan api.StreamChunk, 2)
			ch <- api.StreamChunk{Content: "fallback idle"}
			ch <- api.StreamChunk{Done: true}
			close(ch)
			return ch, nil
		},
	}

	client := NewFallbackClient(primary, fallback)
	ch, err := client.ChatStream(ctx, nil, nil)
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
	if got != "fallback idle" {
		t.Fatalf("expected fallback idle content, got %q", got)
	}
}

func TestIdlePrimaryReturnsError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	primary := &mockLLM{
		chatStreamFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			ch := make(chan api.StreamChunk, 1)
			ch <- api.StreamChunk{Error: fmt.Errorf("server error")}
			close(ch)
			return ch, nil
		},
	}
	fallback := &mockLLM{
		chatStreamFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			ch := make(chan api.StreamChunk, 2)
			ch <- api.StreamChunk{Content: "fallback error"}
			ch <- api.StreamChunk{Done: true}
			close(ch)
			return ch, nil
		},
	}

	client := NewFallbackClient(primary, fallback)
	ch, err := client.ChatStream(ctx, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got string
	for chunk := range ch {
		if chunk.Done {
			break
		}
		if chunk.Error != nil {
			t.Fatalf("unexpected error chunk: %v", chunk.Error)
		}
		got = chunk.Content
	}
	if got != "fallback error" {
		t.Fatalf("expected fallback error content, got %q", got)
	}
}

func TestIdlePrimaryClosesImmediately(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	primary := &mockLLM{
		chatStreamFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			ch := make(chan api.StreamChunk)
			close(ch)
			return ch, nil
		},
	}
	fallback := &mockLLM{
		chatStreamFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			ch := make(chan api.StreamChunk, 2)
			ch <- api.StreamChunk{Content: "fallback closed"}
			ch <- api.StreamChunk{Done: true}
			close(ch)
			return ch, nil
		},
	}

	client := NewFallbackClient(primary, fallback)
	ch, err := client.ChatStream(ctx, nil, nil)
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
	if got != "fallback closed" {
		t.Fatalf("expected fallback closed content, got %q", got)
	}
}

func TestIdlePrimarySendsContent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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
			ch := make(chan api.StreamChunk, 2)
			ch <- api.StreamChunk{Content: "fallback"}
			ch <- api.StreamChunk{Done: true}
			close(ch)
			return ch, nil
		},
	}

	client := NewFallbackClient(primary, fallback)
	ch, err := client.ChatStream(ctx, nil, nil)
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

func TestIdlePrimarySendsErrorThenContent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	primary := &mockLLM{
		chatStreamFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			ch := make(chan api.StreamChunk, 2)
			ch <- api.StreamChunk{Error: fmt.Errorf("server error")}
			ch <- api.StreamChunk{Content: "should not see this"}
			close(ch)
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
	ch, err := client.ChatStream(ctx, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got string
	for chunk := range ch {
		if chunk.Done {
			break
		}
		if chunk.Error != nil {
			t.Fatalf("unexpected error chunk: %v", chunk.Error)
		}
		got = chunk.Content
	}
	if got != "fallback" {
		t.Fatalf("expected fallback content, got %q", got)
	}
}

func TestIdleFallbackReturnsError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wantErr := fmt.Errorf("server error")

	primary := &mockLLM{
		chatStreamFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			ch := make(chan api.StreamChunk)
			close(ch)
			return ch, nil
		},
	}
	fallback := &mockLLM{
		chatStreamFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			return nil, wantErr
		},
	}

	client := NewFallbackClient(primary, fallback)
	_, err := client.ChatStream(ctx, nil, nil)
	if err != wantErr {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestIdlePrimaryReturnsContentThenError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wantErr := fmt.Errorf("mid-stream error")

	primary := &mockLLM{
		chatStreamFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			ch := make(chan api.StreamChunk, 3)
			ch <- api.StreamChunk{Content: "primary content"}
			ch <- api.StreamChunk{Error: wantErr}
			ch <- api.StreamChunk{Content: "should not see this"}
			close(ch)
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
	ch, err := client.ChatStream(ctx, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got string
	var gotErr error
	for chunk := range ch {
		if chunk.Error != nil {
			gotErr = chunk.Error
			break
		}
		got = chunk.Content
	}
	if got != "primary content" {
		t.Fatalf("expected primary content, got %q", got)
	}
	if gotErr != wantErr {
		t.Fatalf("expected error %v, got %v", wantErr, gotErr)
	}
}

func TestIdlePrimaryReturnsClientErrorNoFailover(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wantErr := &api.APIError{StatusCode: 400, Message: "bad request"}

	primary := &mockLLM{
		chatStreamFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			ch := make(chan api.StreamChunk, 1)
			ch <- api.StreamChunk{Error: wantErr}
			close(ch)
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
	_, err := client.ChatStream(ctx, nil, nil)
	if err != wantErr {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestIdlePrimarySyncClientErrorNoFailover(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wantErr := &api.APIError{StatusCode: 400, Message: "bad request"}

	primary := &mockLLM{
		chatStreamFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			return nil, wantErr
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
	_, err := client.ChatStream(ctx, nil, nil)
	if err != wantErr {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestIdleContextCancelledDuringPeek(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	primary := &mockLLM{
		chatStreamFunc: func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
			ch := make(chan api.StreamChunk)
			go func() {
				<-ctx.Done()
				close(ch)
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

	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	_, err := client.ChatStream(ctx, nil, nil)
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
