package store

import (
	"bytes"
	"context"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

func TestZapLogWriter(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantLevel zapcore.Level
		wantMsg   string
	}{
		{
			name:      "olric INFO",
			input:     "2026/06/15 00:22:42 [INFO] The cluster coordinator has been bootstrapped => discovery.go:43\n",
			wantLevel: zapcore.InfoLevel,
			wantMsg:   "The cluster coordinator has been bootstrapped => discovery.go:43",
		},
		{
			name:      "olric DEBUG",
			input:     "2026/06/15 00:22:42 [DEBUG] memberlist: Stream connection from=10.244.1.175:54578\n",
			wantLevel: zapcore.DebugLevel,
			wantMsg:   "memberlist: Stream connection from=10.244.1.175:54578",
		},
		{
			name:      "olric WARN",
			input:     "2026/06/15 00:22:42 [WARN] some warning message\n",
			wantLevel: zapcore.WarnLevel,
			wantMsg:   "some warning message",
		},
		{
			name:      "olric ERROR",
			input:     "2026/06/15 00:22:42 [ERROR] something failed\n",
			wantLevel: zapcore.ErrorLevel,
			wantMsg:   "something failed",
		},
		{
			name:      "memberlist ERR",
			input:     "2026/06/15 00:22:42 [ERR] memberlist: Error accepting TCP connection: EOF\n",
			wantLevel: zapcore.ErrorLevel,
			wantMsg:   "memberlist: Error accepting TCP connection: EOF",
		},
		{
			name:      "non-matching format falls back to INFO",
			input:     "some unexpected log line\n",
			wantLevel: zapcore.InfoLevel,
			wantMsg:   "some unexpected log line",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			enc := zapcore.NewJSONEncoder(zap.NewProductionConfig().EncoderConfig)
			core := zapcore.NewCore(enc, zapcore.AddSync(&buf), zapcore.DebugLevel)
			logger := zap.New(core)

			writer := &zapLogWriter{log: logger}
			_, err := writer.Write([]byte(tt.input))
			if err != nil {
				t.Fatalf("Write() error = %v", err)
			}

			output := buf.String()
			if len(output) == 0 {
				t.Fatal("expected log output, got empty")
			}

			// Verify level is present in output
			levelStr := tt.wantLevel.String()
			switch tt.wantLevel {
			case zapcore.DebugLevel:
				levelStr = `"debug"`
			case zapcore.InfoLevel:
				levelStr = `"info"`
			case zapcore.WarnLevel:
				levelStr = `"warn"`
			case zapcore.ErrorLevel:
				levelStr = `"error"`
			}
			if !bytes.Contains(buf.Bytes(), []byte(levelStr)) {
				t.Errorf("expected level %s in output: %s", levelStr, output)
			}

			// Verify message is present
			if !bytes.Contains(buf.Bytes(), []byte(tt.wantMsg)) {
				t.Errorf("expected message %q in output: %s", tt.wantMsg, output)
			}
		})
	}
}

// TestOlricStore_SingleNode boots a real embedded olric member, so it is
// skipped in -short runs.
func TestOlricStore_SingleNode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded olric test in short mode")
	}

	s, err := New(config.StoreConfig{
		Backend: BackendOlric,
		TTL:     time.Minute,
		Olric: config.OlricConfig{
			Env:            "local",
			BindAddr:       "127.0.0.1",
			BindPort:       13320,
			MemberlistPort: 13322,
		},
	}, Options{})
	if err != nil {
		t.Fatalf("new olric store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	ctx := context.Background()

	first, err := s.Dedupe().FirstSeen(ctx, "evt-1")
	if err != nil || !first {
		t.Fatalf("FirstSeen = (%v, %v), want (true, nil)", first, err)
	}
	again, err := s.Dedupe().FirstSeen(ctx, "evt-1")
	if err != nil || again {
		t.Fatalf("FirstSeen repeat = (%v, %v), want (false, nil)", again, err)
	}

	rb := s.RunBuffer()
	if err := rb.Add(ctx, "uid-1", "build", sampleEvent("build", domain.StateSuccess)); err != nil {
		t.Fatalf("Add: %v", err)
	}
	tasks, found, err := rb.Flush(ctx, "uid-1")
	if err != nil || !found {
		t.Fatalf("Flush = (found=%v, %v), want (true, nil)", found, err)
	}
	if tasks["build"].State != domain.StateSuccess {
		t.Errorf("state = %q, want success", tasks["build"].State)
	}
	if _, found, _ := rb.Flush(ctx, "uid-1"); found {
		t.Error("second Flush should report not found")
	}
}
