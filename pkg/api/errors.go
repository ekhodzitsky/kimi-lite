package api

import "fmt"

// APIError represents an error response from an LLM API.
// Callers can use errors.As to inspect status code and response body.
type APIError struct {
	StatusCode int
	Message    string
	Body       string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("API error %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("API error %d", e.StatusCode)
}

// IsClientError reports whether the error is a 4xx status code.
func (e *APIError) IsClientError() bool {
	return e.StatusCode >= 400 && e.StatusCode < 500
}

// IsServerError reports whether the error is a 5xx status code.
func (e *APIError) IsServerError() bool {
	return e.StatusCode >= 500
}

// IsRateLimit reports whether the error is a 429 Too Many Requests.
func (e *APIError) IsRateLimit() bool {
	return e.StatusCode == 429
}
