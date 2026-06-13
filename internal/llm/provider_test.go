package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func TestNewClientFromConfig_BackwardCompat(t *testing.T) {
	t.Parallel()

	cfg := &api.Config{
		LLM: api.LLMConfig{
			Provider: "moonshot",
			APIKey:   "test-key",
			Model:    "kimi-k2.5",
			BaseURL:  "https://api.moonshot.cn/v1",
			Timeout:  60 * time.Second,
		},
	}

	client, err := NewClientFromConfig(cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	c, ok := client.(*Client)
	if !ok {
		t.Fatalf("expected *Client, got %T", client)
	}
	if c.baseURL != cfg.LLM.BaseURL {
		t.Errorf("baseURL = %q, want %q", c.baseURL, cfg.LLM.BaseURL)
	}
	if c.model != cfg.LLM.Model {
		t.Errorf("model = %q, want %q", c.model, cfg.LLM.Model)
	}
	if c.apiKey != cfg.LLM.APIKey {
		t.Errorf("apiKey = %q, want %q", c.apiKey, cfg.LLM.APIKey)
	}
}

func TestNewClientFromConfig_ProvidersAndAlias(t *testing.T) {
	t.Parallel()

	cfg := &api.Config{
		LLM: api.LLMConfig{
			Timeout: 60 * time.Second,
		},
		Providers: map[string]api.ProviderConfig{
			"openai": {
				Type:         string(api.ProviderTypeOpenAI),
				APIKey:       "openai-key",
				BaseURL:      "https://api.openai.com/v1",
				DefaultModel: "gpt-4o",
			},
		},
		Models: map[string]api.ModelAlias{
			"smart": {
				Provider: "openai",
				Model:    "gpt-4o",
			},
		},
		DefaultProvider: "openai",
		DefaultModel:    "smart",
	}

	client, err := NewClientFromConfig(cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	c := client.(*Client)
	if c.baseURL != "https://api.openai.com/v1" {
		t.Errorf("baseURL = %q, want %q", c.baseURL, "https://api.openai.com/v1")
	}
	if c.model != "gpt-4o" {
		t.Errorf("model = %q, want %q", c.model, "gpt-4o")
	}
	if c.apiKey != "openai-key" {
		t.Errorf("apiKey = %q, want %q", c.apiKey, "openai-key")
	}
}

func TestNewClientFromConfig_RawDefaultModel(t *testing.T) {
	t.Parallel()

	cfg := &api.Config{
		LLM: api.LLMConfig{Timeout: 60 * time.Second},
		Providers: map[string]api.ProviderConfig{
			"kimi": {
				Type:         string(api.ProviderTypeKimi),
				APIKey:       "kimi-key",
				BaseURL:      "https://api.moonshot.cn/v1",
				DefaultModel: "kimi-k2.5",
			},
		},
		DefaultProvider: "kimi",
		DefaultModel:    "kimi-k2",
	}

	client, err := NewClientFromConfig(cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	c := client.(*Client)
	if c.model != "kimi-k2" {
		t.Errorf("model = %q, want %q", c.model, "kimi-k2")
	}
}

func TestNewClientFromConfig_UnsupportedProvider(t *testing.T) {
	t.Parallel()

	cfg := &api.Config{
		LLM: api.LLMConfig{Timeout: 60 * time.Second},
		Providers: map[string]api.ProviderConfig{
			"anthropic": {
				Type:         string(api.ProviderTypeAnthropic),
				APIKey:       "key",
				BaseURL:      "https://api.anthropic.com",
				DefaultModel: "claude-3",
			},
		},
		DefaultProvider: "anthropic",
	}

	_, err := NewClientFromConfig(cfg, nil)
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}
	if !contains(err.Error(), "not yet supported") {
		t.Errorf("error = %q, want not yet supported", err.Error())
	}
}

func TestNewClientFromConfig_CustomHeaders(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Custom"); got != "value" {
			t.Errorf("X-Custom = %q, want %q", got, "value")
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
					}{Role: "assistant", Content: "ok"},
					FinishReason: "stop",
				},
			},
		})
	}))
	defer server.Close()

	cfg := &api.Config{
		LLM: api.LLMConfig{Timeout: 5 * time.Second},
		Providers: map[string]api.ProviderConfig{
			"openai": {
				Type:         string(api.ProviderTypeOpenAI),
				APIKey:       "key",
				BaseURL:      server.URL,
				DefaultModel: "gpt-4o",
				CustomHeaders: map[string]string{
					"X-Custom": "value",
				},
			},
		},
		DefaultProvider: "openai",
	}

	client, err := NewClientFromConfig(cfg, server.Client())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = client.Chat(context.Background(), []api.Message{{Role: api.RoleUser, Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("unexpected chat error: %v", err)
	}
}

func TestNewClientFromConfig_Fallback(t *testing.T) {
	t.Parallel()

	cfg := &api.Config{
		LLM: api.LLMConfig{
			Provider: "moonshot",
			APIKey:   "primary-key",
			Model:    "kimi-k2.5",
			BaseURL:  "https://api.moonshot.cn/v1",
			Timeout:  60 * time.Second,
			Fallback: &api.LLMConfig{
				Provider: "openai",
				APIKey:   "fallback-key",
				Model:    "gpt-4o",
				BaseURL:  "https://api.openai.com/v1",
				Timeout:  60 * time.Second,
			},
		},
	}

	client, err := NewClientFromConfig(cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := client.(*FallbackClient); !ok {
		t.Fatalf("expected *FallbackClient, got %T", client)
	}
}

func TestResolveModelFromConfig(t *testing.T) {
	t.Parallel()

	cfg := &api.Config{
		LLM: api.LLMConfig{Timeout: 60 * time.Second},
		Providers: map[string]api.ProviderConfig{
			"openai": {
				Type:         string(api.ProviderTypeOpenAI),
				APIKey:       "key",
				BaseURL:      "https://api.openai.com/v1",
				DefaultModel: "gpt-4o",
			},
		},
		Models: map[string]api.ModelAlias{
			"fast": {Provider: "openai", Model: "gpt-4o-mini"},
		},
		DefaultProvider: "openai",
	}

	tests := []struct {
		name string
		cfg  *api.Config
		want string
	}{
		{
			name: "provider default",
			cfg:  cfg,
			want: "gpt-4o",
		},
		{
			name: "alias",
			cfg: func() *api.Config {
				c := *cfg
				c.DefaultModel = "fast"
				return &c
			}(),
			want: "gpt-4o-mini",
		},
		{
			name: "raw model",
			cfg: func() *api.Config {
				c := *cfg
				c.DefaultModel = "gpt-4-turbo"
				return &c
			}(),
			want: "gpt-4-turbo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ResolveModelFromConfig(tt.cfg)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("ResolveModelFromConfig() = %q, want %q", got, tt.want)
			}
		})
	}
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
