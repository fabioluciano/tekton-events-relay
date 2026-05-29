package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestNewClient(t *testing.T) {
	tests := []struct {
		name      string
		cfg       ClientConfig
		wantDebug bool
	}{
		{
			name: "default client without debug",
			cfg: ClientConfig{
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
			cfg: ClientConfig{
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
			client := NewClient(tt.cfg)
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test response"))
	}))
	defer server.Close()

	logger := zap.NewNop()
	cfg := ClientConfig{
		Timeout:    5 * time.Second,
		MaxRetries: 3,
		BaseDelay:  100 * time.Millisecond,
		Debug:      true,
		Logger:     logger,
	}

	client := NewClient(cfg)
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestDefaultClientConfig(t *testing.T) {
	cfg := DefaultClientConfig()
	if cfg.Timeout != 10*time.Second {
		t.Errorf("default timeout = %v, want 10s", cfg.Timeout)
	}
	if cfg.MaxRetries != 3 {
		t.Errorf("default retries = %d, want 3", cfg.MaxRetries)
	}
	if cfg.Debug {
		t.Error("default debug should be false")
	}
}
