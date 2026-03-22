package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/CodartEU/ksef-go/pkg/ksef"
)

// Client wraps a *http.Client with retry logic, structured logging, and
// automatic KSeF error parsing.
type Client struct {
	baseURL    string
	httpClient *http.Client
	logger     *slog.Logger
	retry      RetryConfig
}

// New creates a Client.
//
//   - baseURL is prepended to every path passed to Get/Post/Put/Delete.
//   - hc is the underlying HTTP client; if nil, http.DefaultClient is used.
//   - logger is used for debug-level request logs; if nil, logging is disabled.
//   - retry controls retry behaviour.
func New(baseURL string, hc *http.Client, logger *slog.Logger, retry RetryConfig) *Client {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &Client{
		baseURL:    baseURL,
		httpClient: hc,
		logger:     logger,
		retry:      retry,
	}
}

// Get performs an HTTP GET to baseURL+path with the supplied extra headers.
func (c *Client) Get(ctx context.Context, path string, headers map[string]string) ([]byte, error) {
	return c.do(ctx, http.MethodGet, path, nil, headers)
}

// Post performs an HTTP POST to baseURL+path, serialising body as JSON when it
// is non-nil.
func (c *Client) Post(ctx context.Context, path string, body any, headers map[string]string) ([]byte, error) {
	return c.do(ctx, http.MethodPost, path, body, headers)
}

// Put performs an HTTP PUT to baseURL+path, serialising body as JSON when it
// is non-nil.
func (c *Client) Put(ctx context.Context, path string, body any, headers map[string]string) ([]byte, error) {
	return c.do(ctx, http.MethodPut, path, body, headers)
}

// Delete performs an HTTP DELETE to baseURL+path with the supplied extra
// headers.
func (c *Client) Delete(ctx context.Context, path string, headers map[string]string) ([]byte, error) {
	return c.do(ctx, http.MethodDelete, path, nil, headers)
}

// PostRaw performs an HTTP POST to baseURL+path with a raw byte body and the
// given Content-Type (e.g. "application/xml"). Unlike Post, the body is sent
// verbatim without JSON encoding.
func (c *Client) PostRaw(ctx context.Context, path, contentType string, body []byte, headers map[string]string) ([]byte, error) {
	return c.execute(ctx, http.MethodPost, path, contentType, body, headers)
}

// do is the single entry-point that all method helpers delegate to. It
// encodes the request body, executes the request with retry logic, logs each
// attempt, and converts non-2xx responses into typed errors.
func (c *Client) do(ctx context.Context, method, path string, body any, headers map[string]string) ([]byte, error) {
	var rawBody []byte
	if body != nil {
		var err error
		rawBody, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("httpclient: marshal request body: %w", err)
		}
	}
	return c.execute(ctx, method, path, "application/json", rawBody, headers)
}

// execute performs an HTTP request with retry logic, logging, and error
// parsing. rawBody is sent verbatim; contentType is only set when rawBody is
// non-nil.
func (c *Client) execute(ctx context.Context, method, path, contentType string, rawBody []byte, headers map[string]string) ([]byte, error) {
	url := c.baseURL + path

	var (
		respBody []byte
		lastErr  error
	)

	for attempt := 0; attempt <= c.retry.MaxRetries; attempt++ {
		var bodyReader io.Reader
		if rawBody != nil {
			bodyReader = bytes.NewReader(rawBody)
		}

		req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
		if err != nil {
			return nil, fmt.Errorf("httpclient: build request: %w", err)
		}

		if rawBody != nil {
			req.Header.Set("Content-Type", contentType)
		}
		req.Header.Set("Accept", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		start := time.Now()
		resp, err := c.httpClient.Do(req)
		elapsed := time.Since(start)

		if err != nil {
			c.logRequest(ctx, method, path, 0, elapsed, attempt)
			lastErr = fmt.Errorf("httpclient: %s %s: %w", method, path, err)
			// Network errors: sleep exponential delay then retry if budget remains.
			if attempt < c.retry.MaxRetries {
				if serr := sleepContext(ctx, c.backoffDelay(attempt+1)); serr != nil {
					return nil, fmt.Errorf("httpclient: retry cancelled: %w", serr)
				}
			}
			continue
		}

		respBody, err = io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("httpclient: read response body: %w", err)
		}

		c.logRequest(ctx, method, path, resp.StatusCode, elapsed, attempt)

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return respBody, nil
		}

		// Map non-2xx response to a typed error.
		apiErr := parseError(resp.StatusCode, resp, respBody)

		if !shouldRetry(resp.StatusCode) || attempt == c.retry.MaxRetries {
			return respBody, apiErr
		}

		// Determine sleep duration before the next attempt.
		// 429: honour Retry-After header; 5xx: exponential back-off.
		var delay time.Duration
		if resp.StatusCode == http.StatusTooManyRequests {
			delay = retryAfterDelay(resp, c.backoffDelay(attempt+1))
		} else {
			delay = c.backoffDelay(attempt + 1)
		}

		if serr := sleepContext(ctx, delay); serr != nil {
			return nil, fmt.Errorf("httpclient: retry cancelled: %w", serr)
		}

		lastErr = apiErr
	}

	if lastErr != nil {
		return respBody, lastErr
	}
	return respBody, nil
}

// backoffDelay returns the exponential back-off delay for the given attempt
// number (1-based: attempt 1 → baseDelay, attempt 2 → 2×baseDelay, …).
func (c *Client) backoffDelay(attempt int) time.Duration {
	d := c.retry.BaseDelay
	for i := 1; i < attempt; i++ {
		d *= 2
	}
	return d
}

// logRequest emits a debug-level log line, if a logger is configured.
func (c *Client) logRequest(ctx context.Context, method, path string, status int, elapsed time.Duration, attempt int) {
	if c.logger == nil {
		return
	}
	args := []any{
		slog.String("method", method),
		slog.String("path", path),
		slog.Duration("duration", elapsed),
	}
	if status != 0 {
		args = append(args, slog.Int("status", status))
	}
	if attempt > 0 {
		args = append(args, slog.Int("attempt", attempt+1))
	}
	c.logger.DebugContext(ctx, "httpclient: request", args...)
}

// exceptionResponse mirrors the KSeF API's ExceptionResponse JSON shape.
type exceptionResponse struct {
	Exception *exceptionInfo `json:"exception"`
}

type exceptionInfo struct {
	ExceptionDetailList []exceptionDetail `json:"exceptionDetailList"`
	ReferenceNumber     string            `json:"referenceNumber"`
}

type exceptionDetail struct {
	ExceptionCode        int32    `json:"exceptionCode"`
	ExceptionDescription string   `json:"exceptionDescription"`
	Details              []string `json:"details"`
}

// parseError converts a non-2xx response into the appropriate typed error.
func parseError(status int, resp *http.Response, body []byte) error {
	ksefErr := &ksef.KSeFError{HTTPStatus: status}

	var env exceptionResponse
	if json.Unmarshal(body, &env) == nil && env.Exception != nil {
		ksefErr.ReferenceNumber = env.Exception.ReferenceNumber
		for _, d := range env.Exception.ExceptionDetailList {
			ksefErr.Exceptions = append(ksefErr.Exceptions, ksef.ExceptionDetail{
				ExceptionCode:        d.ExceptionCode,
				ExceptionDescription: d.ExceptionDescription,
				Details:              d.Details,
			})
		}
	}

	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return &ksef.AuthenticationError{Cause: ksefErr}
	case http.StatusTooManyRequests:
		return &ksef.RateLimitError{
			RetryAfter: retryAfterDelay(resp, 0),
		}
	default:
		return ksefErr
	}
}
