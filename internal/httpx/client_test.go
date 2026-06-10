package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestNewClient(t *testing.T) {
	tests := []struct {
		name string
		cfg  struct {
			Timeout            time.Duration
			MaxRetries         int
			BaseDelay          time.Duration
			Debug              bool
			Logger             *zap.Logger
			Name               string
			InsecureSkipVerify bool
		}
		wantDebug bool
	}{
		{
			name: "default client without debug",
			cfg: struct {
				Timeout            time.Duration
				MaxRetries         int
				BaseDelay          time.Duration
				Debug              bool
				Logger             *zap.Logger
				Name               string
				InsecureSkipVerify bool
			}{
				Timeout:    5 * time.Second,
				MaxRetries: 3,
				BaseDelay:  100 * time.Millisecond,
				Debug:      false,
				Logger:     nil,
			},
			wantDebug: false,
		},
		{
			name: "client with debug enabled",
			cfg: struct {
				Timeout            time.Duration
				MaxRetries         int
				BaseDelay          time.Duration
				Debug              bool
				Logger             *zap.Logger
				Name               string
				InsecureSkipVerify bool
			}{
				Timeout:    5 * time.Second,
				MaxRetries: 3,
				BaseDelay:  100 * time.Millisecond,
				Debug:      true,
				Logger:     zap.NewNop(),
			},
			wantDebug: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var opts []Option
			if tt.cfg.Timeout != 0 {
				opts = append(opts, WithTimeout(tt.cfg.Timeout))
			}
			if tt.cfg.Debug {
				opts = append(opts, WithDebug(tt.cfg.Logger, tt.cfg.Name))
			}
			if tt.cfg.InsecureSkipVerify {
				opts = append(opts, WithInsecureSkipVerify())
			}

			client := NewClient(opts...)
			if client == nil {
				t.Fatal("NewClient returned nil")
			}
			if client.Timeout != tt.cfg.Timeout {
				t.Errorf("timeout = %v, want %v", client.Timeout, tt.cfg.Timeout)
			}

			// Verify debug transport is attached when debug=true
			if tt.wantDebug {
				if _, ok := client.Transport.(*debugTransport); !ok {
					t.Error("expected debugTransport when Debug=true")
				}
			} else {
				if _, ok := client.Transport.(*debugTransport); ok {
					t.Error("did not expect debugTransport when Debug=false")
				}
			}
		})
	}
}

func TestDebugTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("test response"))
	}))
	defer server.Close()

	logger := zap.NewNop()

	client := NewClient(
		WithTimeout(5*time.Second),
		WithDebug(logger, "test-client"),
	)
	req, _ := http.NewRequestWithContext(context.Background(), "GET", server.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}
