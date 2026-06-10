package http

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/event"
	"github.com/fabioluciano/tekton-events-relay/internal/http/middleware"
	"github.com/fabioluciano/tekton-events-relay/internal/metrics"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/pipeline"
	"github.com/fabioluciano/tekton-events-relay/internal/secrets"
	"github.com/fabioluciano/tekton-events-relay/internal/tracing"
)

// healthHandler is a minimal liveness/readiness handler.
type healthHandler struct {
	checks []func() error
}

func newHealthHandler() *healthHandler { return &healthHandler{} }

func (h *healthHandler) addCheck(fn func() error) {
	h.checks = append(h.checks, fn)
}

func (h *healthHandler) liveEndpoint(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (h *healthHandler) readyEndpoint(w http.ResponseWriter, _ *http.Request) {
	for _, check := range h.checks {
		if err := check(); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
}

// HandlerSource provides access to handler names (subset of pipeline.HandlerSource)
type HandlerSource interface {
	Names() []string
}

// DecoderSource provides access to decoder names
type DecoderSource interface {
	Names() []string
}

// BuildMetricsServer constructs a dedicated *http.Server for metrics and readiness probes.
func BuildMetricsServer(addr string, promReg *prometheus.Registry) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(promReg, promhttp.HandlerOpts{}))

	health := newHealthHandler()
	mux.HandleFunc("/readyz", health.readyEndpoint)
	mux.HandleFunc("/healthz", health.liveEndpoint)

	return &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
}

// buildHealthHandler creates a healthHandler with readiness checks for handlers and decoders.
func buildHealthHandler(registry HandlerSource, decoders DecoderSource) *healthHandler {
	health := newHealthHandler()

	health.addCheck(func() error {
		if len(registry.Names()) == 0 {
			return errors.New("no handlers registered")
		}
		return nil
	})

	health.addCheck(func() error {
		if len(decoders.Names()) == 0 {
			return errors.New("no decoders registered")
		}
		return nil
	})

	return health
}

// BuildServer constructs an *http.Server with the CloudEvents handler and health endpoint.
func BuildServer(cfg *config.Config, decoders *event.Registry, chain pipeline.Handler, reg *notifier.Registry, log *zap.Logger, promReg *prometheus.Registry, collectors *metrics.Collectors) (*http.Server, error) {
	mux := http.NewServeMux()

	health := buildHealthHandler(reg, decoders)
	mux.HandleFunc("/readyz", health.readyEndpoint)
	mux.HandleFunc("/healthz", health.liveEndpoint)
	mux.Handle("/metrics", promhttp.HandlerFor(promReg, promhttp.HandlerOpts{}))

	// Build middleware chain for /events endpoint (order matters: outermost runs first)
	handler := http.Handler(CloudEventsHandler(decoders, chain, log, collectors, cfg.Logging.Verbose.Payloads))

	// 1. Request logging (always active)
	handler = middleware.RequestLogging(log)(handler)

	// 2. Body limit (always active)
	maxBodySize := cfg.Server.MaxBodySize
	if maxBodySize == 0 {
		maxBodySize = config.DefaultMaxBodySize
	}
	handler = middleware.BodyLimitMiddleware(maxBodySize)(handler)

	// 2. Panic recovery (always active - outermost for safety)
	handler = middleware.PanicRecovery(log)(handler)

	// 3. Rate limit (optional)
	if cfg.Server.RateLimit.Enabled {
		rps := cfg.Server.RateLimit.RequestsPerSecond
		burst := cfg.Server.RateLimit.Burst
		if rps == 0 {
			rps = config.DefaultRateLimitRPS
		}
		if burst == 0 {
			burst = config.DefaultRateLimitBurst
		}
		handler = middleware.RateLimitMiddleware(rps, burst)(handler)
	}

	// 4. Auth (optional)
	if cfg.Server.Auth.Enabled {
		secret, err := secrets.Resolve(cfg.Server.Auth.SecretFile, log)
		if err != nil {
			return nil, fmt.Errorf("resolve auth secret: %w", err)
		}
		authCfg := middleware.AuthConfig{
			Type:   cfg.Server.Auth.Type,
			Secret: secret,
		}
		authMiddleware, err := middleware.AuthMiddleware(authCfg)
		if err != nil {
			return nil, fmt.Errorf("build auth middleware: %w", err)
		}
		handler = authMiddleware(handler)
	}

	// 5. Observability (always active)
	handler = tracing.HTTPMiddleware(handler)
	handler = metrics.HTTPMiddlewareWithCollectors(collectors)(handler)

	mux.Handle("/", handler)
	return &http.Server{
		Addr:         cfg.Server.Addr,
		Handler:      mux,
		ReadTimeout:  time.Duration(cfg.Server.ReadTimeoutSec) * time.Second,
		WriteTimeout: time.Duration(cfg.Server.WriteTimeoutSec) * time.Second,
	}, nil
}
