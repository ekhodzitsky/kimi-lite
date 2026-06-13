package observability

import (
	"context"
	"testing"
	"time"
)

func TestStartPprofSmoke(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addr := "127.0.0.1:0"
	started := make(chan error, 1)
	go func() {
		started <- StartPprof(ctx, addr)
	}()

	// Wait for the server to be reachable. Port 0 is not exposed, so we rely on
	// the fact that the function blocks until the context is cancelled.
	time.Sleep(50 * time.Millisecond)

	cancel()

	select {
	case err := <-started:
		if err != nil {
			t.Fatalf("StartPprof returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("StartPprof did not return after context cancellation")
	}
}

func TestStartPprofEmptyAddr(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := StartPprof(ctx, ""); err != nil {
		t.Fatalf("empty addr should return nil, got %v", err)
	}
}

func TestStartPprofCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := StartPprof(ctx, "127.0.0.1:0"); err != nil {
		t.Fatalf("cancelled context should return nil, got %v", err)
	}
}
