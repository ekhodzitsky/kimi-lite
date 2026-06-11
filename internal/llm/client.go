// Package llm provides an OpenAI-compatible LLM client with SSE streaming support.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"sort"
	"time"

	"github.com/ekhodzitsky/kimi-lite/internal/netutil"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

const (
	defaultTimeout  = 60 * time.Second
	defaultRetries  = 3
	maxBackoffDelay = 30 * time.Second
)

// Client implements api.LLMClient for OpenAI-compatible APIs.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	model      string
	timeout    time.Duration
	maxRetries int
}

// NewClient creates a new LLM client from configuration.
// If httpClient is nil, a default http.Client is used.
func NewClient(cfg api.LLMConfig, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{
			Transport: netutil.SecureTransport(),
		}
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	return &Client{
		httpClient: httpClient,
		baseURL:    cfg.BaseURL,
		apiKey:     cfg.APIKey,
		model:      cfg.Model,
		timeout:    timeout,
		maxRetries: defaultRetries,
	}
}

// Chat sends messages to the LLM and returns the complete response.
func (c *Client) Chat(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (*api.Message, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	reqBody := c.buildChatRequest(messages, tools, false)
	respBody, err := c.doRequest(ctx, reqBody)
	if err != nil {
		return nil, fmt.Errorf("chat request failed: %w", err)
	}

	var resp chatCompletionResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("decode chat response: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, &api.APIError{StatusCode: http.StatusOK, Message: "empty response from API"}
	}

	choice := resp.Choices[0].Message
	msg := &api.Message{
		Role:      api.Role(choice.Role),
		Content:   choice.Content,
		CreatedAt: time.Now().UTC(),
	}

	if len(choice.ToolCalls) > 0 {
		msg.ToolCalls = make([]api.ToolCall, 0, len(choice.ToolCalls))
		for _, tc := range choice.ToolCalls {
			msg.ToolCalls = append(msg.ToolCalls, api.ToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			})
		}
	}

	return msg, nil
}

// sortedToolCalls extracts tool calls from the accumulator in index order.
func sortedToolCalls(accumulator map[int]*rawToolCall) []api.ToolCall {
	if len(accumulator) == 0 {
		return nil
	}
	indices := make([]int, 0, len(accumulator))
	for i := range accumulator {
		indices = append(indices, i)
	}
	sort.Ints(indices)

	calls := make([]api.ToolCall, 0, len(indices))
	for _, i := range indices {
		tc := accumulator[i]
		calls = append(calls, api.ToolCall{
			ID:        tc.ID,
			Name:      tc.Name,
			Arguments: tc.Arguments,
		})
	}
	return calls
}

// ChatStream sends messages to the LLM and streams the response via a channel.
// The returned channel is closed when the stream ends or an error occurs.
// Callers should check chunk.Error for stream errors.
func (c *Client) ChatStream(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
	ctx, cancel := context.WithCancel(ctx)

	reqBody := c.buildChatRequest(messages, tools, true)
	body, err := c.doRequestStream(ctx, reqBody)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("chat stream request failed: %w", err)
	}

	ch := make(chan api.StreamChunk, 64)
	go func() {
		defer close(ch)
		defer cancel()
		defer body.Close()

		reader := NewStreamReader(body)
		accumulator := make(map[int]*rawToolCall)

		idleTimeout := c.timeout
		type result struct {
			raw rawChunk
			err error
		}
		readCh := make(chan result, 1)

		// One read goroutine per stream (not per chunk).
		go func() {
			defer close(readCh)
			for {
				raw, err := reader.readRawChunk(ctx)
				select {
				case readCh <- result{raw, err}:
				case <-ctx.Done():
					return
				}
				if err != nil {
					return
				}
			}
		}()

		timer := time.NewTimer(idleTimeout)
		defer timer.Stop()

		for {
			select {
			case <-ctx.Done():
				select {
				case ch <- api.StreamChunk{Error: ctx.Err()}:
				case <-ctx.Done():
				}
				return
			case <-timer.C:
				select {
				case ch <- api.StreamChunk{Error: fmt.Errorf("stream idle timeout after %v", idleTimeout)}:
				case <-ctx.Done():
				}
				return
			case res, ok := <-readCh:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				if !ok {
					return
				}
				raw, err := res.raw, res.err
				if err == io.EOF {
					select {
					case ch <- api.StreamChunk{Done: true, ToolCalls: sortedToolCalls(accumulator)}:
					case <-ctx.Done():
					}
					return
				}
				if err != nil {
					select {
					case ch <- api.StreamChunk{Error: err}:
					case <-ctx.Done():
					}
					return
				}

				for _, tc := range raw.ToolCalls {
					if _, ok := accumulator[tc.Index]; !ok {
						accumulator[tc.Index] = &rawToolCall{}
					}
					if tc.ID != "" {
						accumulator[tc.Index].ID = tc.ID
					}
					if tc.Name != "" {
						accumulator[tc.Index].Name = tc.Name
					}
					accumulator[tc.Index].Arguments += tc.Arguments
				}

				if raw.Done {
					select {
					case ch <- api.StreamChunk{Done: true, ToolCalls: sortedToolCalls(accumulator)}:
					case <-ctx.Done():
					}
					return
				}

				if raw.Content != "" {
					select {
					case ch <- api.StreamChunk{Content: raw.Content}:
					case <-ctx.Done():
						return
					}
				}

				timer.Reset(idleTimeout)
			}
		}
	}()

	return ch, nil
}

// Models returns available model configurations.
func (c *Client) Models() []api.ModelInfo {
	return AllModels()
}

// withTimeout applies the client timeout if the context has no deadline.
func (c *Client) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, c.timeout)
}

// doRequestWithRetry performs the HTTP request with retries and returns the raw response.
func (c *Client) doRequestWithRetry(ctx context.Context, body []byte, stream bool) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(c.backoff(attempt)):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		if stream {
			req.Header.Set("Accept", "text/event-stream")
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			if !isRetryableError(err) {
				return nil, fmt.Errorf("do request: %w", err)
			}
			continue
		}

		if resp.StatusCode >= http.StatusInternalServerError || resp.StatusCode == http.StatusTooManyRequests {
			respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			slog.Debug("LLM server error", "status", resp.StatusCode, "body", string(respBody))
			lastErr = &api.APIError{StatusCode: resp.StatusCode, Message: "server error", Body: string(respBody)}
			continue
		}

		if resp.StatusCode >= http.StatusBadRequest {
			respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			slog.Debug("LLM client error", "status", resp.StatusCode, "body", string(respBody))
			return nil, &api.APIError{StatusCode: resp.StatusCode, Message: "client error", Body: string(respBody)}
		}

		return resp, nil
	}

	return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}

// doRequest performs a non-streaming request with retries.
func (c *Client) doRequest(ctx context.Context, reqBody chatCompletionRequest) ([]byte, error) {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	resp, err := c.doRequestWithRetry(ctx, body, false)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	return respBody, nil
}

// doRequestStream performs a streaming request with retries.
// On success it returns the response body for reading SSE events.
func (c *Client) doRequestStream(ctx context.Context, reqBody chatCompletionRequest) (io.ReadCloser, error) {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	resp, err := c.doRequestWithRetry(ctx, body, true)
	if err != nil {
		return nil, err
	}

	return resp.Body, nil
}

// backoff returns the delay before a retry attempt.
func (c *Client) backoff(attempt int) time.Duration {
	delay := time.Duration(math.Pow(2, float64(attempt-1))) * time.Second
	if delay > maxBackoffDelay {
		delay = maxBackoffDelay
	}
	return delay
}

// isRetryableError reports whether an error warrants a retry.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	return false
}

// buildChatRequest constructs the API request payload from api types.
func (c *Client) buildChatRequest(messages []api.Message, tools []api.ToolDefinition, stream bool) chatCompletionRequest {
	req := chatCompletionRequest{
		Model:    c.model,
		Messages: make([]chatMessage, 0, len(messages)),
		Stream:   stream,
	}

	for _, msg := range messages {
		cm := chatMessage{
			Role:    string(msg.Role),
			Content: msg.Content,
		}
		if msg.Role == api.RoleTool {
			cm.ToolCallID = msg.ToolCallID
		}
		if len(msg.ToolCalls) > 0 {
			cm.ToolCalls = make([]toolCall, 0, len(msg.ToolCalls))
			for _, tc := range msg.ToolCalls {
				cm.ToolCalls = append(cm.ToolCalls, toolCall{
					ID:   tc.ID,
					Type: "function",
					Function: function{
						Name:      tc.Name,
						Arguments: tc.Arguments,
					},
				})
			}
		}
		req.Messages = append(req.Messages, cm)
	}

	if len(tools) > 0 {
		req.Tools = make([]toolDef, 0, len(tools))
		for _, t := range tools {
			req.Tools = append(req.Tools, toolDef{
				Type: "function",
				Function: functionDef{
					Name:        t.Name,
					Description: t.Description,
					Parameters:  t.Parameters,
				},
			})
		}
	}

	return req
}

// ---------------------------------------------------------------------------
// OpenAI API types
// ---------------------------------------------------------------------------

type chatCompletionRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Tools    []toolDef     `json:"tools,omitempty"`
	Stream   bool          `json:"stream"`
}

type chatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type toolDef struct {
	Type     string      `json:"type"`
	Function functionDef `json:"function"`
}

type functionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Role      string     `json:"role"`
			Content   string     `json:"content"`
			ToolCalls []toolCall `json:"tool_calls,omitempty"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

type toolCall struct {
	ID       string   `json:"id"`
	Type     string   `json:"type"`
	Function function `json:"function"`
}

type function struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}
