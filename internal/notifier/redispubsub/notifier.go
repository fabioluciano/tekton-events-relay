// Package redispubsub implements a notifier that publishes pipeline events
// to a Redis Pub/Sub channel.
//
// It maintains a persistent go-redis client opened at construction time and
// closed via notifier.Closer. Connection and transient publish errors are
// wrapped as RetryableError so the pipeline re-dispatches them.
package redispubsub

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/redis/go-redis/v9"
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

// Config holds the Redis Pub/Sub notifier configuration.
type Config struct {
	Name     string // instance name
	Address  string // Redis address (host:port)
	Channel  string // target channel
	Password string // optional password
	DB       int    // Redis DB number
	Log      *zap.Logger
}

// Notifier publishes pipeline events to a Redis Pub/Sub channel.
type Notifier struct {
	cfg    Config
	rdb    *redis.Client
	log    *zap.Logger
	closed bool
	mu     sync.Mutex
}

// New creates a Redis Pub/Sub notifier with a persistent client.
func New(cfg Config) (*Notifier, error) {
	if cfg.Address == "" {
		return nil, fmt.Errorf("redispubsub: address is required")
	}
	if cfg.Channel == "" {
		return nil, fmt.Errorf("redispubsub: channel is required")
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Address,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	// Verify connectivity.
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close() //nolint:errcheck // best-effort cleanup on error path
		return nil, fmt.Errorf("redispubsub: ping: %w", err)
	}

	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}

	return &Notifier{
		cfg: cfg,
		rdb: rdb,
		log: log,
	}, nil
}

// Name returns the notifier instance name.
func (n *Notifier) Name() string { return n.cfg.Name }

// Type returns the action type.
func (n *Notifier) Type() notifier.ActionType { return notifier.ActionNotify }

// Handle publishes the event to the Redis Pub/Sub channel as a JSON message.
func (n *Notifier) Handle(ctx context.Context, e domain.Event) error {
	n.mu.Lock()
	if n.closed {
		n.mu.Unlock()
		return fmt.Errorf("redispubsub: client is closed")
	}
	n.mu.Unlock()

	payload, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("redispubsub: marshal event: %w", err)
	}

	if err := n.rdb.Publish(ctx, n.cfg.Channel, payload).Err(); err != nil {
		if isConnectionError(err) {
			return apperrors.NewRetryable(err, "redispubsub_connection")
		}
		return fmt.Errorf("redispubsub: publish: %w", err)
	}

	n.log.Debug("redispubsub event published",
		zap.String("channel", n.cfg.Channel),
		zap.String("state", string(e.State)),
		zap.String("run_name", e.RunName),
	)

	return nil
}

// Close shuts down the Redis client. Idempotent.
func (n *Notifier) Close() error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.closed {
		return nil
	}
	n.closed = true

	if n.rdb != nil {
		if err := n.rdb.Close(); err != nil {
			return fmt.Errorf("redispubsub: close: %w", err)
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
	return strings.Contains(s, "connection refused") ||
		strings.Contains(s, "connection closed") ||
		strings.Contains(s, "EOF") ||
		strings.Contains(s, "timeout") ||
		strings.Contains(s, "context canceled") ||
		strings.Contains(s, "context deadline exceeded") ||
		strings.Contains(s, "redis: client is closed")
}
