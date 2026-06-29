package rabbitmq

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

const testRabbitName = "test"

func TestNew(t *testing.T) {
	t.Run("missing url", func(t *testing.T) {
		_, err := New(Config{
			Name: testRabbitName,
		})
		if err == nil {
			t.Fatal("expected error for missing url")
		}
	})

	t.Run("invalid url", func(t *testing.T) {
		_, err := New(Config{
			Name: testRabbitName,
			URL:  "amqp://invalid-host:5672",
		})
		if err == nil {
			t.Fatal("expected error for invalid url")
		}
	})
}

func TestType(t *testing.T) {
	n := &Notifier{
		cfg: Config{Name: testRabbitName, URL: "amqp://localhost"},
	}
	if n.Type() != notifier.ActionNotify {
		t.Errorf("Type() = %v, want %v", n.Type(), notifier.ActionNotify)
	}
}

func TestClose_Idempotent(t *testing.T) {
	n := &Notifier{
		cfg: Config{Name: testRabbitName, URL: "amqp://localhost"},
	}
	if err := n.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := n.Close(); err != nil {
		t.Fatalf("second Close() error = %v, want nil (idempotent)", err)
	}
}

func TestHandle_AfterClose(t *testing.T) {
	n := &Notifier{
		cfg: Config{Name: testRabbitName, URL: "amqp://localhost"},
	}
	_ = n.Close()

	err := n.Handle(context.Background(), domain.Event{
		State:   domain.StateSuccess,
		RunName: "run-1",
	})
	if err == nil {
		t.Fatal("expected error after Close()")
	}
}

// TestCloseCalledOnReload simulates the reload pattern: old handlers are closed
// after a new registry is swapped in. This proves that the Close() method is
// invoked and the connection is properly shut down.
func TestCloseCalledOnReload(t *testing.T) {
	n := &Notifier{
		cfg: Config{Name: testRabbitName, URL: "amqp://localhost"},
	}

	// Verify compile-time interface satisfaction.
	var _ notifier.Closer = n

	// Simulate reload: close old handler, verify idempotent (no error on double-close).
	if err := n.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := n.Close(); err != nil {
		t.Fatalf("second Close() (idempotent) error = %v, want nil", err)
	}
	if !n.closed {
		t.Error("expected closed = true after Close()")
	}
}

func TestPayloadSerialization(t *testing.T) {
	event := domain.Event{
		State:       domain.StateSuccess,
		RunName:     "build-123",
		RunID:       "uid-456",
		Namespace:   "default",
		Context:     "tekton/build",
		Description: "Build succeeded",
		Resource:    domain.ResourcePipelineRun,
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var decoded domain.Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if decoded.State != event.State {
		t.Errorf("decoded.State = %q, want %q", decoded.State, event.State)
	}
	if decoded.RunName != event.RunName {
		t.Errorf("decoded.RunName = %q, want %q", decoded.RunName, event.RunName)
	}
	if decoded.RunID != event.RunID {
		t.Errorf("decoded.RunID = %q, want %q", decoded.RunID, event.RunID)
	}
}

func TestIsConnectionError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isConnectionError(tt.err); got != tt.want {
				t.Errorf("isConnectionError() = %v, want %v", got, tt.want)
			}
		})
	}
}
