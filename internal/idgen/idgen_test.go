package idgen

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestGenerateID(t *testing.T) {
	t.Parallel()

	id := GenerateID()
	if len(id) != 32 {
		t.Errorf("expected 32 chars, got %d", len(id))
	}
	if strings.ToLower(id) != id {
		t.Errorf("expected lowercase hex, got %q", id)
	}
	if _, err := hex.DecodeString(id); err != nil {
		t.Errorf("expected valid hex, got %q: %v", id, err)
	}

	// Rough uniqueness check across a modest batch.
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		next := GenerateID()
		if seen[next] {
			t.Fatalf("duplicate ID generated: %s", next)
		}
		seen[next] = true
	}
}

func TestGenerateIDError(t *testing.T) {
	t.Parallel()

	id, err := GenerateIDError()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(id) != 32 {
		t.Errorf("expected 32 chars, got %d", len(id))
	}
	if _, err := hex.DecodeString(id); err != nil {
		t.Errorf("expected valid hex, got %q: %v", id, err)
	}
}

func BenchmarkGenerateID(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = GenerateID()
	}
}

func BenchmarkGenerateIDError(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _ = GenerateIDError()
	}
}
