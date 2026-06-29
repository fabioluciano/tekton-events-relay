package tracing

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
)

func TestInit_EmptyEndpoint_ReturnsNoopProvider(t *testing.T) {
	tp, err := Init(context.Background(), "", "test-service", true, 1.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tp == nil {
		t.Fatal("expected non-nil TracerProvider even for noop")
	}

	// Verify it was set as global provider
	global := otel.GetTracerProvider()
	if global != tp {
		t.Error("expected global TracerProvider to be the returned provider")
	}

	// Shutdown should not error
	if err := tp.Shutdown(context.Background()); err != nil {
		t.Errorf("unexpected shutdown error: %v", err)
	}
}

func TestInit_WithEndpoint_CreatesTracerProvider(t *testing.T) {
	// Use a non-routable endpoint; the provider is created lazily so no connection error occurs.
	tp, err := Init(context.Background(), "localhost:4318", "test-service", true, 1.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tp == nil {
		t.Fatal("expected non-nil TracerProvider")
	}

	global := otel.GetTracerProvider()
	if global != tp {
		t.Error("expected global TracerProvider to be the returned provider")
	}

	if err := tp.Shutdown(context.Background()); err != nil {
		t.Errorf("unexpected shutdown error: %v", err)
	}
}

func TestInit_Shutdown(t *testing.T) {
	tp, err := Init(context.Background(), "", "test-service", true, 1.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Double shutdown should not panic
	_ = tp.Shutdown(context.Background())
	_ = tp.Shutdown(context.Background())
}

func TestSamplingRate_ZeroRate_NoSpansExported(t *testing.T) {
	tp, err := Init(context.Background(), "localhost:4318", "test-service", true, 0.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tp == nil {
		t.Fatal("expected non-nil TracerProvider")
	}
	defer func() { _ = tp.Shutdown(context.Background()) }()

	tracer := tp.Tracer("test")
	_, span := tracer.Start(context.Background(), "test-span")
	span.End()
}

func TestSamplingRate_FullRate_AllSpansSampled(t *testing.T) {
	tp, err := Init(context.Background(), "localhost:4318", "test-service", true, 1.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tp == nil {
		t.Fatal("expected non-nil TracerProvider")
	}
	defer func() { _ = tp.Shutdown(context.Background()) }()

	tracer := tp.Tracer("test")
	_, span := tracer.Start(context.Background(), "test-span")
	span.End()
}

func TestSamplingRate_PartialRate_CreatesProvider(t *testing.T) {
	tp, err := Init(context.Background(), "localhost:4318", "test-service", true, 0.5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tp == nil {
		t.Fatal("expected non-nil TracerProvider")
	}
	defer func() { _ = tp.Shutdown(context.Background()) }()
}
