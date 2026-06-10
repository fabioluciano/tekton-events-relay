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
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	promcollectors "github.com/prometheus/client_golang/prometheus/collectors"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/event"
	"github.com/fabioluciano/tekton-events-relay/internal/event/tekton"
	"github.com/fabioluciano/tekton-events-relay/internal/factory"
	httpx "github.com/fabioluciano/tekton-events-relay/internal/http"
	"github.com/fabioluciano/tekton-events-relay/internal/logging"
	"github.com/fabioluciano/tekton-events-relay/internal/metrics"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/pipeline"
	"github.com/fabioluciano/tekton-events-relay/internal/tracing"
)

type app struct {
	cfg        *config.Config
	log        *zap.Logger
	srv        *http.Server
	metricsSrv *http.Server
	reg        *notifier.Registry
	decoders   *event.Registry
	cleanup    func()
}

func newApp(configPath string) (*app, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		bootLog, _ := logging.New("info", logging.VerboseOpts{})
		bootLog.Error("load config", zap.Error(err))
		return nil, fmt.Errorf("load config: %w", err)
	}

	log, err := logging.New(cfg.Logging.Level, logging.VerboseOpts{Caller: cfg.Logging.Verbose.Caller})
	if err != nil {
		return nil, fmt.Errorf("initialize logger: %w", err)
	}

	tp, cleanupTracer, err := tracing.InitGlobal(context.Background(), cfg.Tracing.Endpoint, cfg.Tracing.ServiceName, log)
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

	reg, err := factory.BuildAll(cfg, log)
	if err != nil {
		cleanupTracer()
		_ = log.Sync()
		return nil, fmt.Errorf("build action handlers: %w", err)
	}

	collectors.HandlersRegistered.Set(float64(len(reg.Names())))

	decoders := buildDecoders()
	chain := buildChain(cfg, reg, log, collectors)

	srv, err := httpx.BuildServer(cfg, decoders, chain, reg, log, promReg, collectors)
	if err != nil {
		cleanupTracer()
		_ = log.Sync()
		return nil, fmt.Errorf("build server: %w", err)
	}

	var metricsSrv *http.Server
	if cfg.Server.MetricsAddr != "" {
		metricsSrv = httpx.BuildMetricsServer(cfg.Server.MetricsAddr, promReg)
	}

	cleanup := func() {
		cleanupTracer()
		_ = log.Sync()
	}

	return &app{
		cfg:        cfg,
		log:        log,
		srv:        srv,
		metricsSrv: metricsSrv,
		reg:        reg,
		decoders:   decoders,
		cleanup:    cleanup,
	}, nil
}

func (a *app) run(ctx context.Context) error {
	defer a.cleanup()

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		a.log.Info("server listening",
			zap.String("addr", a.cfg.Server.Addr),
			zap.Strings("handlers", a.reg.Names()),
			zap.Strings("decoders", a.decoders.Names()))
		if err := a.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
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

	return a.shutdown()
}

func (a *app) shutdown() error {
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

	return nil
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

func buildChain(cfg *config.Config, reg pipeline.HandlerSource, log *zap.Logger, collectors *metrics.Collectors) pipeline.Handler {
	validator := pipeline.NewValidator()
	filter := pipeline.NewEventFilter(
		cfg.Filter.AllowTaskRun,
		cfg.Filter.AllowPipelineRun,
		cfg.Filter.AllowCustomRun,
		cfg.Filter.AllowEventListener,
		cfg.Filter.IgnoreUnknown,
	)
	deduper := pipeline.NewDeduper(cfg.DedupeSize, collectors)
	enricher := pipeline.NewEnricher(cfg.DashboardURL)
	dispatcher := pipeline.NewDispatcher(reg, log, collectors, cfg.MaxConcurrency)

	return pipeline.Build(
		pipeline.WithMetrics(validator, collectors.HandlerDuration.WithLabelValues("validator"), collectors.EventsProcessed, "validator"),
		pipeline.WithMetrics(filter, collectors.HandlerDuration.WithLabelValues("filter"), collectors.EventsProcessed, "filter"),
		pipeline.WithMetrics(deduper, collectors.HandlerDuration.WithLabelValues("deduper"), collectors.EventsProcessed, "deduper"),
		pipeline.WithMetrics(enricher, collectors.HandlerDuration.WithLabelValues("enricher"), collectors.EventsProcessed, "enricher"),
		pipeline.WithMetrics(dispatcher, collectors.HandlerDuration.WithLabelValues("dispatcher"), collectors.EventsProcessed, "dispatcher"),
	)
}
