package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/dlq"
	"github.com/fabioluciano/tekton-events-relay/internal/event"
	"github.com/fabioluciano/tekton-events-relay/internal/http/middleware"
	"github.com/fabioluciano/tekton-events-relay/internal/metrics"
	"github.com/fabioluciano/tekton-events-relay/internal/pipeline"
	"github.com/fabioluciano/tekton-events-relay/internal/secrets"
	"github.com/fabioluciano/tekton-events-relay/internal/store"
	"github.com/fabioluciano/tekton-events-relay/internal/tracing"
)

// healthHandler is a minimal liveness/readiness handler.
type healthHandler struct {
	checks []func() error
	status *pipeline.StatusTracker
	store  store.Store
}

func (h *healthHandler) addCheck(fn func() error) {
	h.checks = append(h.checks, fn)
}

func (h *healthHandler) liveEndpoint(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// readyEndpoint reports readiness as JSON. The handlers section exposes
// per-handler last event, last error and counters for troubleshooting.
// The store section reports connectivity health but does not affect the
// overall readiness status (degraded store does not trigger unavailable).
func (h *healthHandler) readyEndpoint(w http.ResponseWriter, _ *http.Request) {
	body := map[string]any{"status": "ok"}
	code := http.StatusOK
	for _, check := range h.checks {
		if err := check(); err != nil {
			body["status"] = "unavailable"
			body["reason"] = err.Error()
			code = http.StatusServiceUnavailable
			break
		}
	}
	if h.status != nil {
		if snapshot := h.status.Snapshot(); snapshot != nil {
			body["handlers"] = snapshot
		}
	}
	if h.store != nil {
		storeStatus := h.store.Backend()
		storeHealth := "healthy"
		if err := h.store.Ping(context.Background()); err != nil {
			storeHealth = "degraded"
		}
		body["store"] = map[string]string{
			"status":  storeHealth,
			"backend": storeStatus,
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
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
func BuildMetricsServer(addr string, promReg *prometheus.Registry, st store.Store) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(promReg, promhttp.HandlerOpts{}))

	health := &healthHandler{store: st}
	mux.HandleFunc("/readyz", health.readyEndpoint)
	mux.HandleFunc("/healthz", health.liveEndpoint)

	return &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
}

// buildHealthHandler creates a healthHandler with readiness checks for handlers and decoders.
func buildHealthHandler(registry HandlerSource, decoders DecoderSource, status *pipeline.StatusTracker, st store.Store) *healthHandler {
	health := &healthHandler{status: status, store: st}

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
// deadLetter may be nil; when set, the DLQ inspection/replay API is mounted under /api/v1/dlq.
func BuildServer(cfg *config.Config, decoders *event.Registry, chain pipeline.Handler, reg HandlerSource, log *zap.Logger, promReg *prometheus.Registry, collectors *metrics.Collectors, deadLetter dlq.Queue, status *pipeline.StatusTracker, st store.Store) (*http.Server, error) {
	mux := http.NewServeMux()

	health := buildHealthHandler(reg, decoders, status, st)
	mux.HandleFunc("/readyz", health.readyEndpoint)
	mux.HandleFunc("/healthz", health.liveEndpoint)
	mux.Handle("/metrics", promhttp.HandlerFor(promReg, promhttp.HandlerOpts{}))

	// Auth middleware is shared by the events endpoint and the DLQ API.
	var authMW func(http.Handler) http.Handler
	if cfg.Server.Auth.Enabled {
		secret, err := secrets.Resolve(cfg.Server.Auth.SecretFile, log)
		if err != nil {
			return nil, fmt.Errorf("resolve auth secret: %w", err)
		}
		authMW, err = middleware.AuthMiddleware(middleware.AuthConfig{
			Type:               cfg.Server.Auth.Type,
			Secret:             secret,
			ValidateTimestamp:  cfg.Server.Auth.ValidateTimestamp,
			TimestampTolerance: cfg.Server.Auth.TimestampTolerance,
		})
		if err != nil {
			return nil, fmt.Errorf("build auth middleware: %w", err)
		}
	}

	var replayLimiter *middleware.RateLimiter
	if deadLetter != nil {
		listHandler := http.Handler(dlqListHandler(deadLetter, log))
		replayHandler := http.Handler(dlqReplayHandler(deadLetter, chain, collectors, log))
		listHandler = middleware.PanicRecovery(log)(listHandler)
		replayHandler = middleware.PanicRecovery(log)(replayHandler)
		// Per-IP rate limiter for the replay endpoint (10 req/s, burst 20).
		replayLimiter = middleware.NewRateLimiter(dlqReplayRPS, dlqReplayBurst)
		replayHandler = replayLimiter.Middleware()(replayHandler)
		if authMW != nil {
			listHandler = authMW(listHandler)
			replayHandler = authMW(replayHandler)
		}
		mux.Handle("/api/v1/dlq", listHandler)
		mux.Handle("/api/v1/dlq/replay", replayHandler)
	}

	// Build middleware chain for /events endpoint (order matters: outermost runs first)
	handler := http.Handler(CloudEventsHandler(decoders, chain, log, collectors, cfg.Logging.Verbose.Payloads, deadLetter))

	// 1. Request logging (always active)
	handler = middleware.RequestLogging(log)(handler)

	// 2. Body limit (always active)
	maxBodySize := cfg.Server.MaxBodySize
	if maxBodySize == 0 {
		maxBodySize = config.DefaultMaxBodySize
	}
	handler = middleware.BodyLimitMiddleware(maxBodySize)(handler)

	// 3. Panic recovery (always active - outermost for safety)
	handler = middleware.PanicRecovery(log)(handler)

	// 4. Rate limit (optional)
	var rateLimiter *middleware.RateLimiter
	if cfg.Server.RateLimit.Enabled {
		rps := cfg.Server.RateLimit.RequestsPerSecond
		burst := cfg.Server.RateLimit.Burst
		if rps == 0 {
			rps = config.DefaultRateLimitRPS
		}
		if burst == 0 {
			burst = config.DefaultRateLimitBurst
		}
		rateLimiter = middleware.NewRateLimiter(rps, burst)
		handler = rateLimiter.Middleware()(handler)
	}

	// 5. Auth (optional)
	if authMW != nil {
		handler = authMW(handler)
	}

	// 6. Observability (always active)
	handler = tracing.HTTPMiddleware(handler)
	handler = metrics.HTTPMiddlewareWithCollectors(collectors)(handler)

	mux.Handle("/", handler)
	srv := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           mux,
		ReadTimeout:       time.Duration(cfg.Server.ReadTimeoutSec) * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      time.Duration(cfg.Server.WriteTimeoutSec) * time.Second,
	}
	if rateLimiter != nil {
		srv.RegisterOnShutdown(rateLimiter.Stop)
	}
	if replayLimiter != nil {
		srv.RegisterOnShutdown(replayLimiter.Stop)
	}
	return srv, nil
}
