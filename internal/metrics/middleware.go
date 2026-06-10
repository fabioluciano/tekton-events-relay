// Package metrics provides Prometheus metrics collectors and HTTP middleware.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// HTTPMiddlewareWithCollectors records HTTP request metrics using Collectors.
func HTTPMiddlewareWithCollectors(collectors *Collectors) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		// First wrap the business logic (EventsReceived counter)
		instrumented := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			collectors.EventsReceived.WithLabelValues(
				r.Header.Get("Ce-Type"),
				r.Header.Get("Ce-Source"),
			).Inc()
			next.ServeHTTP(w, r)
		})
		// Then wrap with RED instrumentation
		h := promhttp.InstrumentHandlerDuration(collectors.HTTPRequestDuration, instrumented)
		h = promhttp.InstrumentHandlerCounter(collectors.HTTPRequestsTotal, h)
		return promhttp.InstrumentHandlerInFlight(collectors.HTTPRequestsInFlight, h)
	}
}
