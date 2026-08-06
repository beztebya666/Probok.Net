package telemetry

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

type Metrics struct {
	Registry                    *prometheus.Registry
	HTTPServerRequests          *prometheus.CounterVec
	HTTPServerDuration          *prometheus.HistogramVec
	ActiveSSEConnections        prometheus.Gauge
	SSERejected                 *prometheus.CounterVec
	SSEConnectionDuration       prometheus.Histogram
	AuditRowsPurged             prometheus.Counter
	AuditFailures               *prometheus.CounterVec
	RouteSearchTotal            *prometheus.CounterVec
	RouteSearchDuration         prometheus.Histogram
	InitialCandidatesDuration   prometheus.Histogram
	EnhancedSearchDuration      prometheus.Histogram
	ProviderRequests            *prometheus.CounterVec
	ProviderRequestDuration     *prometheus.HistogramVec
	ProviderErrors              *prometheus.CounterVec
	Provider429                 prometheus.Counter
	ProviderCircuitBreakerState prometheus.Gauge
	CandidatesGenerated         prometheus.Counter
	CandidatesDeduplicated      prometheus.Counter
	GreenRouteFound             prometheus.Counter
	NoGreenRoute                prometheus.Counter
	SearchBudgetExhausted       prometheus.Counter
	ActiveRouteSearches         prometheus.Gauge
	RouteSearchRejected         *prometheus.CounterVec
	SearchFinalizationFailures  prometheus.Counter
	CandidateEvaluationFailures prometheus.Counter
	StaleSearchRecoveries       prometheus.Counter
	LowConfidenceResults        prometheus.Counter
	EstimatedProviderCost       prometheus.Counter
	SelectedExtraDistance       prometheus.Histogram
	SelectedRedDuration         prometheus.Histogram
}

func NewMetrics(service string) *Metrics {
	registry := prometheus.NewRegistry()
	factory := promauto.With(registry)
	m := &Metrics{
		Registry:                    registry,
		HTTPServerRequests:          factory.NewCounterVec(prometheus.CounterOpts{Name: "http_server_requests_total", Help: "HTTP requests by stable route and response status."}, []string{"route", "status_code"}),
		HTTPServerDuration:          factory.NewHistogramVec(prometheus.HistogramOpts{Name: "http_server_request_duration_seconds", Help: "HTTP request duration by stable route.", Buckets: prometheus.DefBuckets}, []string{"route"}),
		ActiveSSEConnections:        factory.NewGauge(prometheus.GaugeOpts{Name: "sse_active_connections", Help: "Currently active public SSE streams."}),
		SSERejected:                 factory.NewCounterVec(prometheus.CounterOpts{Name: "sse_rejected_total", Help: "SSE streams rejected by bounded admission control."}, []string{"reason"}),
		SSEConnectionDuration:       factory.NewHistogram(prometheus.HistogramOpts{Name: "sse_connection_duration_seconds", Help: "Public SSE stream lifetime.", Buckets: []float64{1, 5, 15, 30, 60, 300, 900, 1800, 3600}}),
		AuditRowsPurged:             factory.NewCounter(prometheus.CounterOpts{Name: "audit_rows_purged_total", Help: "Expired privacy-audit rows removed by retention enforcement."}),
		AuditFailures:               factory.NewCounterVec(prometheus.CounterOpts{Name: "audit_failures_total", Help: "Audit persistence or retention failures."}, []string{"operation"}),
		RouteSearchTotal:            factory.NewCounterVec(prometheus.CounterOpts{Name: "route_search_total", Help: "Route searches by terminal status."}, []string{"status", "mode"}),
		RouteSearchDuration:         factory.NewHistogram(prometheus.HistogramOpts{Name: "route_search_duration_seconds", Help: "Complete route search latency.", Buckets: []float64{.1, .25, .5, 1, 2, 3, 5, 10, 20, 30}}),
		InitialCandidatesDuration:   factory.NewHistogram(prometheus.HistogramOpts{Name: "initial_candidates_duration_seconds", Help: "Initial candidate acquisition latency."}),
		EnhancedSearchDuration:      factory.NewHistogram(prometheus.HistogramOpts{Name: "enhanced_search_duration_seconds", Help: "Enhanced search latency."}),
		ProviderRequests:            factory.NewCounterVec(prometheus.CounterOpts{Name: "provider_requests_total", Help: "Provider requests by operation and outcome."}, []string{"operation", "outcome"}),
		ProviderRequestDuration:     factory.NewHistogramVec(prometheus.HistogramOpts{Name: "provider_request_duration_seconds", Help: "Provider request latency."}, []string{"operation"}),
		ProviderErrors:              factory.NewCounterVec(prometheus.CounterOpts{Name: "provider_errors_total", Help: "Sanitized provider errors."}, []string{"category"}),
		Provider429:                 factory.NewCounter(prometheus.CounterOpts{Name: "provider_429_total", Help: "Provider rate-limit responses."}),
		ProviderCircuitBreakerState: factory.NewGauge(prometheus.GaugeOpts{Name: "provider_circuit_breaker_state", Help: "Circuit state (0 closed, 1 open, .5 half-open)."}),
		CandidatesGenerated:         factory.NewCounter(prometheus.CounterOpts{Name: "candidates_generated_total", Help: "Route candidates generated."}),
		CandidatesDeduplicated:      factory.NewCounter(prometheus.CounterOpts{Name: "candidates_deduplicated_total", Help: "Equivalent candidates discarded."}),
		GreenRouteFound:             factory.NewCounter(prometheus.CounterOpts{Name: "green_route_found_total", Help: "Searches yielding a route without confirmed red segments."}),
		NoGreenRoute:                factory.NewCounter(prometheus.CounterOpts{Name: "no_green_route_total", Help: "Searches with no eligible green route."}),
		SearchBudgetExhausted:       factory.NewCounter(prometheus.CounterOpts{Name: "search_budget_exhausted_total", Help: "Searches stopped by provider request budget."}),
		ActiveRouteSearches:         factory.NewGauge(prometheus.GaugeOpts{Name: "route_search_active", Help: "Asynchronous route searches currently admitted."}),
		RouteSearchRejected:         factory.NewCounterVec(prometheus.CounterOpts{Name: "route_search_rejected_total", Help: "Route searches rejected before admission."}, []string{"reason"}),
		SearchFinalizationFailures:  factory.NewCounter(prometheus.CounterOpts{Name: "search_finalization_failures_total", Help: "Terminal search snapshots that could not be committed after retries."}),
		CandidateEvaluationFailures: factory.NewCounter(prometheus.CounterOpts{Name: "candidate_evaluation_failures_total", Help: "Candidate evaluations isolated after an internal failure."}),
		StaleSearchRecoveries:       factory.NewCounter(prometheus.CounterOpts{Name: "stale_search_recoveries_total", Help: "Interrupted searches atomically finalized by crash recovery."}),
		LowConfidenceResults:        factory.NewCounter(prometheus.CounterOpts{Name: "low_confidence_results_total", Help: "Selected low-confidence results."}),
		EstimatedProviderCost:       factory.NewCounter(prometheus.CounterOpts{Name: "estimated_provider_cost_total", Help: "Estimated provider billing units/cost."}),
		SelectedExtraDistance: factory.NewHistogram(prometheus.HistogramOpts{
			Name: "selected_route_extra_distance_meters", Help: "Extra distance against fastest route.",
			Buckets: []float64{0, 500, 1000, 3000, 5000, 10000, 30000, 50000},
		}),
		SelectedRedDuration: factory.NewHistogram(prometheus.HistogramOpts{
			Name: "selected_route_red_duration_seconds", Help: "Estimated red duration of selected routes.",
			Buckets: []float64{0, 15, 30, 60, 120, 300, 600, 1200},
		}),
	}
	registry.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	_ = service // retained for resource correlation; metric names intentionally match the product contract.
	return m
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{})
}

func SetupTracing(ctx context.Context, serviceName string) (func(context.Context) error, error) {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		provider := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.NeverSample()))
		otel.SetTracerProvider(provider)
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
		return provider.Shutdown, nil
	}
	options := []otlptracehttp.Option{otlptracehttp.WithEndpointURL(endpoint)}
	if insecure, _ := strconv.ParseBool(os.Getenv("OTEL_EXPORTER_OTLP_INSECURE")); insecure {
		options = append(options, otlptracehttp.WithInsecure())
	}
	exporter, err := otlptracehttp.New(ctx, options...)
	if err != nil {
		return nil, err
	}
	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceName(serviceName)))
	if err != nil {
		return nil, err
	}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(2*time.Second)),
		sdktrace.WithResource(res), sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(1))),
	)
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	return provider.Shutdown, nil
}

// QuerylessRequestFilter prevents query parameters from being copied into
// automatically generated HTTP spans. Route searches use JSON request bodies;
// query-bearing endpoints (notably geosuggest) are intentionally left
// uninstrumented because their parameters can contain precise user locations.
func QuerylessRequestFilter(request *http.Request) bool {
	return request == nil || request.URL == nil || request.URL.RawQuery == ""
}

func SetupLogging(ctx context.Context, serviceName string, level slog.Level) (*slog.Logger, func(context.Context) error, error) {
	stdout := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		return slog.New(stdout), func(context.Context) error { return nil }, nil
	}
	options := []otlploghttp.Option{otlploghttp.WithEndpointURL(endpoint)}
	if insecure, _ := strconv.ParseBool(os.Getenv("OTEL_EXPORTER_OTLP_INSECURE")); insecure {
		options = append(options, otlploghttp.WithInsecure())
	}
	exporter, err := otlploghttp.New(ctx, options...)
	if err != nil {
		return nil, nil, err
	}
	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceName(serviceName)))
	if err != nil {
		return nil, nil, err
	}
	provider := sdklog.NewLoggerProvider(sdklog.WithResource(res), sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)))
	otelHandler := otelslog.NewHandler(serviceName, otelslog.WithLoggerProvider(provider))
	return slog.New(multiHandler{handlers: []slog.Handler{stdout, otelHandler}}), provider.Shutdown, nil
}

type multiHandler struct{ handlers []slog.Handler }

func (h multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h multiHandler) Handle(ctx context.Context, record slog.Record) error {
	var result error
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, record.Level) {
			result = errors.Join(result, handler.Handle(ctx, record.Clone()))
		}
	}
	return result
}

func (h multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, len(h.handlers))
	for index, handler := range h.handlers {
		handlers[index] = handler.WithAttrs(attrs)
	}
	return multiHandler{handlers: handlers}
}

func (h multiHandler) WithGroup(name string) slog.Handler {
	handlers := make([]slog.Handler, len(h.handlers))
	for index, handler := range h.handlers {
		handlers[index] = handler.WithGroup(name)
	}
	return multiHandler{handlers: handlers}
}

func Shutdown(ctx context.Context, shutdowns ...func(context.Context) error) error {
	var result error
	for i := len(shutdowns) - 1; i >= 0; i-- {
		if shutdowns[i] != nil {
			result = errors.Join(result, shutdowns[i](ctx))
		}
	}
	return result
}
