package factory

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/accumulator"
	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/domain"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier/scm"
)

// BuildAccumulator creates an Handler if enabled in config.
// Returns nil if disabled. When cfg.Provider is set, looks up the named handler
// from reg; otherwise falls back to log-only mode with a warning.
// A nil buf selects the default in-memory LRU buffer.
func BuildAccumulator(cfg config.AccumulatorConfig, reg *notifier.Registry, buf accumulator.Buffer, log *zap.Logger) (notifier.ActionHandler, error) {
	if !cfg.Enabled {
		return nil, nil //nolint:nilnil // intentional: disabled accumulator returns no handler
	}

	// Apply defaults
	ttl := cfg.TTL
	if ttl == 0 {
		ttl = 30 * time.Second
	}

	maxSize := cfg.MaxSize
	if maxSize == 0 {
		maxSize = 100
	}

	var provider notifier.ActionHandler
	if cfg.Provider == nil {
		log.Warn("accumulator: no provider configured, posting to log only")
		provider = &logOnlyProvider{log: log}
	} else {
		p := reg.Lookup(cfg.Provider.Name)
		if p == nil {
			return nil, fmt.Errorf("accumulator: provider %q not found in registry", cfg.Provider.Name)
		}
		provider = p
	}

	if buf == nil {
		buf = accumulator.NewLRUBuffer(ttl, maxSize)
	}
	handler := accumulator.NewHandler(
		"accumulator",
		provider,
		buf,
		log,
	)

	if cfg.Template != "" {
		tmpl, err := scm.CompileTemplate("accumulator-summary", cfg.Template, nil)
		if err != nil {
			return nil, fmt.Errorf("accumulator: compile template: %w", err)
		}
		handler.SetTemplate(tmpl)
	}

	log.Info("accumulator handler initialized",
		zap.Duration("ttl", ttl),
		zap.Int("max_size", maxSize),
	)

	return handler, nil
}

// logOnlyProvider is a no-op provider that logs aggregate events instead of posting them.
// This allows accumulator wiring without SCM integration.
type logOnlyProvider struct {
	log *zap.Logger
}

func (l *logOnlyProvider) Name() string {
	return "log-only-provider"
}

func (l *logOnlyProvider) Type() notifier.ActionType {
	return notifier.ActionPRComment
}

func (l *logOnlyProvider) Handle(_ context.Context, event domain.Event) error {
	l.log.Info("accumulator flush (log-only mode)",
		zap.String("run_id", event.RunID),
		zap.String("run_name", event.RunName),
		zap.String("state", string(event.State)),
		zap.String("description", event.Description),
	)
	return nil
}
