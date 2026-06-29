package notifier

import (
	"context"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
)

// DedupeHandler wraps an ActionHandler and skips execution when the
// (handler_name, cloud_event_id) pair has already been processed within
// the store's TTL window. It requires the caller (the pipeline dispatcher)
// to set the CloudEventID in the context via notifier.CloudEventIDKey.
type DedupeHandler struct {
	inner ActionHandler
	store NotifierDedupeStore
	log   *zap.Logger
}

// NewDedupeHandler creates a dedupe wrapper. When store is nil, the
// handler passes through unconditionally (safe fallback).
func NewDedupeHandler(inner ActionHandler, store NotifierDedupeStore, log *zap.Logger) ActionHandler {
	return &DedupeHandler{
		inner: inner,
		store: store,
		log:   log,
	}
}

// Name returns the inner handler's name.
func (d *DedupeHandler) Name() string { return d.inner.Name() }

// Type returns the inner handler's action type.
func (d *DedupeHandler) Type() ActionType { return d.inner.Type() }

// Close delegates to the inner handler's Close method.
func (d *DedupeHandler) Close() error { return d.inner.Close() }

// Handle checks the dedupe store before delegating. If the same
// (handler_name, cloud_event_id) pair was already seen, the event is
// silently skipped. Without a CloudEventID in context the handler
// passes through (no dedupe).
func (d *DedupeHandler) Handle(ctx context.Context, e domain.Event) error {
	if d.store == nil {
		return d.inner.Handle(ctx, e)
	}

	ceID, ok := ctx.Value(CloudEventIDKey).(string)
	if !ok || ceID == "" {
		return d.inner.Handle(ctx, e)
	}

	first, err := d.store.FirstSeen(ctx, d.inner.Name(), ceID)
	if err != nil {
		// Fail open: process the event rather than lose it.
		if d.log != nil {
			d.log.Warn("notifier dedupe store unavailable, processing without deduplication",
				zap.String("handler", d.inner.Name()),
				zap.String("ce_id", ceID),
				zap.Error(err))
		}
		return d.inner.Handle(ctx, e)
	}

	if !first {
		// Already seen — skip.
		if d.log != nil {
			d.log.Debug("notification skipped by dedupe",
				zap.String("handler", d.inner.Name()),
				zap.String("ce_id", ceID))
		}
		return nil
	}

	return d.inner.Handle(ctx, e)
}
