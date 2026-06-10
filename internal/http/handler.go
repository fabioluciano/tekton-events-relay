// Package http provides the HTTP handler and server for the tekton-events-relay receiver.
package http

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/cehttp"
	relayerrors "github.com/fabioluciano/tekton-events-relay/internal/errors"
	"github.com/fabioluciano/tekton-events-relay/internal/event"
	"github.com/fabioluciano/tekton-events-relay/internal/metrics"
	"github.com/fabioluciano/tekton-events-relay/internal/pipeline"
)

var sensitivePayloadKeys = map[string]bool{
	"token": true, "secret": true, "password": true,
	"api_key": true, "apiKey": true,
	"webhook_url": true, "webhookUrl": true, "webhookURL": true,
	"integration_key": true, "integrationKey": true,
	"app_password": true, "appPassword": true,
}

func redactPayload(data []byte) []byte {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return data
	}
	redactMap(m)
	out, err := json.Marshal(m)
	if err != nil {
		return data
	}
	return out
}

func redactMap(m map[string]any) {
	for k, v := range m {
		if sensitivePayloadKeys[k] {
			m[k] = "[REDACTED]" //nolint:goconst
			continue
		}
		switch vt := v.(type) {
		case map[string]any:
			redactMap(vt)
		case []any:
			for _, elem := range vt {
				if sub, ok := elem.(map[string]any); ok {
					redactMap(sub)
				}
			}
		}
	}
}

// CloudEventsHandler returns an HTTP handler that decodes CloudEvents and dispatches them through the processing pipeline.
func CloudEventsHandler(decoders *event.Registry, chain pipeline.Handler, log *zap.Logger, collectors *metrics.Collectors, logPayloads bool) http.HandlerFunc {
	tracer := otel.Tracer("tekton-events-relay")
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ctx, span := tracer.Start(r.Context(), "handle.cloudevent")
		defer span.End()

		// Extract trace context for correlation
		traceID := span.SpanContext().TraceID().String()
		spanID := span.SpanContext().SpanID().String()

		// Log request start
		log.Info("cloudevent_request_started",
			zap.String("trace_id", traceID),
			zap.String("span_id", spanID),
			zap.String("remote_addr", r.RemoteAddr),
		)

		// Log completion on exit
		defer func() {
			duration := time.Since(start)
			log.Info("cloudevent_request_completed",
				zap.String("trace_id", traceID),
				zap.Int64("duration_ms", duration.Milliseconds()),
			)
		}()

		ce, err := cehttp.FromRequest(r)
		if err != nil {
			span.RecordError(err)
			// Log parse failure with context
			log.Warn("cloudevent_parse_failed",
				zap.String("trace_id", traceID),
				zap.String("remote_addr", r.RemoteAddr),
				zap.String("content_type", r.Header.Get("Content-Type")),
				zap.Error(err),
			)
			http.Error(w, "not a cloudevent: "+err.Error(), http.StatusBadRequest)
			return
		}

		span.SetAttributes(
			attribute.String("cloudevent.id", ce.ID),
			attribute.String("cloudevent.type", ce.Type),
			attribute.String("cloudevent.source", ce.Source),
		)

		// Log CloudEvent metadata at DEBUG
		log.Debug("cloudevent_received",
			zap.String("trace_id", traceID),
			zap.String("ce_id", ce.ID),
			zap.String("ce_type", ce.Type),
			zap.String("ce_source", ce.Source),
			zap.String("ce_time", ce.Time),
		)

		logCloudEvent(log, ce, logPayloads)

		env, ok := decodeEvent(decoders, ce, log, w)
		if !ok {
			return
		}

		handleChainResult(ctx, chain, env, log, collectors, w)
	}
}

func logCloudEvent(log *zap.Logger, ce *cehttp.Event, logPayloads bool) {
	if log.Level() != zap.DebugLevel {
		return
	}
	fields := []zap.Field{
		zap.String("id", ce.ID),
		zap.String("type", ce.Type),
		zap.String("source", ce.Source),
	}
	if logPayloads {
		fields = append(fields, zap.ByteString("data", redactPayload(ce.Data)))
	}
	log.Debug("cloudevent received", fields...)
}

func decodeEvent(decoders *event.Registry, ce *cehttp.Event, log *zap.Logger, w http.ResponseWriter) (*event.Envelope, bool) {
	decoder, err := decoders.Find(ce.Type)
	if err != nil {
		log.Debug("no decoder", zap.String("type", ce.Type))
		w.WriteHeader(http.StatusOK)
		return nil, false
	}
	env, err := decoder.Decode(event.RawEvent{ID: ce.ID, Type: ce.Type, Source: ce.Source, Data: ce.Data})
	if err != nil {
		log.Debug("skip event", zap.String("decoder", decoder.Name()), zap.Error(err))
		w.WriteHeader(http.StatusOK)
		return nil, false
	}
	return env, true
}

func handleChainResult(ctx context.Context, chain pipeline.Handler, env *event.Envelope, log *zap.Logger, collectors *metrics.Collectors, w http.ResponseWriter) {
	if err := chain.Handle(ctx, env); err != nil {
		if relayerrors.IsRetryable(err) {
			collectors.EventsBackpressure.Inc()
			log.Warn("retryable error, returning 503 for back-pressure",
				zap.String("ce_id", env.CloudEventID),
				zap.Error(err))
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		collectors.ErrorsPermanent.WithLabelValues("chain_error").Inc()
		log.Error("permanent error in pipeline chain",
			zap.String("ce_id", env.CloudEventID),
			zap.Error(err))
	}
	w.WriteHeader(http.StatusOK)
}
