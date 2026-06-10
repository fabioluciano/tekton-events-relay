package main

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"go.uber.org/zap"

	"github.com/fabioluciano/tekton-events-relay/internal/config"
	"github.com/fabioluciano/tekton-events-relay/internal/event"
	"github.com/fabioluciano/tekton-events-relay/internal/factory"
	"github.com/fabioluciano/tekton-events-relay/internal/notifier"
	"github.com/fabioluciano/tekton-events-relay/internal/pipeline"
)

const reloadDebounce = 500 * time.Millisecond

// registryHolder lets the HTTP server and dispatcher observe registry swaps
// performed by config reloads without rebuilding the server.
type registryHolder struct {
	p atomic.Pointer[notifier.Registry]
}

func newRegistryHolder(reg *notifier.Registry) *registryHolder {
	h := &registryHolder{}
	h.p.Store(reg)
	return h
}

// Names implements httpx.HandlerSource.
func (h *registryHolder) Names() []string { return h.p.Load().Names() }

// All implements pipeline.HandlerSource.
func (h *registryHolder) All() []notifier.ActionHandler { return h.p.Load().All() }

// Lookup delegates to the current registry.
func (h *registryHolder) Lookup(name string) notifier.ActionHandler { return h.p.Load().Lookup(name) }

// chainHolder lets the HTTP handler observe chain swaps performed by config
// reloads. It is itself a pipeline.Handler delegating to the current chain.
type chainHolder struct {
	p atomic.Pointer[pipeline.Handler]
}

func newChainHolder(chain pipeline.Handler) *chainHolder {
	h := &chainHolder{}
	h.p.Store(&chain)
	return h
}

// Handle delegates to the currently installed chain.
func (h *chainHolder) Handle(ctx context.Context, env *event.Envelope) error {
	return (*h.p.Load()).Handle(ctx, env)
}

// SetNext is a no-op: the holder always wraps a complete chain.
func (h *chainHolder) SetNext(pipeline.Handler) {}

// reload loads, validates and applies a new configuration. Handlers and the
// processing chain are rebuilt and swapped atomically; in-flight events keep
// using the old chain. An invalid config leaves the current one untouched.
// Server, store, dlq, logging and tracing sections require a restart.
func (a *app) reload() {
	cfg, err := config.Load(a.configPath)
	if err != nil {
		a.observeReload("failure")
		a.log.Error("config reload: load failed, keeping current configuration", zap.Error(err))
		return
	}

	reg, err := factory.BuildAll(cfg, a.log, a.buildOpts...)
	if err != nil {
		a.observeReload("failure")
		a.log.Error("config reload: handler build failed, keeping current configuration", zap.Error(err))
		return
	}

	warnImmutableChanges(a.cfg, cfg, a.log)

	chain := buildChain(cfg, a.regHolder, a.log, a.collectors, a.store, a.status)

	a.regHolder.p.Store(reg)
	a.chainHolder.p.Store(&chain)
	a.collectors.HandlersRegistered.Set(float64(len(reg.Names())))

	a.observeReload("success")
	a.log.Info("configuration reloaded",
		zap.Strings("handlers", reg.Names()))
}

func (a *app) observeReload(result string) {
	if a.collectors != nil {
		a.collectors.ConfigReloads.WithLabelValues(result).Inc()
	}
}

// warnImmutableChanges flags config sections that only apply after restart.
func warnImmutableChanges(old, next *config.Config, log *zap.Logger) {
	if !reflect.DeepEqual(old.Server, next.Server) {
		log.Warn("config reload: server section changed; changes require a restart")
	}
	if !reflect.DeepEqual(old.Store, next.Store) {
		log.Warn("config reload: store section changed; changes require a restart")
	}
	if !reflect.DeepEqual(old.DLQ, next.DLQ) {
		log.Warn("config reload: dlq section changed; changes require a restart")
	}
	if !reflect.DeepEqual(old.Logging, next.Logging) {
		log.Warn("config reload: logging section changed; changes require a restart")
	}
	if !reflect.DeepEqual(old.Tracing, next.Tracing) {
		log.Warn("config reload: tracing section changed; changes require a restart")
	}
}

// watchConfig triggers reloads on SIGHUP and on config file changes
// (fsnotify on the directory, debounced — covers Kubernetes ConfigMap
// symlink swaps). Returns when ctx is done.
func (a *app) watchConfig(ctx context.Context) {
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)

	var fsEvents chan fsnotify.Event
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		a.log.Warn("config watcher unavailable, reload via SIGHUP only", zap.Error(err))
	} else {
		defer func() { _ = watcher.Close() }()
		if err := watcher.Add(filepath.Dir(a.configPath)); err != nil {
			a.log.Warn("config watcher unavailable, reload via SIGHUP only", zap.Error(err))
		} else {
			fsEvents = watcher.Events
		}
	}

	var debounce *time.Timer
	debounceCh := make(chan struct{}, 1)
	scheduleReload := func() {
		if debounce != nil {
			debounce.Stop()
		}
		debounce = time.AfterFunc(reloadDebounce, func() {
			select {
			case debounceCh <- struct{}{}:
			default:
			}
		})
	}

	for {
		select {
		case <-ctx.Done():
			if debounce != nil {
				debounce.Stop()
			}
			return
		case <-hup:
			a.log.Info("SIGHUP received, reloading configuration")
			a.reload()
		case ev, ok := <-fsEvents:
			if !ok {
				fsEvents = nil
				continue
			}
			if configFileChanged(ev, a.configPath) {
				scheduleReload()
			}
		case <-debounceCh:
			a.log.Info("config file changed, reloading configuration")
			a.reload()
		}
	}
}

// configFileChanged reports whether a watcher event affects the config file,
// including Kubernetes ConfigMap atomic symlink updates (..data swaps).
func configFileChanged(ev fsnotify.Event, configPath string) bool {
	if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename|fsnotify.Remove) == 0 {
		return false
	}
	name := filepath.Base(ev.Name)
	return name == filepath.Base(configPath) || name == "..data"
}
