package store

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"runtime"
	"strings"
	"testing"
	"time"

	olric "github.com/olric-data/olric"
	olricconfig "github.com/olric-data/olric/config"
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
			input:     "2026/06/15 00:22:42 [INFO] Olric bindAddr: 10.244.1.200, bindPort: 3320 => olric.go:402\n",
			wantLevel: zapcore.DebugLevel,
			wantMsg:   "Olric bindAddr: 10.244.1.200, bindPort: 3320 => olric.go:402",
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
			name:      "non-matching format falls back to WARN",
			input:     "some unexpected log line\n",
			wantLevel: zapcore.WarnLevel,
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
func TestOlricPing(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded olric test in short mode")
	}

	s, err := New(config.StoreConfig{
		Backend: BackendOlric,
		TTL:     time.Minute,
		Olric: config.OlricConfig{
			Env:            "local",
			BindAddr:       "127.0.0.1",
			BindPort:       13330,
			MemberlistPort: 13332,
		},
	}, Options{})
	if err != nil {
		t.Fatalf("new olric store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if err := s.Ping(context.Background()); err != nil {
		t.Fatalf("olric Ping: %v", err)
	}
}

const (
	olricTestEnv  = "local"
	olricTestBind = "127.0.0.1"
)

func TestOlricStore_SingleNode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded olric test in short mode")
	}

	s, err := New(config.StoreConfig{
		Backend: BackendOlric,
		TTL:     time.Minute,
		Olric: config.OlricConfig{
			Env:            olricTestEnv,
			BindAddr:       olricTestBind,
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

func TestOlricStore_DistributedLock(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded olric test in short mode")
	}

	s, err := New(config.StoreConfig{
		Backend: BackendOlric,
		TTL:     time.Minute,
		Olric: config.OlricConfig{
			Env:            olricTestEnv,
			BindAddr:       olricTestBind,
			BindPort:       13330,
			MemberlistPort: 13332,
		},
	}, Options{})
	if err != nil {
		t.Fatalf("new olric store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	ctx := context.Background()
	rb := s.RunBuffer()

	const workers = 10
	errs := make(chan error, workers)
	for i := range workers {
		go func(idx int) {
			task := fmt.Sprintf("task-%c", 'A'+idx)
			errs <- rb.Add(ctx, "uid-concurrent", task, sampleEvent(task, domain.StateRunning))
		}(i)
	}

	for range workers {
		if err := <-errs; err != nil {
			t.Errorf("concurrent Add: %v", err)
		}
	}

	tasks, found, err := rb.Flush(ctx, "uid-concurrent")
	if err != nil {
		t.Fatalf("Flush after concurrent Add: %v", err)
	}
	if !found {
		t.Fatal("Flush after concurrent Add returned not found")
	}
	if len(tasks) != workers {
		t.Errorf("len(tasks) = %d, want %d (possible data loss under lock)", len(tasks), workers)
	}

	for i := range workers {
		task := "task-" + string(rune('A'+i))
		if _, ok := tasks[task]; !ok {
			t.Errorf("task %q missing from flushed result (data loss)", task)
		}
	}
}

func TestOlricStore_DistributedLock_Flush(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded olric test in short mode")
	}

	s, err := New(config.StoreConfig{
		Backend: BackendOlric,
		TTL:     time.Minute,
		Olric: config.OlricConfig{
			Env:            olricTestEnv,
			BindAddr:       olricTestBind,
			BindPort:       13331,
			MemberlistPort: 13333,
		},
	}, Options{})
	if err != nil {
		t.Fatalf("new olric store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	ctx := context.Background()
	rb := s.RunBuffer()

	_, found, err := rb.Flush(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("Flush on nonexistent: %v", err)
	}
	if found {
		t.Error("Flush on nonexistent key should be false")
	}

	if err := rb.Add(ctx, "uid-flush", "build", sampleEvent("build", domain.StateSuccess)); err != nil {
		t.Fatalf("Add: %v", err)
	}
	tasks, found, err := rb.Flush(ctx, "uid-flush")
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if !found {
		t.Fatal("Flush should return found")
	}
	if len(tasks) != 1 || tasks["build"].State != domain.StateSuccess {
		t.Errorf("unexpected tasks after flush: %+v", tasks)
	}

	if _, found, _ := rb.Flush(ctx, "uid-flush"); found {
		t.Error("second Flush should report not found (atomic delete under lock)")
	}
}

func TestOlricStore_PeerDiscovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded olric test in short mode")
	}

	s, err := New(config.StoreConfig{
		Backend: BackendOlric,
		TTL:     time.Minute,
		Olric: config.OlricConfig{
			Env:            olricTestEnv,
			BindAddr:       olricTestBind,
			BindPort:       13350,
			MemberlistPort: 13352,
			Peers:          []string{"127.0.0.1:13353"},
		},
	}, Options{})
	if err != nil {
		t.Fatalf("new olric store with peers: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	ctx := context.Background()

	first, err := s.Dedupe().FirstSeen(ctx, "peer-evt")
	if err != nil || !first {
		t.Fatalf("FirstSeen = (%v, %v), want (true, nil)", first, err)
	}
	again, err := s.Dedupe().FirstSeen(ctx, "peer-evt")
	if err != nil || again {
		t.Fatalf("FirstSeen repeat = (%v, %v), want (false, nil)", again, err)
	}

	rb := s.RunBuffer()
	if err := rb.Add(ctx, "uid-peer", "build", sampleEvent("build", domain.StateRunning)); err != nil {
		t.Fatalf("Add: %v", err)
	}
	tasks, found, err := rb.Flush(ctx, "uid-peer")
	if err != nil || !found {
		t.Fatalf("Flush = (found=%v, %v), want (true, nil)", found, err)
	}
	if tasks["build"].State != domain.StateRunning {
		t.Errorf("state = %q, want running", tasks["build"].State)
	}
}

func TestOlricStore_StartTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded olric test in short mode")
	}

	oc := olricconfig.New(olricTestEnv)
	oc.LogLevel = "WARN"
	oc.Logger = log.New(&zapLogWriter{log: zap.NewNop()}, "", 0)
	oc.BindAddr = olricTestBind
	oc.BindPort = 13340
	oc.MemberlistConfig.BindAddr = olricTestBind
	oc.MemberlistConfig.BindPort = 13342

	db, err := olric.New(oc)
	if err != nil {
		t.Fatalf("olric.New: %v", err)
	}

	started := make(chan struct{}) // never closed — forces timeout

	before := runtime.NumGoroutine()

	err = startOlric(db, started, 50*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error = %q, want contains 'timed out'", err.Error())
	}

	// startOlric drains the goroutine before returning; give the runtime
	// a moment to schedule the exit, then assert no leak.
	time.Sleep(100 * time.Millisecond)

	after := runtime.NumGoroutine()
	if after > before+2 {
		t.Errorf("goroutine leak: before=%d, after=%d", before, after)
	}
}
