package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// contextKey is a custom type for context keys to avoid collisions.
type contextKey string

const requestIDKey contextKey = "request_id"

// responseWriter wraps http.ResponseWriter to capture status code and bytes written.
type responseWriter struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.bytesWritten += n
	return n, err
}

// RequestLogging logs HTTP request start and completion with structured fields.
func RequestLogging(logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Generate request ID if not present
			requestID := r.Header.Get("X-Request-ID")
			if requestID == "" {
				requestID = generateRequestID()
			}

			// Wrap response writer to capture status and bytes
			rw := &responseWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK, // default if WriteHeader not called
			}

			// Log request start
			logger.Info("http_request_started",
				zap.String("request_id", requestID),
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.String("query", r.URL.RawQuery),
				zap.String("remote_addr", r.RemoteAddr),
				zap.String("user_agent", r.UserAgent()),
			)

			// Inject request ID into context
			ctx := context.WithValue(r.Context(), requestIDKey, requestID)
			r = r.WithContext(ctx)

			// Set response header
			rw.Header().Set("X-Request-ID", requestID)

			// Handle request
			next.ServeHTTP(rw, r)

			// Calculate duration
			duration := time.Since(start)

			// Determine log level by status code
			level := zap.InfoLevel
			if rw.statusCode >= 500 {
				level = zap.ErrorLevel
			} else if rw.statusCode >= 400 {
				level = zap.WarnLevel
			}

			// Log request completion
			logger.Log(level, "http_request_completed",
				zap.String("request_id", requestID),
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.Int("status", rw.statusCode),
				zap.Int("bytes_sent", rw.bytesWritten),
				zap.Int64("duration_ms", duration.Milliseconds()),
			)
		})
	}
}

// generateRequestID creates a unique request ID.
func generateRequestID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "req_" + time.Now().UTC().Format("20060102150405")
	}
	return "req_" + hex.EncodeToString(b)
}
