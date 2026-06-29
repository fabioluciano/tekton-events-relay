// Package kafka implements a notifier that publishes pipeline events to Apache Kafka.
//
// It maintains a persistent kafka.Writer (connection pool) opened at construction
// time and closed via notifier.Closer. Connection and transient publish errors
// are wrapped as RetryableError so the pipeline re-dispatches them.
package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	apperrors "github.com/fabioluciano/tekton-events-relay/internal/errors"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
)

// Compile-time interface checks.
var (
	_ notifier.ActionHandler = (*Notifier)(nil)
	_ notifier.Closer        = (*Notifier)(nil)
)

// Config holds the Kafka notifier configuration.
type Config struct {
	Name         string                    // instance name
	Brokers      []string                  // Kafka broker addresses
	Topic        string                    // target topic
	TopicFunc    func(domain.Event) string // optional: dynamic topic selection per event
	RequiredAcks int                       // -1=all, 0=none, 1=leader (default: all)
	Log          *zap.Logger
}

// Notifier publishes pipeline events to Kafka.
type Notifier struct {
	cfg    Config
	writer *kafka.Writer
	log    *zap.Logger
	closed bool
	mu     sync.Mutex
}

// New creates a Kafka notifier with a persistent writer.
func New(cfg Config) (*Notifier, error) {
	if len(cfg.Brokers) == 0 {
		return nil, fmt.Errorf("kafka: at least one broker is required")
	}
	if cfg.Topic == "" && cfg.TopicFunc == nil {
		return nil, fmt.Errorf("kafka: topic or topic_func is required")
	}

	acks := kafka.RequireAll
	if cfg.RequiredAcks != 0 {
		acks = kafka.RequiredAcks(cfg.RequiredAcks)
	}

	w := &kafka.Writer{
		Addr:         kafka.TCP(cfg.Brokers...),
		Topic:        cfg.Topic,
		Balancer:     &kafka.LeastBytes{},
		RequiredAcks: acks,
		Async:        false, // synchronous writes for reliability
	}

	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}

	return &Notifier{
		cfg:    cfg,
		writer: w,
		log:    log,
	}, nil
}

// Name returns the notifier instance name.
func (n *Notifier) Name() string { return n.cfg.Name }

// Type returns the action type.
func (n *Notifier) Type() notifier.ActionType { return notifier.ActionNotify }

// Handle publishes the event to Kafka as a JSON message.
func (n *Notifier) Handle(ctx context.Context, e domain.Event) error {
	n.mu.Lock()
	if n.closed {
		n.mu.Unlock()
		return fmt.Errorf("kafka: writer is closed")
	}
	n.mu.Unlock()

	payload, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("kafka: marshal event: %w", err)
	}

	topic := n.cfg.Topic
	if n.cfg.TopicFunc != nil {
		topic = n.cfg.TopicFunc(e)
	}

	key := e.RunID
	if key == "" {
		key = e.RunName
	}

	msg := kafka.Message{
		Key:   []byte(key),
		Value: payload,
		Topic: topic,
	}

	if err := n.writer.WriteMessages(ctx, msg); err != nil {
		if isConnectionError(err) {
			return apperrors.NewRetryable(err, "kafka_connection")
		}
		return fmt.Errorf("kafka: publish: %w", err)
	}

	n.log.Debug("kafka event published",
		zap.String("topic", topic),
		zap.String("key", key),
		zap.String("state", string(e.State)),
		zap.String("run_name", e.RunName),
	)

	return nil
}

// Close shuts down the Kafka writer. Idempotent.
func (n *Notifier) Close() error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.closed {
		return nil
	}
	n.closed = true

	if err := n.writer.Close(); err != nil {
		return fmt.Errorf("kafka: close writer: %w", err)
	}
	return nil
}

// isConnectionError checks if the error indicates a transient connection issue.
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	// kafka-go returns descriptive errors for connection issues
	return strings.Contains(s, "connection refused") ||
		strings.Contains(s, "broker: ") ||
		strings.Contains(s, "no available partitions") ||
		strings.Contains(s, "context canceled") ||
		strings.Contains(s, "context deadline exceeded")
}
