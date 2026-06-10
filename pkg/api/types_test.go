package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLLMConfig_APIKey_JSONOmit(t *testing.T) {
	t.Parallel()

	cfg := LLMConfig{
		Provider: "moonshot",
		APIKey:   "super-secret-key",
		Model:    "kimi-k2.5",
		BaseURL:  "https://api.example.com",
	}

	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if strings.Contains(string(b), "super-secret-key") {
		t.Errorf("marshaled JSON contains the API key: %s", b)
	}

	// Fallback chain is also covered because Fallback is *LLMConfig.
	cfg.Fallback = &LLMConfig{APIKey: "fallback-secret"}
	b, err = json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal with fallback: %v", err)
	}
	if strings.Contains(string(b), "fallback-secret") {
		t.Errorf("marshaled JSON contains the fallback API key: %s", b)
	}
}
