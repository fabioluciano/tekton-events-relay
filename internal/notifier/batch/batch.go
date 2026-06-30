// Package batch provides a generic message batching wrapper for ActionHandlers.
//
// When enabled, events are accumulated in a buffered channel and flushed as a
// batch either when MaxSize is reached or FlushInterval elapses, whichever
// comes first. Close() performs a final flush of any remaining buffered events.
//
// Each notifier that supports batching implements the Flusher interface with
// its own batch format (Slack=attachments, Teams=Adaptive Cards, Discord=embeds,
// Webhook=JSON array).
package batch

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/event"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

const (
	defaultMaxSize       = 10
	defaultFlushInterval = 5 * time.Second
)

// Flusher is implemented by notifiers that support batch sending.
// Flush receives all accumulated events and sends them as a single
// combined message. The batch may contain 1..N events.
type Flusher interface {
	Flush(ctx context.Context, events []domain.Event) error
}

// DLQEnqueuer abstracts the dead letter queue for enqueueing failed events.
type DLQEnqueuer interface {
	Enqueue(ctx context.Context, env *event.Envelope, cause error) error
}

// Config controls batching behavior.
type Config struct {
	Enabled       bool
	MaxSize       int
	FlushInterval time.Duration
}

// ApplyDefaults fills zero-value fields with sensible defaults.
func (c *Config) ApplyDefaults() {
	if c.MaxSize <= 0 {
		c.MaxSize = defaultMaxSize
	}
	if c.FlushInterval <= 0 {
		c.FlushInterval = defaultFlushInterval
	}
}

// bufferedEvent pairs a domain.Event with its CloudEvent metadata for DLQ
// construction. The CloudEvent ID is extracted from context at Handle() time.
type bufferedEvent struct {
	cloudEventID string
	event        domain.Event
}

// Handler wraps a Flusher with buffered batching. It implements
// notifier.ActionHandler so it can replace the inner handler in the pipeline.
type Handler struct {
	inner  Flusher
	name   string
	cfg    Config
	log    *zap.Logger
	dlq    DLQEnqueuer
	buffer []bufferedEvent
	ch     chan bufferedEvent
	done   chan struct{}
	mu     sync.Mutex
	closed bool
}

// New creates a batched handler. The goroutine starts immediately.
func New(inner Flusher, name string, cfg Config, log *zap.Logger, dlq DLQEnqueuer) *Handler {
	cfg.ApplyDefaults()

	h := &Handler{
		inner:  inner,
		name:   name,
		cfg:    cfg,
		log:    log,
		dlq:    dlq,
		buffer: make([]bufferedEvent, 0, cfg.MaxSize),
		ch:     make(chan bufferedEvent, cfg.MaxSize*2),
		done:   make(chan struct{}),
	}

	go h.run()
	return h
}

// Name returns the instance name.
func (h *Handler) Name() string { return h.name }

// Provider returns the provider type identifier.
func (h *Handler) Provider() string { return h.name }

// Type returns the action type.
func (h *Handler) Type() notifier.ActionType { return notifier.ActionNotify }

// Handle buffers the event. The CloudEvent ID is extracted from context
// (set by the pipeline dispatcher) for DLQ construction on flush failure.
func (h *Handler) Handle(ctx context.Context, e domain.Event) error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return fmt.Errorf("batch: handler %s is closed", h.name)
	}
	h.mu.Unlock()

	ceID, _ := ctx.Value(notifier.CloudEventIDKey).(string)

	h.ch <- bufferedEvent{cloudEventID: ceID, event: e}
	return nil
}

// Close stops the goroutine and flushes any remaining buffered events.
// Idempotent. Returns an error if the final flush fails.
// Close releases resources held by the handler.
func (h *Handler) Close() error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	h.closed = true
	close(h.ch)
	h.mu.Unlock()

	<-h.done

	h.mu.Lock()
	defer h.mu.Unlock()

	if len(h.buffer) == 0 {
		return nil
	}

	events := h.drainEvents()
	if err := h.flush(context.Background(), events); err != nil {
		return fmt.Errorf("batch: final flush for %s: %w", h.name, err)
	}
	return nil
}

// run is the background goroutine that drains the channel and flushes on
// size or time triggers.
func (h *Handler) run() {
	defer close(h.done)

	ticker := time.NewTicker(h.cfg.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case be, ok := <-h.ch:
			if !ok {
				return
			}
			h.mu.Lock()
			h.buffer = append(h.buffer, be)
			shouldFlush := len(h.buffer) >= h.cfg.MaxSize
			h.mu.Unlock()

			if shouldFlush {
				h.mu.Lock()
				batch := h.drainBuffer()
				h.mu.Unlock()

				if len(batch) > 0 {
					events := extractEvents(batch)
					if err := h.flush(context.Background(), events); err != nil {
						h.log.Error("batch flush failed",
							zap.String("handler", h.name),
							zap.Int("batch_size", len(batch)),
							zap.Error(err),
						)
						h.enqueueFailed(batch, err)
					}
				}
			}

		case <-ticker.C:
			h.mu.Lock()
			batch := h.drainBuffer()
			h.mu.Unlock()

			if len(batch) > 0 {
				events := extractEvents(batch)
				if err := h.flush(context.Background(), events); err != nil {
					h.log.Error("batch flush failed",
						zap.String("handler", h.name),
						zap.Int("batch_size", len(batch)),
						zap.Error(err),
					)
					h.enqueueFailed(batch, err)
				}
			}
		}
	}
}

// flush sends a batch of events via the inner Flusher.
func (h *Handler) flush(ctx context.Context, events []domain.Event) error {
	h.log.Debug("batch flush",
		zap.String("handler", h.name),
		zap.Int("batch_size", len(events)),
	)
	return h.inner.Flush(ctx, events)
}

// drainBuffer takes all buffered events and resets the buffer.
func (h *Handler) drainBuffer() []bufferedEvent {
	batch := make([]bufferedEvent, len(h.buffer))
	copy(batch, h.buffer)
	h.buffer = h.buffer[:0]
	return batch
}

// drainEvents drains the buffer and returns only the domain.Event slice.
func (h *Handler) drainEvents() []domain.Event {
	batch := h.drainBuffer()
	return extractEvents(batch)
}

// extractEvents pulls domain.Event values from a bufferedEvent slice.
func extractEvents(batch []bufferedEvent) []domain.Event {
	events := make([]domain.Event, len(batch))
	for i, be := range batch {
		events[i] = be.event
	}
	return events
}

// enqueueFailed sends failed events to the DLQ if configured.
func (h *Handler) enqueueFailed(batch []bufferedEvent, cause error) {
	if h.dlq == nil {
		return
	}
	for _, be := range batch {
		env := &event.Envelope{
			CloudEventID: be.cloudEventID,
			Report:       be.event,
		}
		if err := h.dlq.Enqueue(context.Background(), env, cause); err != nil {
			h.log.Error("batch dlq enqueue failed",
				zap.String("handler", h.name),
				zap.String("cloud_event_id", be.cloudEventID),
				zap.Error(err),
			)
		}
	}
}
