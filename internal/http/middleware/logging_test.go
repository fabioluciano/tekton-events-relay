package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestRequestLogging(t *testing.T) {
	tests := []struct {
		name            string
		requestID       string
		statusCode      int
		expectedLevel   string
		expectRequestID bool
	}{
		{
			name:            "success request with generated ID",
			requestID:       "",
			statusCode:      http.StatusOK,
			expectedLevel:   "info",
			expectRequestID: true,
		},
		{
			name:            "client error returns warn",
			requestID:       "test-req-123",
			statusCode:      http.StatusBadRequest,
			expectedLevel:   "warn",
			expectRequestID: true,
		},
		{
			name:            "server error returns error",
			requestID:       "test-req-456",
			statusCode:      http.StatusInternalServerError,
			expectedLevel:   "error",
			expectRequestID: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create observed logger
			core, logs := observer.New(zap.InfoLevel)
			logger := zap.New(core)

			// Create test handler
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify request ID in context
				requestID := RequestIDFromContext(r.Context())
				if tt.expectRequestID && requestID == "" {
					t.Error("Expected request ID in context, got empty")
				}

				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte("test response"))
			})

			// Wrap with logging middleware
			middleware := RequestLogging(logger)
			wrappedHandler := middleware(handler)

			// Create test request
			req := httptest.NewRequest(http.MethodGet, "/test?foo=bar", nil)
			if tt.requestID != "" {
				req.Header.Set("X-Request-ID", tt.requestID)
			}

			// Create test response recorder
			rr := httptest.NewRecorder()

			// Serve
			wrappedHandler.ServeHTTP(rr, req)

			// Verify response header
			responseID := rr.Header().Get("X-Request-ID")
			if tt.expectRequestID && responseID == "" {
				t.Error("Expected X-Request-ID in response header")
			}
			if tt.requestID != "" && responseID != tt.requestID {
				t.Errorf("Expected request ID %s, got %s", tt.requestID, responseID)
			}

			// Verify logs
			allLogs := logs.All()
			if len(allLogs) < 2 {
				t.Fatalf("Expected at least 2 log entries (start + complete), got %d", len(allLogs))
			}

			// Check request_started log
			startLog := allLogs[0]
			if startLog.Message != "http_request_started" {
				t.Errorf("Expected message 'http_request_started', got '%s'", startLog.Message)
			}

			// Check request_completed log
			completeLog := allLogs[len(allLogs)-1]
			if completeLog.Message != "http_request_completed" {
				t.Errorf("Expected message 'http_request_completed', got '%s'", completeLog.Message)
			}

			// Verify log level
			if completeLog.Level.String() != tt.expectedLevel {
				t.Errorf("Expected level %s, got %s", tt.expectedLevel, completeLog.Level.String())
			}

			// Verify fields
			fields := completeLog.ContextMap()
			if fields["status"] != int64(tt.statusCode) {
				t.Errorf("Expected status %d, got %v", tt.statusCode, fields["status"])
			}
			if fields["bytes_sent"] == nil {
				t.Error("Expected bytes_sent field")
			}
			if fields["duration_ms"] == nil {
				t.Error("Expected duration_ms field")
			}
		})
	}
}

func TestGenerateRequestID(t *testing.T) {
	// Generate multiple IDs and ensure they're unique
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := generateRequestID()
		if id == "" {
			t.Error("Generated empty request ID")
		}
		if ids[id] {
			t.Errorf("Duplicate request ID generated: %s", id)
		}
		ids[id] = true
	}
}

func TestRequestIDFromContext(t *testing.T) {
	t.Run("returns request ID when present", func(t *testing.T) {
		core, _ := observer.New(zap.InfoLevel)
		logger := zap.New(core)

		handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			id := RequestIDFromContext(r.Context())
			if id != "test-123" {
				t.Errorf("Expected request ID 'test-123', got '%s'", id)
			}
		})

		middleware := RequestLogging(logger)
		wrappedHandler := middleware(handler)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Request-ID", "test-123")

		rr := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(rr, req)
	})

	t.Run("returns empty string when not present", func(t *testing.T) {
		ctx := context.Background()
		id := RequestIDFromContext(ctx)
		if id != "" {
			t.Errorf("Expected empty string, got '%s'", id)
		}
	})
}
