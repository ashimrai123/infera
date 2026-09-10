// Package metrics declares the Prometheus metrics infera exposes on /metrics.
//
// All metrics are registered against the default Prometheus registry so the
// standard promhttp.Handler() picks them up automatically — no extra wiring
// needed beyond calling Register() once at startup.
package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
	// RequestsTotal counts successful completions, broken down by provider
	// and model. Use this to see which provider is handling the most traffic.
	//
	// Example query in Prometheus:
	//   rate(infera_requests_total[5m])
	RequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "infera_requests_total",
			Help: "Total number of successful LLM requests, by provider and model.",
		},
		[]string{"provider", "model"},
	)

	// ErrorsTotal counts provider-level failures (network errors, 5xx from
	// the upstream API, etc.). A spike here means a provider is struggling.
	//
	// Example query:
	//   rate(infera_request_errors_total[5m])
	ErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "infera_request_errors_total",
			Help: "Total number of failed LLM requests, by provider.",
		},
		[]string{"provider"},
	)

	// RequestDuration tracks how long each provider call takes, in seconds.
	// Stored as a histogram so Prometheus can compute percentiles (p50/p95/p99).
	//
	// Buckets chosen to cover typical LLM latency range:
	// 50ms, 100ms, 250ms, 500ms, 1s, 2.5s, 5s, 10s, 30s
	//
	// Example query:
	//   histogram_quantile(0.95, rate(infera_request_duration_seconds_bucket[5m]))
	RequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "infera_request_duration_seconds",
			Help:    "Latency of LLM provider calls in seconds.",
			Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0},
		},
		[]string{"provider"},
	)
)

// Register adds all infera metrics to the default Prometheus registry.
// Call once from main() before starting the HTTP server.
func Register() {
	prometheus.MustRegister(RequestsTotal, ErrorsTotal, RequestDuration)
}
