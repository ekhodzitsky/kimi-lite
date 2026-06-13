package tests

import (
	"testing"

	"go.uber.org/goleak"
)

// TestNoGoroutineLeaks verifies that the test binary does not leak goroutines
// after the package-level smoke tests complete. It is intentionally broad so
// that regressions in background goroutines (pprof, MCP, turn loops) are caught.
func TestNoGoroutineLeaks(t *testing.T) {
	goleak.VerifyNone(t)
}
