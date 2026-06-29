// Package tracing provides OpenTelemetry distributed tracing setup for tekton-events-relay.
package tracing

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

// Init configures a global TracerProvider with an OTLP HTTP exporter.
// If endpoint is empty, a noop TracerProvider is set (feature disabled).
// When insecure is false, the exporter uses TLS (HTTPS).
// sampleRate controls the trace sampling ratio (0.0 = none, 1.0 = all).
func Init(ctx context.Context, endpoint, serviceName string, insecure bool, sampleRate float64) (*sdktrace.TracerProvider, error) {
	if endpoint == "" {
		tp := sdktrace.NewTracerProvider()
		otel.SetTracerProvider(tp)
		return tp, nil
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
		),
	)
	if err != nil {
		return nil, err
	}

	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(endpoint),
	}
	if insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}

	exporter, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(
			sdktrace.ParentBased(sdktrace.TraceIDRatioBased(sampleRate)),
		),
	)
	otel.SetTracerProvider(tp)

	return tp, nil
}
