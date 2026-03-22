package ksef

import (
	"log/slog"
	"net/http"
	"time"
)

// retryConfig holds parameters that control the HTTP retry behaviour.
type retryConfig struct {
	maxRetries int
	baseDelay  time.Duration
}

// defaultRetryConfig is used when the caller does not supply WithRetryConfig.
var defaultRetryConfig = retryConfig{
	maxRetries: 3,
	baseDelay:  500 * time.Millisecond,
}

// Option is a functional option that configures a [Client].
type Option func(*Client)

// WithHTTPClient replaces the default HTTP client used for all API calls.
// The supplied client must not be nil.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.httpClient = hc
		}
	}
}

// WithLogger sets the structured logger used by the client for debug and
// informational output. Pass nil to disable logging entirely.
func WithLogger(l *slog.Logger) Option {
	return func(c *Client) {
		c.logger = l
	}
}

// WithRetryConfig controls how the client retries failed requests.
// maxRetries is the maximum number of additional attempts after the first
// failure (0 means no retries). baseDelay is the initial back-off duration;
// each subsequent retry doubles it.
func WithRetryConfig(maxRetries int, baseDelay time.Duration) Option {
	return func(c *Client) {
		c.retry = retryConfig{
			maxRetries: maxRetries,
			baseDelay:  baseDelay,
		}
	}
}
