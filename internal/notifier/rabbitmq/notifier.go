// Package rabbitmq implements a notifier that publishes pipeline events to RabbitMQ.
//
// It maintains a persistent AMQP connection and channel opened at construction
// time and closed via notifier.Closer. Connection and transient publish errors
// are wrapped as RetryableError so the pipeline re-dispatches them.
package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	amqp "github.com/rabbitmq/amqp091-go"
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

// Config holds the RabbitMQ notifier configuration.
type Config struct {
	Name       string // instance name
	URL        string // AMQP connection string
	Exchange   string // target exchange
	RoutingKey string // routing key
	Log        *zap.Logger
}

// Notifier publishes pipeline events to RabbitMQ.
type Notifier struct {
	cfg    Config
	conn   *amqp.Connection
	ch     *amqp.Channel
	log    *zap.Logger
	closed bool
	mu     sync.Mutex
}

// New creates a RabbitMQ notifier with a persistent connection and channel.
func New(cfg Config) (*Notifier, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("rabbitmq: url is required")
	}

	conn, err := amqp.Dial(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("rabbitmq: dial: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close() //nolint:errcheck // best-effort cleanup on error path
		return nil, fmt.Errorf("rabbitmq: channel: %w", err)
	}

	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}

	return &Notifier{
		cfg:  cfg,
		conn: conn,
		ch:   ch,
		log:  log,
	}, nil
}

// Name returns the notifier instance name.
func (n *Notifier) Name() string { return n.cfg.Name }

// Provider returns the provider type identifier.
func (n *Notifier) Provider() string { return "rabbitmq" }

// Type returns the action type.
func (n *Notifier) Type() notifier.ActionType { return notifier.ActionNotify }

// Handle publishes the event to RabbitMQ as a JSON message.
func (n *Notifier) Handle(ctx context.Context, e domain.Event) error {
	n.mu.Lock()
	if n.closed {
		n.mu.Unlock()
		return fmt.Errorf("rabbitmq: connection is closed")
	}
	n.mu.Unlock()

	payload, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("rabbitmq: marshal event: %w", err)
	}

	msg := amqp.Publishing{
		ContentType: "application/json",
		Body:        payload,
	}

	if err := n.ch.PublishWithContext(ctx,
		n.cfg.Exchange,
		n.cfg.RoutingKey,
		false, // mandatory
		false, // immediate
		msg,
	); err != nil {
		if isConnectionError(err) {
			return apperrors.NewRetryable(err, "rabbitmq_connection")
		}
		return fmt.Errorf("rabbitmq: publish: %w", err)
	}

	n.log.Debug("rabbitmq event published",
		zap.String("exchange", n.cfg.Exchange),
		zap.String("routing_key", n.cfg.RoutingKey),
		zap.String("state", string(e.State)),
		zap.String("run_name", e.RunName),
	)

	return nil
}

// Close shuts down the RabbitMQ channel and connection. Idempotent.
func (n *Notifier) Close() error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.closed {
		return nil
	}
	n.closed = true

	if n.ch != nil {
		if err := n.ch.Close(); err != nil && !isAlreadyClosed(err) {
			return fmt.Errorf("rabbitmq: close channel: %w", err)
		}
	}
	if n.conn != nil {
		if err := n.conn.Close(); err != nil && !isAlreadyClosed(err) {
			return fmt.Errorf("rabbitmq: close connection: %w", err)
		}
	}
	return nil
}

// isConnectionError checks if the error indicates a transient connection issue.
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "connection closed") ||
		strings.Contains(s, "connection refused") ||
		strings.Contains(s, "channel/connection is not open") ||
		strings.Contains(s, "NOT_FOUND") ||
		strings.Contains(s, "context canceled") ||
		strings.Contains(s, "context deadline exceeded")
}

// isAlreadyClosed checks if the error is from closing an already-closed resource.
func isAlreadyClosed(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "connection closed") ||
		strings.Contains(err.Error(), "channel is closed")
}
