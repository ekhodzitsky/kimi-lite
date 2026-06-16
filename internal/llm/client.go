// Package llm provides an OpenAI-compatible LLM client with SSE streaming support.
package llm

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/ekhodzitsky/kimi-lite/internal/netutil"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

const (
	defaultTimeout  = 60 * time.Second
	defaultAttempts = 3 // total request attempts (initial + retries)
	maxBackoffDelay = 30 * time.Second
)

// ErrEmptyResponse is returned when the LLM API returns a 200 OK with no choices.
var ErrEmptyResponse = errors.New("empty response from API")

// Client implements api.LLMClient for OpenAI-compatible APIs.
type Client struct {
	httpClient   *http.Client
	baseURL      string
	endpoint     string
	apiKey       string
	model        string
	timeout      time.Duration
	maxAttempts  int
	extraHeaders http.Header
	metrics      api.MetricsCollector
	mu           sync.RWMutex
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
	endpoint, _ := url.JoinPath(cfg.BaseURL, "chat/completions")
	if endpoint == "" {
		endpoint = cfg.BaseURL + "/chat/completions"
	}
	return &Client{
		httpClient:  httpClient,
		baseURL:     cfg.BaseURL,
		endpoint:    endpoint,
		apiKey:      cfg.APIKey,
		model:       cfg.Model,
		timeout:     timeout,
		maxAttempts: defaultAttempts,
		metrics:     api.NoopMetricsCollector{},
	}
}

// SetMetricsCollector sets the metrics collector.
// Safe for concurrent use with Chat/ChatStream.
func (c *Client) SetMetricsCollector(m api.MetricsCollector) {
	if m == nil {
		m = api.NoopMetricsCollector{}
	}
	c.mu.Lock()
	c.metrics = m
	c.mu.Unlock()
}

// SetHeaders replaces any custom headers sent with each request.
// Safe for concurrent use with Chat/ChatStream.
func (c *Client) SetHeaders(headers map[string]string) {
	c.mu.Lock()
	c.extraHeaders = make(http.Header, len(headers))
	for k, v := range headers {
		c.extraHeaders.Set(k, v)
	}
	c.mu.Unlock()
}

// Chat sends messages to the LLM and returns the complete response.
func (c *Client) Chat(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (*api.Message, error) {
	start := time.Now()
	c.mu.RLock()
	metrics := c.metrics
	extraHeaders := c.extraHeaders.Clone()
	c.mu.RUnlock()
	metrics.IncCounter("llm.chat")

	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	reqBody := c.buildChatRequest(messages, tools, false)
	respBody, err := c.doRequest(ctx, reqBody, extraHeaders)
	if err != nil {
		metrics.RecordError("llm.chat")
		metrics.RecordLatency("llm.chat.latency", time.Since(start))
		return nil, fmt.Errorf("chat request failed: %w", err)
	}

	var resp chatCompletionResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("decode chat response: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, ErrEmptyResponse
	}

	choice := resp.Choices[0].Message
	role := api.Role(choice.Role)
	if role == "" {
		role = api.RoleAssistant
	}
	msg := &api.Message{
		Role:         role,
		Content:      choice.Content,
		FinishReason: resp.Choices[0].FinishReason,
		CreatedAt:    time.Now().UTC(),
	}
	if msg.FinishReason == "length" || msg.FinishReason == "content_filter" {
		slog.Warn("LLM response truncated", "finish_reason", msg.FinishReason)
	}

	metrics.RecordLatency("llm.chat.latency", time.Since(start))

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
// The streaming goroutine owns the HTTP response body and closes it via defer body.Close().
func (c *Client) ChatStream(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
	start := time.Now()
	c.mu.RLock()
	metrics := c.metrics
	extraHeaders := c.extraHeaders.Clone()
	c.mu.RUnlock()
	metrics.IncCounter("llm.stream")

	ctx, cancel := context.WithCancel(ctx)

	reqBody := c.buildChatRequest(messages, tools, true)
	body, err := c.doRequestStream(ctx, reqBody, extraHeaders)
	if err != nil {
		metrics.RecordError("llm.stream")
		metrics.RecordLatency("llm.stream.first_byte_latency", time.Since(start))
		cancel()
		return nil, fmt.Errorf("chat stream request failed: %w", err)
	}

	ch := make(chan api.StreamChunk, 64)
	metrics.RecordLatency("llm.stream.first_byte_latency", time.Since(start))
	go func() {
		defer close(ch)
		defer cancel()

		// Ensure the response body is closed exactly once, either when the
		// stream goroutine exits or when the caller cancels the context.
		// Closing on cancellation is required to unblock the scanner goroutine
		// when the consumer stops reading from ch and prevents connection leaks.
		var closeBodyOnce sync.Once
		closeBody := func() { _ = body.Close() }
		defer closeBodyOnce.Do(closeBody)
		go func() {
			<-ctx.Done()
			closeBodyOnce.Do(closeBody)
		}()

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
				raw, err := reader.readRawChunk()
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
				if errors.Is(err, io.EOF) {
					select {
					case ch <- api.StreamChunk{Done: true, ToolCalls: sortedToolCalls(accumulator), FinishReason: raw.FinishReason}:
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
					if raw.FinishReason == "length" || raw.FinishReason == "content_filter" {
						slog.Warn("LLM stream truncated", "finish_reason", raw.FinishReason)
					}
					// Emit any terminal content before the done chunk so callers don't lose it.
					if raw.Content != "" {
						select {
						case ch <- api.StreamChunk{Content: raw.Content}:
						case <-ctx.Done():
							return
						}
					}
					select {
					case ch <- api.StreamChunk{Done: true, ToolCalls: sortedToolCalls(accumulator), FinishReason: raw.FinishReason}:
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

// doRequestWithRetry performs the HTTP request with up to c.maxAttempts total
// attempts (including the initial request) and returns the raw response.
func (c *Client) doRequestWithRetry(ctx context.Context, body []byte, stream bool, extraHeaders http.Header) (*http.Response, error) {
	var lastErr error
	var retryAfterDelay time.Duration
	for attempt := 0; attempt < c.maxAttempts; attempt++ {
		if attempt > 0 {
			delay := c.backoff(attempt)
			if retryAfterDelay > delay {
				delay = retryAfterDelay
			}
			if delay > maxBackoffDelay {
				delay = maxBackoffDelay
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
			retryAfterDelay = 0
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		if stream {
			req.Header.Set("Accept", "text/event-stream")
		}
		for k, vals := range extraHeaders {
			for _, v := range vals {
				req.Header.Add(k, v)
			}
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
			retryAfterDelay = parseRetryAfter(resp.Header.Get("Retry-After"))
			_ = resp.Body.Close()
			slog.Debug("LLM server error", "status", resp.StatusCode, "body", string(respBody), "retry_after", retryAfterDelay)
			lastErr = &api.APIError{StatusCode: resp.StatusCode, Message: "server error", Body: string(respBody)}
			continue
		}

		if resp.StatusCode >= http.StatusBadRequest {
			respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
			slog.Debug("LLM client error", "status", resp.StatusCode, "body", string(respBody))
			apiErr := &api.APIError{StatusCode: resp.StatusCode, Message: "client error", Body: string(respBody)}
			return nil, fmt.Errorf("LLM client error: %w", apiErr)
		}

		return resp, nil
	}

	return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}

// doRequest performs a non-streaming request with retries.
func (c *Client) doRequest(ctx context.Context, reqBody chatCompletionRequest, extraHeaders http.Header) ([]byte, error) {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	resp, err := c.doRequestWithRetry(ctx, body, false, extraHeaders)
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
// The caller takes ownership of the returned body and is responsible for closing it.
func (c *Client) doRequestStream(ctx context.Context, reqBody chatCompletionRequest, extraHeaders http.Header) (io.ReadCloser, error) {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	resp, err := c.doRequestWithRetry(ctx, body, true, extraHeaders)
	if err != nil {
		return nil, err
	}

	return resp.Body, nil //nolint:bodyclose // ownership transferred to streaming goroutine, closed in the ChatStream goroutine
}

// parseRetryAfter parses the Retry-After header value, supporting both
// integer-seconds and HTTP-date forms. It returns 0 for invalid values.
func parseRetryAfter(value string) time.Duration {
	if value == "" {
		return 0
	}
	// Try integer seconds first.
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds < 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	// Fall back to HTTP-date parsing.
	if t, err := http.ParseTime(value); err == nil {
		d := time.Until(t)
		if d < 0 {
			return 0
		}
		return d
	}
	return 0
}

// backoff returns the delay before a retry attempt.
func (c *Client) backoff(attempt int) time.Duration {
	shift := attempt - 1
	if shift < 0 {
		shift = 0
	}
	if shift > 31 {
		shift = 31
	}
	maxDelay := min(time.Duration(1<<shift)*time.Second, maxBackoffDelay)
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return maxDelay
	}
	val := binary.BigEndian.Uint64(buf[:])
	// #nosec G115 -- maxDelay is bounded by maxBackoffDelay (30s) and non-negative.
	return time.Duration(val % (uint64(maxDelay) + 1))
}

// isRetryableError reports whether an error warrants a retry.
// Transient errors (connection resets, unexpected EOF) are retried.
// Permanent errors (connection refused, DNS not found) and caller context
// errors (cancellation, deadline exceeded) are not.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	if errors.Is(err, syscall.ECONNRESET) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}
	return false
}

// messageContent returns the OpenAI content value for a message: a plain
// string when no content parts are present, otherwise an array of text/image
// parts. The message's plain-text Content is always included as the first text
// part so it is not lost when images are attached.
func messageContent(msg api.Message) any {
	if len(msg.ContentParts) == 0 {
		return msg.Content
	}
	parts := make([]contentPart, 0, len(msg.ContentParts)+1)
	if msg.Content != "" {
		parts = append(parts, contentPart{Type: "text", Text: msg.Content})
	}
	for _, p := range msg.ContentParts {
		switch p.Type {
		case api.ContentPartText:
			parts = append(parts, contentPart{Type: "text", Text: p.Text})
		case api.ContentPartImageURL:
			detail := "auto"
			if p.ImageURL != nil && p.ImageURL.Detail != "" {
				detail = p.ImageURL.Detail
			}
			url := ""
			if p.ImageURL != nil {
				url = p.ImageURL.URL
			}
			parts = append(parts, contentPart{Type: "image_url", ImageURL: &imageURLPart{URL: url, Detail: detail}})
		default:
			parts = append(parts, contentPart{Type: "text", Text: p.Text})
		}
	}
	return parts
}

// buildChatRequest constructs the API request payload from api types.
func (c *Client) buildChatRequest(messages []api.Message, tools []api.ToolDefinition, stream bool) chatCompletionRequest {
	req := chatCompletionRequest{
		Model:     c.model,
		Messages:  make([]chatMessage, 0, len(messages)),
		Stream:    stream,
		MaxTokens: LookupModel(c.model).MaxTokens,
	}

	for _, msg := range messages {
		cm := chatMessage{
			Role:    string(msg.Role),
			Content: messageContent(msg),
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
	Model     string        `json:"model"`
	Messages  []chatMessage `json:"messages"`
	Tools     []toolDef     `json:"tools,omitempty"`
	Stream    bool          `json:"stream"`
	MaxTokens int           `json:"max_tokens,omitempty"`
}

type chatMessage struct {
	Role       string     `json:"role"`
	Content    any        `json:"content"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type contentPart struct {
	Type     string        `json:"type"`
	Text     string        `json:"text,omitempty"`
	ImageURL *imageURLPart `json:"image_url,omitempty"`
}

type imageURLPart struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
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
