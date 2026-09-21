// Package observability provides the three pillars the service exposes:
// structured JSON logs with correlation IDs, Prometheus metrics, and
// liveness/readiness endpoints. Nothing in here may ever log a secret;
// tokens and webhook secrets simply never reach these call sites.
package observability

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ---- Logging -------------------------------------------------------------

type ctxKey int

const requestIDKey ctxKey = 0

// NewLogger builds the process-wide slog JSON logger.
func NewLogger(level string) *slog.Logger {
	var l slog.Level
	switch level {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: l}))
}

// WithRequestID returns a context carrying the given correlation ID.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestID extracts the correlation ID from a context ("" if absent).
func RequestID(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

// LoggerFrom returns the base logger enriched with the context's request ID,
// so every log line inside one request/event shares one correlation ID.
func LoggerFrom(ctx context.Context, base *slog.Logger) *slog.Logger {
	if id := RequestID(ctx); id != "" {
		return base.With("request_id", id)
	}
	return base
}

// ---- Metrics -------------------------------------------------------------

// Metrics bundles every Prometheus series the connector exports. One instance
// is created at startup and threaded to the components that record into it.
type Metrics struct {
	Registry *prometheus.Registry

	EventsReceived    *prometheus.CounterVec // source
	EventsProcessed   *prometheus.CounterVec // source
	EventsFailed      *prometheus.CounterVec // source, reason
	EventsDuplicate   *prometheus.CounterVec // source
	TelemetryReceived prometheus.Counter

	ProviderRequests      *prometheus.CounterVec // provider, operation
	ProviderRequestErrors *prometheus.CounterVec // provider, operation, kind
	OAuthRefreshes        *prometheus.CounterVec // provider, outcome

	WebhookDuplicates *prometheus.CounterVec // provider
	WebhookRejected   *prometheus.CounterVec // provider, reason

	CommandsCreated    *prometheus.CounterVec // type
	CommandTransitions *prometheus.CounterVec // to_state
	CommandFailures    prometheus.Counter

	HTTPDuration *prometheus.HistogramVec // route, method, code
}

// NewMetrics builds and registers all series on a private registry (so tests
// can create as many instances as they like without double-registration).
func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()
	reg.MustRegister(prometheus.NewGoCollector())
	f := promauto.With(reg)
	return &Metrics{
		Registry: reg,
		EventsReceived: f.NewCounterVec(prometheus.CounterOpts{
			Name: "events_received_total", Help: "Hazard events received, by source, before validation.",
		}, []string{"source"}),
		EventsProcessed: f.NewCounterVec(prometheus.CounterOpts{
			Name: "events_processed_total", Help: "Hazard events validated, normalized and persisted.",
		}, []string{"source"}),
		EventsFailed: f.NewCounterVec(prometheus.CounterOpts{
			Name: "events_failed_total", Help: "Hazard events rejected or failed, by reason.",
		}, []string{"source", "reason"}),
		EventsDuplicate: f.NewCounterVec(prometheus.CounterOpts{
			Name: "events_duplicate_total", Help: "Hazard events dropped as idempotent duplicates.",
		}, []string{"source"}),
		TelemetryReceived: f.NewCounter(prometheus.CounterOpts{
			Name: "telemetry_received_total", Help: "Telemetry snapshots accepted from edge devices.",
		}),
		ProviderRequests: f.NewCounterVec(prometheus.CounterOpts{
			Name: "provider_requests_total", Help: "Outbound requests to providers, by operation.",
		}, []string{"provider", "operation"}),
		ProviderRequestErrors: f.NewCounterVec(prometheus.CounterOpts{
			Name: "provider_request_errors_total", Help: "Failed outbound provider requests, by kind (transient|permanent).",
		}, []string{"provider", "operation", "kind"}),
		OAuthRefreshes: f.NewCounterVec(prometheus.CounterOpts{
			Name: "oauth_token_refresh_total", Help: "OAuth token refresh attempts, by outcome (ok|error).",
		}, []string{"provider", "outcome"}),
		WebhookDuplicates: f.NewCounterVec(prometheus.CounterOpts{
			Name: "webhook_duplicates_total", Help: "Webhook deliveries dropped as duplicates.",
		}, []string{"provider"}),
		WebhookRejected: f.NewCounterVec(prometheus.CounterOpts{
			Name: "webhook_rejected_total", Help: "Webhook deliveries rejected, by reason.",
		}, []string{"provider", "reason"}),
		CommandsCreated: f.NewCounterVec(prometheus.CounterOpts{
			Name: "command_delivery_total", Help: "Commands created, by type.",
		}, []string{"type"}),
		CommandTransitions: f.NewCounterVec(prometheus.CounterOpts{
			Name: "command_transitions_total", Help: "Command state transitions, by target state.",
		}, []string{"to_state"}),
		CommandFailures: f.NewCounter(prometheus.CounterOpts{
			Name: "command_delivery_failures_total", Help: "Commands that ended FAILED.",
		}),
		HTTPDuration: f.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "Inbound HTTP request latency.",
			Buckets: prometheus.DefBuckets,
		}, []string{"route", "method", "code"}),
	}
}

// Handler serves the metrics registry in Prometheus exposition format.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{})
}

// ---- HTTP middleware -----------------------------------------------------

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Middleware wraps an http.Handler with: correlation ID (accepted from
// X-Request-ID or minted), per-request timeout, access logging, and latency
// metrics. `route` is a static label — never the raw URL — to keep metric
// cardinality bounded.
func Middleware(next http.Handler, route string, log *slog.Logger, m *Metrics, timeout time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = uuid.NewString()
		}
		ctx, cancel := context.WithTimeout(WithRequestID(r.Context(), id), timeout)
		defer cancel()

		w.Header().Set("X-Request-ID", id)
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(rec, r.WithContext(ctx))
		elapsed := time.Since(start)

		m.HTTPDuration.WithLabelValues(route, r.Method, strconv.Itoa(rec.status)).Observe(elapsed.Seconds())
		log.LogAttrs(ctx, slog.LevelInfo, "http_request",
			slog.String("request_id", id),
			slog.String("route", route),
			slog.String("method", r.Method),
			slog.Int("status", rec.status),
			slog.Duration("duration", elapsed),
			slog.String("remote", r.RemoteAddr),
		)
	})
}

// ---- Health --------------------------------------------------------------

// ReadyFunc reports whether the service can do useful work right now.
type ReadyFunc func(ctx context.Context) error

// HealthHandler answers liveness: the process is up and serving.
func HealthHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
}

// ReadyHandler answers readiness: dependencies (the database) are reachable.
// Kubernetes uses this to keep traffic away from a pod that cannot serve.
func ReadyHandler(ready ReadyFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		w.Header().Set("Content-Type", "application/json")
		if err := ready(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"unavailable"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	})
}
