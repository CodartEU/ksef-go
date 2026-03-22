package ksef

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// Client is the entry point for all KSeF API operations. Create one with
// [NewClient] and then use it to open sessions, submit invoices, and query
// invoice status.
//
// Client is safe for concurrent use by multiple goroutines.
type Client struct {
	env        Environment
	baseURL    string
	httpClient *http.Client
	logger     *slog.Logger
	retry      retryConfig
}

// NewClient creates a Client targeting the given environment. Zero or more
// [Option] values can be passed to override defaults (HTTP client, logger,
// retry behaviour).
//
// Example:
//
//	client, err := ksef.NewClient(ksef.Test,
//	    ksef.WithLogger(slog.Default()),
//	    ksef.WithRetryConfig(5, time.Second),
//	)
func NewClient(env Environment, opts ...Option) (*Client, error) {
	baseURL, ok := baseURLs[env]
	if !ok {
		return nil, fmt.Errorf("ksef: unknown environment %d", int(env))
	}

	c := &Client{
		env:     env,
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: slog.Default(),
		retry:  defaultRetryConfig,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c, nil
}

// Environment returns the environment this client is configured for.
func (c *Client) Environment() Environment { return c.env }

// BaseURL returns the API base URL this client will use for all requests.
func (c *Client) BaseURL() string { return c.baseURL }
