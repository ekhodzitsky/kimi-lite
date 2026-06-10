// Package llm provides an OpenAI-compatible LLM client with SSE streaming support.
package llm

import (
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// modelConfig holds internal model configuration.
type modelConfig struct {
	name          string
	provider      string
	maxTokens     int
	contextWindow int
}

// registry is the global model registry mapping model names to configurations.
var registry = map[string]modelConfig{
	// Moonshot models
	"kimi-k2.5":        {name: "kimi-k2.5", provider: "moonshot", maxTokens: 8192, contextWindow: 256000},
	"kimi-k2":          {name: "kimi-k2", provider: "moonshot", maxTokens: 8192, contextWindow: 256000},
	"kimi-k1.5":        {name: "kimi-k1.5", provider: "moonshot", maxTokens: 8192, contextWindow: 256000},
	"moonshot-v1-8k":   {name: "moonshot-v1-8k", provider: "moonshot", maxTokens: 4096, contextWindow: 8192},
	"moonshot-v1-32k":  {name: "moonshot-v1-32k", provider: "moonshot", maxTokens: 4096, contextWindow: 32768},
	"moonshot-v1-128k": {name: "moonshot-v1-128k", provider: "moonshot", maxTokens: 4096, contextWindow: 131072},

	// OpenAI models
	"gpt-4o":        {name: "gpt-4o", provider: "openai", maxTokens: 4096, contextWindow: 128000},
	"gpt-4o-mini":   {name: "gpt-4o-mini", provider: "openai", maxTokens: 4096, contextWindow: 128000},
	"gpt-4-turbo":   {name: "gpt-4-turbo", provider: "openai", maxTokens: 4096, contextWindow: 128000},
	"gpt-4":         {name: "gpt-4", provider: "openai", maxTokens: 4096, contextWindow: 8192},
	"gpt-3.5-turbo": {name: "gpt-3.5-turbo", provider: "openai", maxTokens: 4096, contextWindow: 16385},
}

// modelInfo converts internal modelConfig to api.ModelInfo.
func modelInfo(m modelConfig) api.ModelInfo {
	return api.ModelInfo{
		Name:          m.name,
		Provider:      m.provider,
		MaxTokens:     m.maxTokens,
		ContextWindow: m.contextWindow,
	}
}

// AllModels returns all registered model configurations.
func AllModels() []api.ModelInfo {
	models := make([]api.ModelInfo, 0, len(registry))
	for _, m := range registry {
		models = append(models, modelInfo(m))
	}
	return models
}

// LookupModel returns the model configuration for the given name.
// If the model is not found, it returns a generic configuration.
func LookupModel(name string) api.ModelInfo {
	if m, ok := registry[name]; ok {
		return modelInfo(m)
	}
	// Return a generic fallback for unknown models.
	return api.ModelInfo{
		Name:          name,
		Provider:      "unknown",
		MaxTokens:     4096,
		ContextWindow: 128000,
	}
}
