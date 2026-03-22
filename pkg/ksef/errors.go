package ksef

import (
	"fmt"
	"time"
)

// ExceptionDetail represents a single error entry returned by the KSeF API
// inside an ExceptionResponse body.
type ExceptionDetail struct {
	// ExceptionCode is the numeric KSeF error code (e.g. 21405).
	ExceptionCode int32
	// ExceptionDescription is the human-readable description of the error,
	// typically in Polish.
	ExceptionDescription string
	// Details contains optional additional context provided by the API.
	Details []string
}

// KSeFError is returned when the KSeF API responds with a non-2xx status and
// an ExceptionResponse body. It preserves the full list of exception details
// so callers can inspect individual error codes.
type KSeFError struct {
	// HTTPStatus is the HTTP response status code (e.g. 400, 404).
	HTTPStatus int
	// ReferenceNumber is the KSeF correlation identifier from the response,
	// if present.
	ReferenceNumber string
	// Exceptions contains the individual error entries reported by the API.
	Exceptions []ExceptionDetail
}

// Error implements the error interface. It formats the first exception, if any.
func (e *KSeFError) Error() string {
	if len(e.Exceptions) == 0 {
		return fmt.Sprintf("ksef: API error (HTTP %d)", e.HTTPStatus)
	}
	first := e.Exceptions[0]
	return fmt.Sprintf("ksef: API error (HTTP %d, code %d): %s",
		e.HTTPStatus, first.ExceptionCode, first.ExceptionDescription)
}

// AuthenticationError is returned when the KSeF API rejects a request due to
// invalid or expired credentials (HTTP 401 / 403).
type AuthenticationError struct {
	// Cause is the underlying KSeFError, if the server returned one.
	Cause *KSeFError
}

// Error implements the error interface.
func (e *AuthenticationError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("ksef: authentication failed: %s", e.Cause.Error())
	}
	return "ksef: authentication failed"
}

// Unwrap returns the underlying error so errors.As can traverse the chain.
func (e *AuthenticationError) Unwrap() error { return e.Cause }

// SessionError is returned when an operation fails because the KSeF session is
// in an invalid state (e.g. already closed, not yet opened).
type SessionError struct {
	// Message describes what went wrong.
	Message string
	// Cause is the underlying error, if any.
	Cause error
}

// Error implements the error interface.
func (e *SessionError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("ksef: session error: %s: %v", e.Message, e.Cause)
	}
	return fmt.Sprintf("ksef: session error: %s", e.Message)
}

// Unwrap returns the underlying error so errors.As can traverse the chain.
func (e *SessionError) Unwrap() error { return e.Cause }

// ValidationError is returned when input data fails client-side or server-side
// validation before or during an API call.
type ValidationError struct {
	// Field identifies the field or path that failed validation, if known.
	Field string
	// Message describes the validation failure.
	Message string
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("ksef: validation error on %q: %s", e.Field, e.Message)
	}
	return fmt.Sprintf("ksef: validation error: %s", e.Message)
}

// RateLimitError is returned when the KSeF API responds with HTTP 429
// (Too Many Requests). The caller should wait at least RetryAfter before
// retrying.
type RateLimitError struct {
	// RetryAfter is the duration the caller should wait before retrying.
	// It is derived from the Retry-After response header when present.
	RetryAfter time.Duration
}

// Error implements the error interface.
func (e *RateLimitError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("ksef: rate limit exceeded, retry after %s", e.RetryAfter)
	}
	return "ksef: rate limit exceeded"
}
