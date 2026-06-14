package observability

import (
	"context"
	"fmt"
	"net"
	"net/http"
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

	err := StartPprof(ctx, "127.0.0.1:0")
	if err == nil {
		t.Fatal("cancelled context should return error, got nil")
	}
}

func TestStartPprofInvalidAddr(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := StartPprof(ctx, "127.0.0.1:abc")
	if err == nil {
		t.Fatal("invalid addr should return error, got nil")
	}
}

func TestStartPprofHTTPRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Use a fixed loopback port chosen by the OS.
	addr := "127.0.0.1:0"
	started := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		// StartPprof only returns the error; to learn the actual bound port we
		// spin up a tiny helper server on the same address first. Since we pass
		// ":0", the OS assigns an unused port, then we close it and pass that
		// concrete port to StartPprof. There is a small race where another
		// process could bind the port between close and StartPprof's bind; the
		// test retries the HTTP request to tolerate this.
		lis, err := net.Listen("tcp", addr)
		if err != nil {
			errCh <- err
			return
		}
		bound := lis.Addr().String()
		if closeErr := lis.Close(); closeErr != nil {
			errCh <- closeErr
			return
		}
		started <- bound
		errCh <- StartPprof(ctx, bound)
	}()

	var boundAddr string
	select {
	case boundAddr = <-started:
	case err := <-errCh:
		t.Fatalf("failed to start pprof server: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for pprof server to start")
	}

	url := fmt.Sprintf("http://%s/debug/pprof/", boundAddr)

	// Allow a brief moment for StartPprof to begin listening, then retry a few
	// times to tolerate the small bind race introduced by pre-reserving :0.
	time.Sleep(20 * time.Millisecond)
	var resp *http.Response
	var err error
	for i := 0; i < 20; i++ {
		resp, err = http.Get(url)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("GET %s failed: %v", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s returned status %d, want %d", url, resp.StatusCode, http.StatusOK)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("StartPprof returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("StartPprof did not return after context cancellation")
	}
}
