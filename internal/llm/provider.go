package llm

import (
	"fmt"
	"net/http"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// NewClientFromConfig creates an api.LLMClient from the new provider/model-table
// configuration, falling back to the legacy single-provider cfg.LLM when no
// providers table is configured.
func NewClientFromConfig(cfg *api.Config, httpClient *http.Client) (api.LLMClient, error) {
	providerName, provider, err := resolveDefaultProvider(cfg)
	if err != nil {
		return nil, fmt.Errorf("resolve default provider: %w", err)
	}

	switch provider.Type {
	case string(api.ProviderTypeAnthropic), string(api.ProviderTypeGoogleGenAI):
		return nil, fmt.Errorf("provider %q is not yet supported", provider.Type)
	}

	model := resolveDefaultModel(cfg, provider)
	llmCfg := api.LLMConfig{
		Provider: providerName,
		APIKey:   provider.APIKey,
		Model:    model,
		BaseURL:  provider.BaseURL,
		Timeout:  cfg.LLM.Timeout,
	}

	client := NewClient(llmCfg, httpClient)
	if len(provider.CustomHeaders) > 0 {
		client.SetHeaders(provider.CustomHeaders)
	}

	// Preserve the legacy fallback path when providers are not configured.
	if len(cfg.Providers) == 0 && cfg.LLM.Fallback != nil {
		fallback := NewClient(*cfg.LLM.Fallback, httpClient)
		return NewFallbackClient(client, fallback), nil
	}

	return client, nil
}

// ResolveProviderFromConfig returns the name and configuration of the default
// LLM provider. It is exported so the CLI and doctor commands can validate the
// resolved API key without duplicating resolution logic.
func ResolveProviderFromConfig(cfg *api.Config) (string, api.ProviderConfig, error) {
	return resolveDefaultProvider(cfg)
}

// ResolveModelFromConfig returns the concrete model name that will be used for
// the default provider, taking the optional model alias table into account.
func ResolveModelFromConfig(cfg *api.Config) (string, error) {
	_, provider, err := resolveDefaultProvider(cfg)
	if err != nil {
		return "", err
	}
	return resolveDefaultModel(cfg, provider), nil
}

func resolveDefaultProvider(cfg *api.Config) (string, api.ProviderConfig, error) {
	if len(cfg.Providers) == 0 {
		return cfg.LLM.Provider, api.ProviderConfig{
			Type:         cfg.LLM.Provider,
			APIKey:       cfg.LLM.APIKey,
			BaseURL:      cfg.LLM.BaseURL,
			DefaultModel: cfg.LLM.Model,
		}, nil
	}

	if cfg.DefaultProvider == "" {
		return "", api.ProviderConfig{}, fmt.Errorf("default_provider must be set when providers is configured")
	}
	provider, ok := cfg.Providers[cfg.DefaultProvider]
	if !ok {
		return "", api.ProviderConfig{}, fmt.Errorf("default_provider %q not found in providers", cfg.DefaultProvider)
	}
	return cfg.DefaultProvider, provider, nil
}

func resolveDefaultModel(cfg *api.Config, provider api.ProviderConfig) string {
	if cfg.DefaultModel != "" {
		if alias, ok := cfg.Models[cfg.DefaultModel]; ok {
			return alias.Model
		}
		return cfg.DefaultModel
	}
	return provider.DefaultModel
}
