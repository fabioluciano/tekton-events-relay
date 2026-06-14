// Package tracing provides OpenTelemetry distributed tracing setup for tekton-events-relay.
package tracing

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/sdk/trace"
	"go.uber.org/zap"
)

// InitGlobal initializes TracerProvider with given endpoint and service name.
// Returns the tracer provider, a cleanup function, and any initialization error.
// The caller is responsible for setting the global tracer provider via otel.SetTracerProvider.
func InitGlobal(ctx context.Context, endpoint, serviceName string, insecure bool, log *zap.Logger) (*trace.TracerProvider, func(), error) {
	tp, err := Init(ctx, endpoint, serviceName, insecure)
	if err != nil {
		return nil, nil, fmt.Errorf("init tracing: %w", err)
	}
	if tp == nil {
		return nil, func() {}, nil
	}
	cleanup := func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			log.Error("shutdown tracer", zap.Error(err))
		}
	}
	return tp, cleanup, nil
}
