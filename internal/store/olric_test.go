package store

import (
	"context"
	"testing"
	"time"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

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
