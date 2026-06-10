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

	// HTTP RED metrics (D-30)
	HTTPRequestDuration  *prometheus.HistogramVec // {method, code}
	HTTPRequestsTotal    *prometheus.CounterVec   // {method, code}
	HTTPRequestsInFlight prometheus.Gauge
}

// NewCollectors creates and registers all collectors with the given registerer.
func NewCollectors(reg prometheus.Registerer) *Collectors {
	// Custom histogram buckets: 1ms to 30s
	buckets := []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}

	c := &Collectors{
		EventsReceived: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tekton_relay_events_received_total",
				Help: "Total events received by type and source",
			},
			[]string{"type", "source"},
		),
		EventsProcessed: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tekton_relay_events_processed_total",
				Help: "Total events processed by handler and status",
			},
			[]string{"handler", "status"},
		),
		HandlerDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "tekton_relay_handler_duration_seconds",
				Help:    "Handler execution duration in seconds",
				Buckets: buckets,
			},
			[]string{"handler"},
		),
		DeduperHits: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "tekton_relay_deduper_hits_total",
				Help: "Total deduper cache hits",
			},
		),
		PipelineErrors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tekton_relay_pipeline_errors_total",
				Help: "Total pipeline errors by stage",
			},
			[]string{"stage"},
		),
		EventsFiltered: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tekton_relay_events_filtered_total",
				Help: "Total events filtered by reason",
			},
			[]string{"reason"},
		),
		ChainDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "tekton_relay_chain_duration_seconds",
				Help:    "Full chain execution duration in seconds",
				Buckets: buckets,
			},
			[]string{"result"},
		),
		DedupeCacheSize: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "tekton_relay_dedupe_cache_size",
				Help: "Current number of entries in the deduper cache",
			},
		),
		HandlersRegistered: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "tekton_relay_handlers_registered",
				Help: "Number of registered action handlers",
			},
		),
		ErrorsPermanent: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "tekton_relay_errors_permanent_total",
				Help: "Total permanent (non-retryable) errors by reason",
			},
			[]string{"reason"},
		),
		EventsBackpressure: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "tekton_relay_events_backpressure_total",
				Help: "Total events dropped due to backpressure",
			},
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
		c.HTTPRequestDuration,
		c.HTTPRequestsTotal,
		c.HTTPRequestsInFlight,
	)

	return c
}
