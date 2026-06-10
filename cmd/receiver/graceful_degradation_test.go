package main

import (
	"context"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/event"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/pipeline"
	"github.com/fabioluciano/tekton-events-relay/internal/tracing"
)

// emptyHandlerSource implements pipeline.HandlerSource with zero handlers.
type emptyHandlerSource struct{}

func (e *emptyHandlerSource) All() []notifier.ActionHandler { return nil }
func (e *emptyHandlerSource) Names() []string               { return nil }

func TestBuildChain_EmptyRegistryWarnsButContinues(t *testing.T) {
	core, logs := observer.New(zapcore.WarnLevel)
	log := zap.New(core)

	reg := &emptyHandlerSource{}
	dispatcher := pipeline.NewDispatcher(reg, log, nil, 10)

	// Build a minimal chain with just the dispatcher
	chain := pipeline.Build(dispatcher)

	env := &event.Envelope{
		CloudEventID:   "test-id-1",
		CloudEventType: "dev.tekton.event.taskrun.unknown.v1",
		Source:         "test-source",
		Report: domain.Event{
			RunName:  "test-run",
			Resource: "taskrun",
		},
	}

	// Should not panic; dispatcher logs a warning and continues
	err := chain.Handle(context.Background(), env)
	if err != nil {
		t.Fatalf("expected no error from empty registry dispatch, got: %v", err)
	}

	// Verify warning was logged
	if logs.Len() == 0 {
		t.Error("expected a warning log when no handlers registered")
	}

	found := false
	for _, entry := range logs.All() {
		if entry.Level == zapcore.WarnLevel {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected at least one WARN level log entry for empty handler registry")
	}
}

func TestTracingInit_EmptyEndpointContinuesWithNoopTracer(t *testing.T) {
	// Unset OTEL endpoint to ensure noop path
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	// InitGlobal with empty endpoint should not panic and return a noop cleanup
	log := zap.NewNop()
	tp, cleanup, err := tracing.InitGlobal(context.Background(), "", "test-svc", log)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if tp == nil {
		t.Error("expected non-nil TracerProvider (noop) for empty endpoint")
	}

	if cleanup == nil {
		t.Fatal("expected non-nil cleanup function")
	}

	// Cleanup should not panic
	cleanup()
}

func TestTracingInit_InvalidEndpointDoesNotPanic(t *testing.T) {
	// Init with a non-routable endpoint: provider is created lazily, no connection error
	tp, err := tracing.Init(context.Background(), "invalid-host:99999", "test-svc")
	if err != nil {
		t.Fatalf("unexpected error for invalid endpoint: %v", err)
	}
	if tp == nil {
		t.Fatal("expected non-nil TracerProvider even with invalid endpoint")
	}

	// Shutdown should not panic
	_ = tp.Shutdown(context.Background())
}

func TestBuildDecoders_ReturnsNonEmptyRegistry(t *testing.T) {
	decoders := buildDecoders()
	if decoders == nil {
		t.Fatal("expected non-nil decoder registry")
	}
	names := decoders.Names()
	if len(names) == 0 {
		t.Error("expected at least one decoder registered")
	}
}
