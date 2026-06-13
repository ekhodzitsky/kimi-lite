package observability

import (
	"context"
	"fmt"
	"net/http"
	"net/http/pprof"
	"time"
)

// StartPprof starts an HTTP server on addr exposing net/http/pprof endpoints.
// The server shuts down gracefully when ctx is cancelled. An empty addr is a
// no-op and returns nil immediately.
func StartPprof(ctx context.Context, addr string) error {
	if addr == "" {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		shutdownErr := srv.Shutdown(shutdownCtx)
		<-errCh // Wait for ListenAndServe to return to avoid leaking the goroutine.
		if shutdownErr != nil {
			return fmt.Errorf("pprof server shutdown failed: %w", shutdownErr)
		}
		return nil
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return fmt.Errorf("pprof server failed: %w", err)
	}
}
