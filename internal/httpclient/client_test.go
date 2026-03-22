package httpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CodartEU/ksef-go/pkg/ksef"
)

// noopLogger returns a nil *slog.Logger so the client skips logging in tests.
func noLogger() interface {
	DebugContext(context.Context, string, ...any)
} {
	return nil
}

// newTestClient builds a Client pointing at the given test server URL with
// zero retries and a short base delay so tests complete quickly.
func newTestClient(t *testing.T, serverURL string, retry RetryConfig) *Client {
	t.Helper()
	return New(serverURL, &http.Client{Timeout: 2 * time.Second}, nil, retry)
}

func zeroRetry() RetryConfig { return RetryConfig{MaxRetries: 0, BaseDelay: time.Millisecond} }
func oneRetry() RetryConfig  { return RetryConfig{MaxRetries: 1, BaseDelay: time.Millisecond} }
func twoRetry() RetryConfig  { return RetryConfig{MaxRetries: 2, BaseDelay: time.Millisecond} }

func TestGet_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("want GET, got %s", r.Method)
		}
		if r.URL.Path != "/test" {
			t.Errorf("want path /test, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, zeroRetry())
	body, err := c.Get(context.Background(), "/test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(body) != `{"ok":true}` {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestPost_SetsContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct := r.Header.Get("Content-Type")
		if ct != "application/json" {
			t.Errorf("want Content-Type application/json, got %q", ct)
		}
		b, _ := io.ReadAll(r.Body)
		if string(b) != `{"field":"value"}` {
			t.Errorf("unexpected body: %s", b)
		}
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, zeroRetry())
	_, err := c.Post(context.Background(), "/", map[string]string{"field": "value"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExtraHeaders_Forwarded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Session-Token"); got != "tok123" {
			t.Errorf("want header X-Session-Token=tok123, got %q", got)
		}
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, zeroRetry())
	_, err := c.Get(context.Background(), "/", map[string]string{"X-Session-Token": "tok123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

var errorTests = []struct {
	name           string
	status         int
	responseBody   string
	retryAfter     string
	wantErrType    string // "ksef", "auth", "ratelimit"
	wantHTTPStatus int
	wantExCode     int32
}{
	{
		name:           "400 validation error",
		status:         http.StatusBadRequest,
		responseBody:   `{"exception":{"exceptionDetailList":[{"exceptionCode":21405,"exceptionDescription":"Błąd walidacji.","details":[]}],"referenceNumber":"ref-001"}}`,
		wantErrType:    "ksef",
		wantHTTPStatus: 400,
		wantExCode:     21405,
	},
	{
		name:           "404 not found",
		status:         http.StatusNotFound,
		responseBody:   `{"exception":{"exceptionDetailList":[]}}`,
		wantErrType:    "ksef",
		wantHTTPStatus: 404,
	},
	{
		name:         "401 unauthorized",
		status:       http.StatusUnauthorized,
		responseBody: `{"exception":{"exceptionDetailList":[]}}`,
		wantErrType:  "auth",
	},
	{
		name:         "403 forbidden",
		status:       http.StatusForbidden,
		responseBody: `{"exception":{"exceptionDetailList":[]}}`,
		wantErrType:  "auth",
	},
}

func TestErrorParsing(t *testing.T) {
	for _, tc := range errorTests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.responseBody)
			}))
			defer srv.Close()

			c := newTestClient(t, srv.URL, zeroRetry())
			_, err := c.Get(context.Background(), "/", nil)
			if err == nil {
				t.Fatal("expected an error, got nil")
			}

			switch tc.wantErrType {
			case "ksef":
				var ke *ksef.KSeFError
				if !errors.As(err, &ke) {
					t.Fatalf("want *ksef.KSeFError, got %T: %v", err, err)
				}
				if ke.HTTPStatus != tc.wantHTTPStatus {
					t.Errorf("want HTTPStatus %d, got %d", tc.wantHTTPStatus, ke.HTTPStatus)
				}
				if tc.wantExCode != 0 {
					if len(ke.Exceptions) == 0 {
						t.Fatal("want at least one exception detail, got none")
					}
					if ke.Exceptions[0].ExceptionCode != tc.wantExCode {
						t.Errorf("want ExceptionCode %d, got %d", tc.wantExCode, ke.Exceptions[0].ExceptionCode)
					}
				}
			case "auth":
				var ae *ksef.AuthenticationError
				if !errors.As(err, &ae) {
					t.Fatalf("want *ksef.AuthenticationError, got %T: %v", err, err)
				}
			case "ratelimit":
				var re *ksef.RateLimitError
				if !errors.As(err, &re) {
					t.Fatalf("want *ksef.RateLimitError, got %T: %v", err, err)
				}
			}
		})
	}
}

func TestRateLimit_RetryAfterHeader_NoRetries(t *testing.T) {
	// Without retries the client should return a RateLimitError immediately.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, zeroRetry())
	_, err := c.Get(context.Background(), "/", nil)

	var re *ksef.RateLimitError
	if !errors.As(err, &re) {
		t.Fatalf("want *ksef.RateLimitError, got %T: %v", err, err)
	}
	if re.RetryAfter != time.Second {
		t.Errorf("want RetryAfter 1s, got %s", re.RetryAfter)
	}
}

func TestRateLimit_RetriedAndSucceeds(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			// First call: return 429 with a tiny Retry-After.
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{}`)
			return
		}
		// Second call: success.
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, oneRetry())
	body, err := c.Get(context.Background(), "/", nil)
	if err != nil {
		t.Fatalf("unexpected error after retry: %v", err)
	}
	if !strings.Contains(string(body), "ok") {
		t.Errorf("unexpected body: %s", body)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("want 2 calls, got %d", got)
	}
}

func Test5xx_RetriedAndSucceeds(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, `{}`)
			return
		}
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, twoRetry())
	body, err := c.Get(context.Background(), "/", nil)
	if err != nil {
		t.Fatalf("unexpected error after retries: %v", err)
	}
	if !strings.Contains(string(body), "ok") {
		t.Errorf("unexpected body: %s", body)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("want 3 calls, got %d", got)
	}
}

func Test5xx_ExhaustsRetries(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	cfg := RetryConfig{MaxRetries: 2, BaseDelay: time.Millisecond}
	c := newTestClient(t, srv.URL, cfg)
	_, err := c.Get(context.Background(), "/", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var ke *ksef.KSeFError
	if !errors.As(err, &ke) {
		t.Fatalf("want *ksef.KSeFError, got %T", err)
	}
	if ke.HTTPStatus != http.StatusInternalServerError {
		t.Errorf("want status 500, got %d", ke.HTTPStatus)
	}
	// 1 initial attempt + 2 retries = 3 total calls.
	if got := calls.Load(); got != 3 {
		t.Errorf("want 3 calls, got %d", got)
	}
}

func TestContextCancellation_DuringRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Cancel the context before responding so the client sees an error.
		cancel()
		// Give the cancellation a moment to propagate.
		time.Sleep(10 * time.Millisecond)
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, zeroRetry())
	_, err := c.Get(ctx, "/", nil)
	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled in error chain, got: %v", err)
	}
}

func TestContextCancellation_DuringRetryDelay(t *testing.T) {
	// Server always returns 500 to trigger a retry.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	// Use a long base delay so the context deadline triggers before the retry fires.
	cfg := RetryConfig{MaxRetries: 3, BaseDelay: 5 * time.Second}
	c := newTestClient(t, srv.URL, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := c.Get(ctx, "/", nil)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	// The error should be (or wrap) a deadline/cancellation error.
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Errorf("want deadline/cancel error, got: %v", err)
	}
}

func TestDelete_Method(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("want DELETE, got %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, zeroRetry())
	_, err := c.Delete(context.Background(), "/resource/1", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPut_Method(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("want PUT, got %s", r.Method)
		}
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, zeroRetry())
	_, err := c.Put(context.Background(), "/resource/1", map[string]string{"k": "v"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
