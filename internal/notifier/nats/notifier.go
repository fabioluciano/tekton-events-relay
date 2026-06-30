// Package nats implements a notifier that publishes pipeline events to NATS.
//
// It maintains a persistent nats.Conn opened at construction time and closed
// via notifier.Closer. Connection and transient publish errors are wrapped
// as RetryableError so the pipeline re-dispatches them.
package nats

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/nats-io/nats.go"
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

// Config holds the NATS notifier configuration.
type Config struct {
	Name            string   // instance name
	Servers         []string // NATS server URLs
	Subject         string   // target subject
	CredentialsFile string   // optional: path to NKEY/JWT credentials file
	TLSEnabled      bool     // enable TLS
	Log             *zap.Logger
}

// Notifier publishes pipeline events to NATS.
type Notifier struct {
	cfg    Config
	conn   *nats.Conn
	log    *zap.Logger
	closed bool
	mu     sync.Mutex
}

// New creates a NATS notifier with a persistent connection.
func New(cfg Config) (*Notifier, error) {
	if len(cfg.Servers) == 0 {
		return nil, fmt.Errorf("nats: at least one server is required")
	}
	if cfg.Subject == "" {
		return nil, fmt.Errorf("nats: subject is required")
	}

	opts := []nats.Option{
		nats.Name("tekton-events-relay"),
	}

	if cfg.CredentialsFile != "" {
		opts = append(opts, nats.UserCredentials(cfg.CredentialsFile))
	}

	if cfg.TLSEnabled {
		opts = append(opts, nats.Secure())
	}

	conn, err := nats.Connect(strings.Join(cfg.Servers, ","), opts...)
	if err != nil {
		return nil, fmt.Errorf("nats: connect: %w", err)
	}

	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}

	return &Notifier{
		cfg:  cfg,
		conn: conn,
		log:  log,
	}, nil
}

// Name returns the notifier instance name.
func (n *Notifier) Name() string { return n.cfg.Name }

// Provider returns the provider type identifier.
func (n *Notifier) Provider() string { return "nats" }

// Type returns the action type.
func (n *Notifier) Type() notifier.ActionType { return notifier.ActionNotify }

// Handle publishes the event to NATS as a JSON message.
//
//nolint:revive // ctx accepted for interface conformance; NATS Publish is fire-and-forget
func (n *Notifier) Handle(ctx context.Context, e domain.Event) error {
	n.mu.Lock()
	if n.closed {
		n.mu.Unlock()
		return fmt.Errorf("nats: connection is closed")
	}
	n.mu.Unlock()

	payload, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("nats: marshal event: %w", err)
	}

	if err := n.conn.Publish(n.cfg.Subject, payload); err != nil {
		if isConnectionError(err) {
			return apperrors.NewRetryable(err, "nats_connection")
		}
		return fmt.Errorf("nats: publish: %w", err)
	}

	n.log.Debug("nats event published",
		zap.String("subject", n.cfg.Subject),
		zap.String("state", string(e.State)),
		zap.String("run_name", e.RunName),
	)

	return nil
}

// Close shuts down the NATS connection. Idempotent.
func (n *Notifier) Close() error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.closed {
		return nil
	}
	n.closed = true

	if n.conn != nil {
		_ = n.conn.Drain() //nolint:errcheck // best-effort drain on shutdown
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
		strings.Contains(s, "timeout") ||
		strings.Contains(s, "no servers available") ||
		strings.Contains(s, "context canceled") ||
		strings.Contains(s, "context deadline exceeded")
}
