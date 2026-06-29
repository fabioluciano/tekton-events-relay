package metrics

import "github.com/prometheus/client_golang/prometheus"

// Collectors holds all Prometheus collectors for the relay.
type Collectors struct {
	// Existing (5)
	EventsReceived  *prometheus.CounterVec   // {type, source}
	EventsProcessed *prometheus.CounterVec   // {handler, status}
	HandlerDuration *prometheus.HistogramVec // {handler}
	DeduperHits     prometheus.Counter
	PipelineErrors  *prometheus.CounterVec // {stage}

	// New (6)
	EventsFiltered     *prometheus.CounterVec   // {reason}
	ChainDuration      *prometheus.HistogramVec // {result}
	DedupeCacheSize    prometheus.Gauge
	HandlersRegistered prometheus.Gauge
	ErrorsPermanent    *prometheus.CounterVec // {reason}
	EventsBackpressure prometheus.Counter

	// Events arriving with a CloudEvent type no decoder understands {type}
	EventsUnsupportedType *prometheus.CounterVec

	// Outbound HTTP retry observability
	NotifierRetries       *prometheus.CounterVec // {host, reason}
	NotifierRateLimitHits *prometheus.CounterVec // {host}

	// State backend failures (dedupe/accumulator fail open on error) {backend, op}
	StoreErrors *prometheus.CounterVec

	// Store operation latency (instrumented wrapper) {backend, operation}
	StoreDuration *prometheus.HistogramVec

	// Store operation errors (instrumented wrapper) {backend, operation}
	StoreOpErrors *prometheus.CounterVec

	// Dead letter queue observability
	DLQSize     prometheus.Gauge
	DLQEnqueued prometheus.Counter

	// Config hot-reload outcomes {result}
	ConfigReloads *prometheus.CounterVec

	// Config reload duration (seconds) — no labels
	ConfigReloadDuration prometheus.Histogram

	// Unix seconds of the most recent config reload (success or failure)
	ConfigReloadLastTimestamp prometheus.Gauge

	// Handlers exceeding their execution deadline {handler}
	HandlerTimeouts *prometheus.CounterVec

	// Per-handler dispatch latency {handler, action}
	NotifierLatency *prometheus.HistogramVec

	// Dedupe entries evicted by the LRU capacity bound (memory backend).
	// Sustained evictions mean dedupe_size is too small for the event rate.
	DeduperEvictions prometheus.Counter

	// DLQ replay observability
	DlqReplayTotal    *prometheus.CounterVec   // {status}
	DlqReplayDuration *prometheus.HistogramVec // no labels

	// HTTP RED metrics (D-30)
	HTTPRequestDuration  *prometheus.HistogramVec // {method, code}
	HTTPRequestsTotal    *prometheus.CounterVec   // {method, code}
	HTTPRequestsInFlight prometheus.Gauge

	// Business metrics — event dimension counters
	EventsByStateTotal    *prometheus.CounterVec // {state}
	EventsByProviderTotal *prometheus.CounterVec // {provider}
	EventsByResourceTotal *prometheus.CounterVec // {resource}
}

// NewCollectors creates and registers all collectors with the given registerer.
func NewCollectors(reg prometheus.Registerer) *Collectors {
	// Custom histogram buckets: 1ms to 30s
	buckets := []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}

	c := &Collectors{
		EventsReceived: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tekton_events_relay_events_received_total",
				Help: "Total events received by type and source",
			},
			[]string{"type", "source"},
		),
		EventsProcessed: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tekton_events_relay_events_processed_total",
				Help: "Total events processed by handler and status",
			},
			[]string{"handler", "status"},
		),
		HandlerDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "tekton_events_relay_handler_duration_seconds",
				Help:    "Handler execution duration in seconds",
				Buckets: buckets,
			},
			[]string{"handler"},
		),
		DeduperHits: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "tekton_events_relay_deduper_hits_total",
				Help: "Total deduper cache hits",
			},
		),
		PipelineErrors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tekton_events_relay_pipeline_errors_total",
				Help: "Total pipeline errors by stage",
			},
			[]string{"stage"},
		),
		EventsFiltered: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tekton_events_relay_events_filtered_total",
				Help: "Total events filtered by reason",
			},
			[]string{"reason"},
		),
		ChainDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "tekton_events_relay_chain_duration_seconds",
				Help:    "Full chain execution duration in seconds",
				Buckets: buckets,
			},
			[]string{"result"},
		),
		DedupeCacheSize: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "tekton_events_relay_dedupe_cache_size",
				Help: "Current number of entries in the deduper cache",
			},
		),
		HandlersRegistered: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "tekton_events_relay_handlers_registered",
				Help: "Number of registered action handlers",
			},
		),
		ErrorsPermanent: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tekton_events_relay_errors_permanent_total",
				Help: "Total permanent (non-retryable) errors by reason",
			},
			[]string{"reason"},
		),
		EventsBackpressure: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "tekton_events_relay_events_backpressure_total",
				Help: "Total events dropped due to backpressure",
			},
		),
		EventsUnsupportedType: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tekton_events_relay_events_unsupported_type_total",
				Help: "Total events discarded because no decoder is registered for their CloudEvent type",
			},
			[]string{"type"},
		),
		NotifierRetries: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tekton_events_relay_notifier_retries_total",
				Help: "Total outbound HTTP retries by destination host and reason",
			},
			[]string{"host", "reason"},
		),
		NotifierRateLimitHits: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tekton_events_relay_notifier_rate_limit_hits_total",
				Help: "Total HTTP 429 responses received from destination hosts",
			},
			[]string{"host"},
		),
		StoreErrors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tekton_events_relay_store_errors_total",
				Help: "Total state backend failures by backend and operation (callers fail open)",
			},
			[]string{"backend", "op"},
		),
		DLQSize: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "tekton_events_relay_dlq_size",
				Help: "Current number of events in the dead letter queue",
			},
		),
		DLQEnqueued: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "tekton_events_relay_dlq_enqueued_total",
				Help: "Total events enqueued to the dead letter queue",
			},
		),
		ConfigReloads: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tekton_events_relay_config_reloads_total",
				Help: "Total configuration reload attempts by result",
			},
			[]string{"result"},
		),
		ConfigReloadDuration: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "tekton_events_relay_config_reload_duration_seconds",
				Help:    "Duration of configuration reload in seconds",
				Buckets: buckets,
			},
		),
		ConfigReloadLastTimestamp: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "tekton_events_relay_config_reload_last_timestamp",
				Help: "Unix timestamp of the last configuration reload",
			},
		),
		HandlerTimeouts: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tekton_events_relay_handler_timeouts_total",
				Help: "Total handler executions aborted by the per-handler timeout",
			},
			[]string{"handler"},
		),
		NotifierLatency: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "tekton_events_relay_notifier_latency_seconds",
				Help:    "Notifier dispatch latency by handler and action type",
				Buckets: buckets,
			},
			[]string{"handler", "action"},
		),
		DeduperEvictions: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "tekton_events_relay_deduper_evictions_total",
				Help: "Total dedupe entries evicted by the LRU capacity bound (memory backend)",
			},
		),
		StoreDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "store_operation_duration_seconds",
				Help:    "Store operation latency by backend and operation.",
				Buckets: buckets,
			},
			[]string{"backend", "operation"},
		),
		StoreOpErrors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "store_operation_errors_total",
				Help: "Total store operation errors by backend and operation.",
			},
			[]string{"backend", "operation"},
		),
		DlqReplayTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tekton_events_relay_dlq_replay_total",
				Help: "Total DLQ replay attempts by status",
			},
			[]string{"status"},
		),
		DlqReplayDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "tekton_events_relay_dlq_replay_duration_seconds",
				Help:    "Duration of DLQ replay operations in seconds",
				Buckets: buckets,
			},
			[]string{},
		),
		HTTPRequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request latency.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "code"}),
		HTTPRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total HTTP requests by method and status code.",
		}, []string{"method", "code"}),
		HTTPRequestsInFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "http_requests_in_flight",
			Help: "Current number of HTTP requests being served.",
		}),
		EventsByStateTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tekton_events_relay_events_by_state_total",
				Help: "Total events observed by pipeline state",
			},
			[]string{"state"},
		),
		EventsByProviderTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tekton_events_relay_events_by_provider_total",
				Help: "Total events observed by SCM provider",
			},
			[]string{"provider"},
		),
		EventsByResourceTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tekton_events_relay_events_by_resource_total",
				Help: "Total events observed by Tekton resource type",
			},
			[]string{"resource"},
		),
	}

	reg.MustRegister(
		c.EventsReceived,
		c.EventsProcessed,
		c.HandlerDuration,
		c.DeduperHits,
		c.PipelineErrors,
		c.EventsFiltered,
		c.ChainDuration,
		c.DedupeCacheSize,
		c.HandlersRegistered,
		c.ErrorsPermanent,
		c.EventsBackpressure,
		c.EventsUnsupportedType,
		c.NotifierRetries,
		c.NotifierRateLimitHits,
		c.StoreErrors,
		c.DLQSize,
		c.DLQEnqueued,
		c.ConfigReloads,
		c.ConfigReloadDuration,
		c.ConfigReloadLastTimestamp,
		c.HandlerTimeouts,
		c.NotifierLatency,
		c.DeduperEvictions,
		c.StoreDuration,
		c.StoreOpErrors,
		c.DlqReplayTotal,
		c.DlqReplayDuration,
		c.HTTPRequestDuration,
		c.HTTPRequestsTotal,
		c.HTTPRequestsInFlight,
		c.EventsByStateTotal,
		c.EventsByProviderTotal,
		c.EventsByResourceTotal,
	)

	return c
}
