package errors

import (
	"errors"
	"fmt"
	"testing"
)

func TestRetryableError_Error(t *testing.T) {
	cause := errors.New("connection refused")

	tests := []struct {
		name   string
		err    *RetryableError
		expect string
	}{
		{
			name:   "with reason",
			err:    &RetryableError{Cause: cause, Reason: "network"},
			expect: "retryable error (network): connection refused",
		},
		{
			name:   "without reason",
			err:    &RetryableError{Cause: cause},
			expect: "retryable error: connection refused",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.expect {
				t.Errorf("Error() = %q, want %q", got, tt.expect)
			}
		})
	}
}

func TestRetryableError_Unwrap(t *testing.T) {
	cause := errors.New("original")
	err := &RetryableError{Cause: cause}

	if !errors.Is(err.Unwrap(), cause) {
		t.Error("Unwrap() should return cause")
	}
}

func TestNewRetryable(t *testing.T) {
	cause := errors.New("timeout")
	err := NewRetryable(cause, "timeout")

	if !IsRetryable(err) {
		t.Error("NewRetryable should create retryable error")
	}

	var retryErr *RetryableError
	if !errors.As(err, &retryErr) {
		t.Error("should unwrap to RetryableError")
	}

	if retryErr.Reason != "timeout" {
		t.Errorf("Reason = %q, want timeout", retryErr.Reason)
	}
}

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		expect bool
	}{
		{
			name:   "nil error",
			err:    nil,
			expect: false,
		},
		{
			name:   "retryable error",
			err:    NewRetryable(errors.New("test"), "test"),
			expect: true,
		},
		{
			name:   "wrapped retryable",
			err:    fmt.Errorf("wrapped: %w", NewRetryable(errors.New("test"), "test")),
			expect: true,
		},
		{
			name:   "permanent error",
			err:    errors.New("permanent"),
			expect: false,
		},
		{
			name:   "wrapped permanent",
			err:    fmt.Errorf("wrapped: %w", errors.New("permanent")),
			expect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRetryable(tt.err); got != tt.expect {
				t.Errorf("IsRetryable() = %v, want %v", got, tt.expect)
			}
		})
	}
}
