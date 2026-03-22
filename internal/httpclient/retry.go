// Package httpclient provides an HTTP client wrapper with retry logic and
// structured logging for use by the ksef package internals.
package httpclient

import (
	"context"
	"net/http"
	"strconv"
	"time"
)

// RetryConfig controls the retry behaviour of [Client].
type RetryConfig struct {
	// MaxRetries is the maximum number of additional attempts after the first
	// failure. 0 means no retries.
	MaxRetries int
	// BaseDelay is the initial back-off duration. Each subsequent retry doubles
	// it (exponential back-off).
	BaseDelay time.Duration
}

// DefaultRetryConfig is used when no explicit config is supplied.
var DefaultRetryConfig = RetryConfig{
	MaxRetries: 3,
	BaseDelay:  500 * time.Millisecond,
}

// shouldRetry reports whether a response with the given status code warrants
// another attempt.
func shouldRetry(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

// retryAfterDelay parses the Retry-After header from the response and returns
// the duration the caller should wait. It falls back to baseDelay when the
// header is absent or cannot be parsed.
func retryAfterDelay(resp *http.Response, baseDelay time.Duration) time.Duration {
	if resp == nil {
		return baseDelay
	}
	v := resp.Header.Get("Retry-After")
	if v == "" {
		return baseDelay
	}
	// Retry-After may be a delay-seconds integer or an HTTP-date; we only
	// handle the integer form here as KSeF uses seconds.
	if secs, err := strconv.ParseFloat(v, 64); err == nil && secs > 0 {
		return time.Duration(secs * float64(time.Second))
	}
	return baseDelay
}

// sleepContext sleeps for d, but returns early if ctx is cancelled.
func sleepContext(ctx context.Context, d time.Duration) error {
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
