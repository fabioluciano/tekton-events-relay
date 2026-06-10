// Package errors provides error classification and handling utilities.
package errors

import (
	"errors"
	"fmt"
)

// RetryableError represents an error that should trigger retry via 503 response.
// Tekton CloudEvents controller will retry with exponential backoff.
type RetryableError struct {
	// Cause is the underlying error that triggered the retry
	Cause error
	// Reason is a human-readable classification (e.g., "rate_limit", "timeout", "network")
	Reason string
}

// Error implements error interface.
func (e *RetryableError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("retryable error (%s): %v", e.Reason, e.Cause)
	}
	return fmt.Sprintf("retryable error: %v", e.Cause)
}

// Unwrap returns the underlying cause for errors.Is/As support.
func (e *RetryableError) Unwrap() error {
	return e.Cause
}

// NewRetryable wraps an error as retryable with optional reason.
func NewRetryable(cause error, reason string) error {
	return &RetryableError{
		Cause:  cause,
		Reason: reason,
	}
}

// IsRetryable returns true if err is or wraps a RetryableError.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	var retryErr *RetryableError
	return errors.As(err, &retryErr)
}
