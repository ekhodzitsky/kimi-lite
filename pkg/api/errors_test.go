package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestAPIError_Error(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  *APIError
		want string
	}{
		{
			name: "with message",
			err:  &APIError{StatusCode: http.StatusBadRequest, Message: "invalid request"},
			want: "API error 400: invalid request",
		},
		{
			name: "without message",
			err:  &APIError{StatusCode: http.StatusInternalServerError},
			want: "API error 500",
		},
		{
			name: "with body",
			err:  &APIError{StatusCode: http.StatusBadRequest, Message: "invalid request", Body: "details"},
			want: "API error 400: invalid request: details",
		},
		{
			name: "truncates long body",
			err:  &APIError{StatusCode: http.StatusBadRequest, Body: strings.Repeat("x", 300)},
			want: "API error 400: " + strings.Repeat("x", 256) + "...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAPIError_Classifiers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		statusCode    int
		wantClient    bool
		wantServer    bool
		wantRateLimit bool
	}{
		{"2xx success", http.StatusOK, false, false, false},
		{"4xx client error", http.StatusBadRequest, true, false, false},
		{"401 unauthorized", http.StatusUnauthorized, true, false, false},
		{"429 rate limit", http.StatusTooManyRequests, true, false, true},
		{"5xx server error", http.StatusInternalServerError, false, true, false},
		{"503 unavailable", http.StatusServiceUnavailable, false, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := &APIError{StatusCode: tt.statusCode}
			if got := err.IsClientError(); got != tt.wantClient {
				t.Errorf("IsClientError() = %v, want %v", got, tt.wantClient)
			}
			if got := err.IsServerError(); got != tt.wantServer {
				t.Errorf("IsServerError() = %v, want %v", got, tt.wantServer)
			}
			if got := err.IsRateLimit(); got != tt.wantRateLimit {
				t.Errorf("IsRateLimit() = %v, want %v", got, tt.wantRateLimit)
			}
		})
	}
}

func TestAPIError_ErrorsAs(t *testing.T) {
	t.Parallel()

	inner := &APIError{StatusCode: http.StatusBadRequest, Message: "bad"}
	wrapped := fmt.Errorf("wrapped: %w", inner)
	doubleWrapped := fmt.Errorf("outer: %w", wrapped)

	var apiErr *APIError
	if !errors.As(doubleWrapped, &apiErr) {
		t.Fatal("expected errors.As to find *APIError through wrapping")
	}
	if apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, http.StatusBadRequest)
	}
}
