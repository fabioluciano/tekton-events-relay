// Package main provides the tekton-events-relay receiver service that listens for CloudEvents
// and dispatches notifications to configured destinations.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	promcollectors "github.com/prometheus/client_golang/prometheus/collectors"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/accumulator"
	"github.com/fabioluciano/tekton-events-relay/internal/cel"
	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/dlq"
	"github.com/fabioluciano/tekton-events-relay/internal/event"
	"github.com/fabioluciano/tekton-events-relay/internal/event/tekton"
	"github.com/fabioluciano/tekton-events-relay/internal/factory"
	httpx "github.com/fabioluciano/tekton-events-relay/internal/http"
	relayhttpx "github.com/fabioluciano/tekton-events-relay/internal/httpx"
	"github.com/fabioluciano/tekton-events-relay/internal/logging"
	"github.com/fabioluciano/tekton-events-relay/internal/metrics"
	"github.com/fabioluciano/tekton-events-relay/internal/pipeline"
	"github.com/fabioluciano/tekton-events-relay/internal/store"
	"github.com/fabioluciano/tekton-events-relay/internal/tracing"
)

type app struct {
	cfg          *config.Config
	configPath   string
	log          *zap.Logger
	srv          *http.Server
	metricsSrv   *http.Server
	regHolder    *registryHolder
	chainHolder  *chainHolder
	decoders     *event.Registry
	store        store.Store
	collectors   *metrics.Collectors
	buildOpts    []factory.BuildOption
	status       *pipeline.StatusTracker
	cleanup      func()
	shutdownOnce sync.Once
}

func newApp(configPath string) (*app, error) {
	config.CELCompileFunc = func(expr string) error {
		_, err := cel.Compile(expr)
		return err
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		bootLog, bootErr := logging.New("info", logging.VerboseOpts{})
		if bootErr != nil {
			fmt.Fprintf(os.Stderr, "failed to init logger: %v\n", bootErr)
			os.Exit(1)
		}
		bootLog.Error("load config", zap.Error(err))
		return nil, fmt.Errorf("load config: %w", err)
	}

	log, err := logging.New(cfg.Logging.Level, logging.VerboseOpts{Caller: cfg.Logging.Verbose.Caller})
	if err != nil {
		return nil, fmt.Errorf("initialize logger: %w", err)
	}

	tp, cleanupTracer, err := tracing.InitGlobal(context.Background(), cfg.Tracing.Endpoint, cfg.Tracing.ServiceName, cfg.Tracing.Insecure, cfg.Tracing.SampleRate, log)
	if err != nil {
		_ = log.Sync()
		return nil, fmt.Errorf("init tracing: %w", err)
	}
	if tp != nil {
		otel.SetTracerProvider(tp)
	}

	promReg := prometheus.NewRegistry()
	promReg.MustRegister(
		promcollectors.NewGoCollector(),
		promcollectors.NewProcessCollector(promcollectors.ProcessCollectorOpts{}),
	)
	collectors := metrics.NewCollectors(promReg)

	relayhttpx.SetDefaultRetryPolicy(relayhttpx.RetryPolicy{
		MaxAttempts:    cfg.Retry.MaxAttempts,
		InitialBackoff: cfg.Retry.InitialBackoff,
		MaxBackoff:     cfg.Retry.MaxBackoff,
	})
	relayhttpx.SetRetryMetrics(collectors.NotifierRetries, collectors.NotifierRateLimitHits)

	// State backend for dedupe and accumulation. The store outlives config
	// reloads so dedupe state survives chain rebuilds.
	rawSt, err := store.New(cfg.Store, store.Options{
		DedupeCapacity: cfg.DedupeSize,
		Log:            log,
		Collectors:     collectors,
	})
	if err != nil {
		cleanupTracer()
		_ = log.Sync()
		return nil, fmt.Errorf("build store: %w", err)
	}
	st := store.NewInstrumentedStore(rawSt, collectors.StoreDuration, collectors.StoreOpErrors)

	var buildOpts []factory.BuildOption
	// Inject the store's dedupe backend so notifiers and SCM actions can
	// optionally deduplicate by (handler_name, cloud_event_id).
	buildOpts = append(buildOpts, factory.WithDedupeStore(st.Dedupe()))

	if st.Backend() != store.BackendMemory {
		// The memory backend keeps the accumulator's original LRU buffer;
		// shared backends route accumulation through the store.
		log.Info("shared state store initialized", zap.String("backend", st.Backend()))
		buildOpts = append(buildOpts,
			factory.WithAccumulatorBuffer(accumulator.NewStoreBuffer(st.RunBuffer(), st.Backend(), collectors, log)))
	}

	reg, err := factory.BuildAll(cfg, log, buildOpts...)
	if err != nil {
		if st != nil {
			_ = st.Close()
		}
		cleanupTracer()
		_ = log.Sync()
		return nil, fmt.Errorf("build action handlers: %w", err)
	}

	collectors.HandlersRegistered.Set(float64(len(reg.Names())))

	decoders := buildDecoders()
	regHolder := newRegistryHolder(reg)
	status := pipeline.NewStatusTracker()
	chain := newChainHolder(buildChain(cfg, regHolder, log, collectors, st, status))

	var deadLetter dlq.Queue
	if cfg.DLQ.Enabled {
		deadLetter, err = dlq.NewFileQueue(cfg.DLQ.Path, cfg.DLQ.MaxSizeBytes, cfg.DLQ.RetentionDays)
		if err != nil {
			if st != nil {
				_ = st.Close()
			}
			cleanupTracer()
			_ = log.Sync()
			return nil, fmt.Errorf("build dlq: %w", err)
		}
		log.Info("dead letter queue enabled", zap.String("path", cfg.DLQ.Path))
	}

	srv, err := httpx.BuildServer(cfg, decoders, chain, regHolder, log, promReg, collectors, deadLetter, status, st)
	if err != nil {
		cleanupTracer()
		_ = log.Sync()
		return nil, fmt.Errorf("build server: %w", err)
	}

	var metricsSrv *http.Server
	if cfg.Server.MetricsAddr != "" {
		metricsSrv = httpx.BuildMetricsServer(cfg.Server.MetricsAddr, promReg, st)
	}

	cleanup := func() {
		cleanupTracer()
		_ = log.Sync()
	}

	return &app{
		cfg:         cfg,
		configPath:  configPath,
		log:         log,
		srv:         srv,
		metricsSrv:  metricsSrv,
		regHolder:   regHolder,
		chainHolder: chain,
		decoders:    decoders,
		store:       st,
		collectors:  collectors,
		buildOpts:   buildOpts,
		status:      status,
		cleanup:     cleanup,
	}, nil
}

func (a *app) run(ctx context.Context) error {
	defer a.cleanup()

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go a.watchConfig(ctx)

	go func() {
		a.log.Info("server listening",
			zap.String("addr", a.cfg.Server.Addr),
			zap.Bool("tls", a.cfg.Server.TLS.Enabled()),
			zap.Strings("handlers", a.regHolder.HandlerNames()),
			zap.Strings("decoders", a.decoders.Names()))
		var err error
		if a.cfg.Server.TLS.Enabled() {
			err = a.srv.ListenAndServeTLS(a.cfg.Server.TLS.CertFile, a.cfg.Server.TLS.KeyFile)
		} else {
			err = a.srv.ListenAndServe()
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.log.Error("http serve", zap.Error(err))
			stop()
		}
	}()

	if a.metricsSrv != nil {
		go func() {
			a.log.Info("metrics server listening", zap.String("addr", a.cfg.Server.MetricsAddr))
			if err := a.metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				a.log.Error("metrics http serve", zap.Error(err))
			}
		}()
	}

	<-ctx.Done()
	a.log.Info("shutting down")

	a.shutdown()
	return nil
}

func (a *app) shutdown() {
	a.shutdownOnce.Do(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Duration(a.cfg.Server.ShutdownTimeoutSec)*time.Second)
		defer cancel()

		if err := a.srv.Shutdown(shutdownCtx); err != nil {
			a.log.Error("shutdown main server", zap.Error(err))
		}

		if a.metricsSrv != nil {
			metricsCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
			defer c()
			if err := a.metricsSrv.Shutdown(metricsCtx); err != nil {
				a.log.Error("shutdown metrics server", zap.Error(err))
			}
		}

		for _, h := range a.regHolder.All() {
			if err := h.Close(); err != nil {
				a.log.Error("shutdown handler close",
					zap.String("handler", h.Name()),
					zap.Error(err))
			}
		}

		if a.store != nil {
			if err := a.store.Close(); err != nil {
				a.log.Error("shutdown store", zap.Error(err))
			}
		}
	})
}

func main() {
	configPath := flag.String("config", "/etc/tekton-events-relay/config.yaml", "path to config file")
	validateOnly := flag.Bool("validate", false, "validate config without starting server")
	flag.Parse()

	if *validateOnly {
		runValidation(*configPath)
		return
	}

	a, err := newApp(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize app: %v\n", err)
		os.Exit(1)
	}

	if err := a.run(context.Background()); err != nil {
		os.Exit(1)
	}
}

func buildDecoders() *event.Registry {
	r := event.NewRegistry()
	r.Register(tekton.NewTaskRunDecoder())
	r.Register(tekton.NewPipelineRunDecoder())
	r.Register(tekton.NewCustomRunDecoder())
	r.Register(tekton.NewEventListenerDecoder())
	return r
}

func runValidation(configPath string) {
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config load error: %v\n", err)
		os.Exit(1)
	}

	errs := config.ValidateAll(cfg)
	if len(errs) == 0 {
		fmt.Println("Configuration is valid")
		os.Exit(0)
	}

	fmt.Fprintf(os.Stderr, "Configuration has %d error(s):\n\n", len(errs))
	for _, e := range errs {
		fmt.Fprintf(os.Stderr, "  - %s\n", e.Error())
	}
	os.Exit(1)
}

// buildChain assembles a processing chain. The chain links are fresh per
// build (config reloads create a new chain), but dedupe state lives in the
// shared store so it survives rebuilds.
func buildChain(cfg *config.Config, reg pipeline.HandlerSource, log *zap.Logger, collectors *metrics.Collectors, st store.Store, status *pipeline.StatusTracker) pipeline.Handler {
	validator := pipeline.NewValidator()
	filter := pipeline.NewEventFilter(
		cfg.Filter.AllowTaskRun,
		cfg.Filter.AllowPipelineRun,
		cfg.Filter.AllowCustomRun,
		cfg.Filter.AllowEventListener,
		cfg.Filter.IgnoreUnknown,
		cfg.Filter.AllowNamespaces,
		cfg.Filter.DenyNamespaces,
	)
	deduper := pipeline.NewDeduperWithStore(st.Dedupe(), st.Backend(), collectors, log)
	enricher := pipeline.NewEnricher(cfg.DashboardURL)
	dispatcher := pipeline.NewDispatcher(reg, log, collectors, cfg.MaxConcurrency).
		WithHandlerTimeout(cfg.HandlerTimeout).
		WithStatusTracker(status)

	return pipeline.Build(
		pipeline.WithMetrics(validator, collectors.HandlerDuration.WithLabelValues("validator"), collectors.EventsProcessed, "validator"),
		pipeline.WithMetrics(filter, collectors.HandlerDuration.WithLabelValues("filter"), collectors.EventsProcessed, "filter"),
		pipeline.WithMetrics(deduper, collectors.HandlerDuration.WithLabelValues("deduper"), collectors.EventsProcessed, "deduper"),
		pipeline.WithMetrics(enricher, collectors.HandlerDuration.WithLabelValues("enricher"), collectors.EventsProcessed, "enricher"),
		pipeline.WithMetrics(dispatcher, collectors.HandlerDuration.WithLabelValues("dispatcher"), collectors.EventsProcessed, "dispatcher"),
	)
}
