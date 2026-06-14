package idgen

import (
	"encoding/hex"
	"errors"
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

func TestGenerateIDError_ReadFailure(t *testing.T) {
	mock := func(b []byte) (int, error) { return 0, errors.New("mock read error") }
	oldRandRead := swapRandRead(mock)
	defer swapRandRead(oldRandRead)

	id, err := GenerateIDError()
	if err == nil {
		t.Fatalf("expected error, got id %q", id)
	}
	if id != "" {
		t.Errorf("expected empty id on error, got %q", id)
	}
	if !strings.Contains(err.Error(), "mock read error") {
		t.Errorf("expected error to mention mock read error, got %v", err)
	}
}

func TestGenerateID_PanicOnReadFailure(t *testing.T) {
	mock := func(b []byte) (int, error) { return 0, errors.New("mock read error") }
	oldRandRead := swapRandRead(mock)
	defer swapRandRead(oldRandRead)

	var recovered bool
	func() {
		defer func() {
			r := recover()
			if r == nil {
				t.Fatalf("expected panic, got nil")
			}
			msg, ok := r.(string)
			if !ok {
				t.Fatalf("expected string panic, got %T", r)
			}
			if !strings.Contains(msg, "mock read error") {
				t.Errorf("expected panic message to mention mock read error, got %q", msg)
			}
			recovered = true
		}()
		_ = GenerateID()
	}()

	if !recovered {
		t.Fatalf("GenerateID did not panic")
	}
}

func swapRandRead(f func([]byte) (int, error)) func([]byte) (int, error) {
	randReadMu.Lock()
	defer randReadMu.Unlock()
	old := randRead
	randRead = f
	return old
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
