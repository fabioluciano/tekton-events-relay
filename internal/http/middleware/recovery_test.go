package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestPanicRecovery(t *testing.T) {
	t.Run("recovers from panic and logs", func(t *testing.T) {
		// Create observed logger
		core, logs := observer.New(zapcore.ErrorLevel)
		logger := zap.New(core)

		// Create handler that panics
		handler := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			panic("test panic")
		})

		// Wrap with recovery middleware
		middleware := PanicRecovery(logger)
		wrappedHandler := middleware(handler)

		// Create test request
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rr := httptest.NewRecorder()

		// Serve - should not panic
		wrappedHandler.ServeHTTP(rr, req)

		// Verify response
		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}

		body := strings.TrimSpace(rr.Body.String())
		if body != "Internal Server Error" {
			t.Errorf("Expected 'Internal Server Error', got '%s'", body)
		}

		// Verify panic was logged
		allLogs := logs.All()
		if len(allLogs) != 1 {
			t.Fatalf("Expected 1 log entry, got %d", len(allLogs))
		}

		panicLog := allLogs[0]
		if panicLog.Message != "http_handler_panic" {
			t.Errorf("Expected message 'http_handler_panic', got '%s'", panicLog.Message)
		}

		if panicLog.Level != zapcore.ErrorLevel {
			t.Errorf("Expected error level, got %s", panicLog.Level)
		}

		// Verify fields
		fields := panicLog.ContextMap()
		if fields["method"] != "GET" {
			t.Errorf("Expected method 'GET', got %v", fields["method"])
		}
		if fields["path"] != "/test" {
			t.Errorf("Expected path '/test', got %v", fields["path"])
		}
		if fields["panic"] != "test panic" {
			t.Errorf("Expected panic 'test panic', got %v", fields["panic"])
		}

		// Verify stack trace is present
		stack, ok := fields["stack"].(string)
		if !ok || stack == "" {
			t.Error("Expected non-empty stack trace")
		}
		if !strings.Contains(stack, "panic") {
			t.Error("Expected stack trace to contain 'panic'")
		}
	})

	t.Run("does not interfere with normal requests", func(t *testing.T) {
		core, logs := observer.New(zapcore.ErrorLevel)
		logger := zap.New(core)

		// Create normal handler
		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		})

		middleware := PanicRecovery(logger)
		wrappedHandler := middleware(handler)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rr := httptest.NewRecorder()

		wrappedHandler.ServeHTTP(rr, req)

		// Verify response
		if rr.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", rr.Code)
		}
		if rr.Body.String() != "ok" {
			t.Errorf("Expected body 'ok', got '%s'", rr.Body.String())
		}

		// Verify no logs
		if len(logs.All()) != 0 {
			t.Errorf("Expected 0 log entries for normal request, got %d", len(logs.All()))
		}
	})

	t.Run("recovers from panic with non-string value", func(t *testing.T) {
		core, logs := observer.New(zapcore.ErrorLevel)
		logger := zap.New(core)

		handler := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			panic(12345) // Panic with int
		})

		middleware := PanicRecovery(logger)
		wrappedHandler := middleware(handler)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rr := httptest.NewRecorder()

		wrappedHandler.ServeHTTP(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", rr.Code)
		}

		allLogs := logs.All()
		if len(allLogs) != 1 {
			t.Fatalf("Expected 1 log entry, got %d", len(allLogs))
		}

		fields := allLogs[0].ContextMap()
		if fields["panic"] != int64(12345) {
			t.Errorf("Expected panic value 12345, got %v", fields["panic"])
		}
	})
}
