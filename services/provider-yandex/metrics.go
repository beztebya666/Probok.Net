package main

import (
	"fmt"
	"io"
	"math"
	"sync/atomic"
	"time"
)

type serviceMetrics struct {
	httpRequests            atomic.Uint64
	httpErrors              atomic.Uint64
	providerRequests        atomic.Uint64
	geocoderRequests        atomic.Uint64
	providerSuccesses       atomic.Uint64
	providerErrors          atomic.Uint64
	provider429             atomic.Uint64
	providerDurationNS      atomic.Uint64
	geocoderDurationNS      atomic.Uint64
	providerCandidates      atomic.Uint64
	budgetExhausted         atomic.Uint64
	bulkheadRejected        atomic.Uint64
	localRateGateRejected   atomic.Uint64
	circuitRejected         atomic.Uint64
	geocoderCircuitRejected atomic.Uint64
	geocoderFallbackError   atomic.Uint64
	geocoderFallbackEmpty   atomic.Uint64
	inFlight                atomic.Int64
	breakerState            atomic.Int64
	durationBuckets         [9]atomic.Uint64
	geocoderDurationBuckets [9]atomic.Uint64
	errorRateLimited        atomic.Uint64
	errorAuth               atomic.Uint64
	errorSubscription       atomic.Uint64
	errorAccessForbidden    atomic.Uint64
	errorUpstream           atomic.Uint64
	errorNetwork            atomic.Uint64
	errorInvalid            atomic.Uint64
	errorOther              atomic.Uint64
	billableUnits           atomic.Uint64
	estimatedCostBits       atomic.Uint64
}

var providerDurationBounds = [...]float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

func (m *serviceMetrics) observeProvider(duration time.Duration, err error, candidates int) {
	m.providerDurationNS.Add(uint64(duration))
	seconds := duration.Seconds()
	for index, bound := range providerDurationBounds {
		if seconds <= bound {
			m.durationBuckets[index].Add(1)
		}
	}
	m.durationBuckets[len(providerDurationBounds)].Add(1)
	if err != nil {
		m.providerErrors.Add(1)
		m.recordProviderErrorCode(normalizeProviderError(err).Code)
		return
	}
	m.providerSuccesses.Add(1)
	m.providerCandidates.Add(uint64(candidates))
}

func (m *serviceMetrics) observeGeocoder(duration time.Duration, err error) {
	m.geocoderDurationNS.Add(uint64(duration))
	seconds := duration.Seconds()
	for index, bound := range providerDurationBounds {
		if seconds <= bound {
			m.geocoderDurationBuckets[index].Add(1)
		}
	}
	m.geocoderDurationBuckets[len(providerDurationBounds)].Add(1)
	if err != nil {
		m.providerErrors.Add(1)
		m.recordProviderErrorCode(normalizeProviderError(err).Code)
		return
	}
	m.providerSuccesses.Add(1)
}

func (m *serviceMetrics) recordProviderErrorCode(code string) {
	switch code {
	case "PROVIDER_RATE_LIMITED":
		m.errorRateLimited.Add(1)
	case "PROVIDER_AUTHENTICATION_FAILED":
		m.errorAuth.Add(1)
	case "PROVIDER_SUBSCRIPTION_REQUIRED":
		m.errorSubscription.Add(1)
	case "PROVIDER_ACCESS_FORBIDDEN":
		m.errorAccessForbidden.Add(1)
	case "PROVIDER_UNAVAILABLE", "PROVIDER_UNEXPECTED_STATUS":
		m.errorUpstream.Add(1)
	case "PROVIDER_NETWORK_ERROR", "PROVIDER_TIMEOUT":
		m.errorNetwork.Add(1)
	case "PROVIDER_RESPONSE_INVALID", "PROVIDER_REJECTED_REQUEST", "PROVIDER_NO_ROUTE":
		m.errorInvalid.Add(1)
	default:
		m.errorOther.Add(1)
	}
}

func (m *serviceMetrics) addBillableUnits(units int, costPerUnit float64) {
	if units <= 0 {
		return
	}
	m.billableUnits.Add(uint64(units))
	delta := float64(units) * costPerUnit
	for {
		oldBits := m.estimatedCostBits.Load()
		updated := math.Float64bits(math.Float64frombits(oldBits) + delta)
		if m.estimatedCostBits.CompareAndSwap(oldBits, updated) {
			return
		}
	}
}

func (m *serviceMetrics) writePrometheus(w io.Writer) {
	requestCount := m.providerRequests.Load()
	durationSeconds := float64(m.providerDurationNS.Load()) / float64(time.Second)
	geocoderRequestCount := m.geocoderRequests.Load()
	geocoderDurationSeconds := float64(m.geocoderDurationNS.Load()) / float64(time.Second)
	_, _ = fmt.Fprintf(w, "# HELP provider_http_requests_total Internal HTTP requests handled.\n")
	_, _ = fmt.Fprintf(w, "# TYPE provider_http_requests_total counter\nprovider_http_requests_total %d\n", m.httpRequests.Load())
	_, _ = fmt.Fprintf(w, "# HELP provider_http_errors_total Internal HTTP error responses.\n")
	_, _ = fmt.Fprintf(w, "# TYPE provider_http_errors_total counter\nprovider_http_errors_total %d\n", m.httpErrors.Load())
	_, _ = fmt.Fprintf(w, "# HELP provider_requests_total Outbound provider attempts.\n")
	_, _ = fmt.Fprintf(w, "# TYPE provider_requests_total counter\nprovider_requests_total{operation=\"routes\"} %d\n", requestCount)
	_, _ = fmt.Fprintf(w, "provider_requests_total{operation=\"geocode\"} %d\n", geocoderRequestCount)
	_, _ = fmt.Fprintf(w, "# HELP provider_request_success_total Successful outbound provider attempts.\n")
	_, _ = fmt.Fprintf(w, "# TYPE provider_request_success_total counter\nprovider_request_success_total %d\n", m.providerSuccesses.Load())
	_, _ = fmt.Fprintf(w, "# HELP provider_errors_total Failed outbound provider attempts by sanitized category.\n")
	_, _ = fmt.Fprintf(w, "# TYPE provider_errors_total counter\n")
	_, _ = fmt.Fprintf(w, "provider_errors_total{code=\"rate_limited\"} %d\n", m.errorRateLimited.Load())
	_, _ = fmt.Fprintf(w, "provider_errors_total{code=\"authentication\"} %d\n", m.errorAuth.Load())
	_, _ = fmt.Fprintf(w, "provider_errors_total{code=\"subscription_required\"} %d\n", m.errorSubscription.Load())
	_, _ = fmt.Fprintf(w, "provider_errors_total{code=\"access_forbidden\"} %d\n", m.errorAccessForbidden.Load())
	_, _ = fmt.Fprintf(w, "provider_errors_total{code=\"upstream\"} %d\n", m.errorUpstream.Load())
	_, _ = fmt.Fprintf(w, "provider_errors_total{code=\"network\"} %d\n", m.errorNetwork.Load())
	_, _ = fmt.Fprintf(w, "provider_errors_total{code=\"invalid_response\"} %d\n", m.errorInvalid.Load())
	_, _ = fmt.Fprintf(w, "provider_errors_total{code=\"other\"} %d\n", m.errorOther.Load())
	_, _ = fmt.Fprintf(w, "# HELP provider_429_total Provider rate-limit responses.\n")
	_, _ = fmt.Fprintf(w, "# TYPE provider_429_total counter\nprovider_429_total %d\n", m.provider429.Load())
	_, _ = fmt.Fprintf(w, "# HELP provider_request_duration_seconds Outbound provider attempt latency.\n")
	_, _ = fmt.Fprintf(w, "# TYPE provider_request_duration_seconds histogram\n")
	for index, bound := range providerDurationBounds {
		_, _ = fmt.Fprintf(w, "provider_request_duration_seconds_bucket{operation=\"routes\",le=\"%g\"} %d\n", bound, m.durationBuckets[index].Load())
		_, _ = fmt.Fprintf(w, "provider_request_duration_seconds_bucket{operation=\"geocode\",le=\"%g\"} %d\n", bound, m.geocoderDurationBuckets[index].Load())
	}
	_, _ = fmt.Fprintf(w, "provider_request_duration_seconds_bucket{operation=\"routes\",le=\"+Inf\"} %d\n", m.durationBuckets[len(providerDurationBounds)].Load())
	_, _ = fmt.Fprintf(w, "provider_request_duration_seconds_bucket{operation=\"geocode\",le=\"+Inf\"} %d\n", m.geocoderDurationBuckets[len(providerDurationBounds)].Load())
	_, _ = fmt.Fprintf(w, "provider_request_duration_seconds_sum{operation=\"routes\"} %.9f\nprovider_request_duration_seconds_count{operation=\"routes\"} %d\n", durationSeconds, requestCount)
	_, _ = fmt.Fprintf(w, "provider_request_duration_seconds_sum{operation=\"geocode\"} %.9f\nprovider_request_duration_seconds_count{operation=\"geocode\"} %d\n", geocoderDurationSeconds, geocoderRequestCount)
	_, _ = fmt.Fprintf(w, "# HELP provider_candidates_total Normalized route candidates returned.\n")
	_, _ = fmt.Fprintf(w, "# TYPE provider_candidates_total counter\nprovider_candidates_total %d\n", m.providerCandidates.Load())
	_, _ = fmt.Fprintf(w, "# HELP provider_budget_exhausted_total Calls stopped by request budget.\n")
	_, _ = fmt.Fprintf(w, "# TYPE provider_budget_exhausted_total counter\nprovider_budget_exhausted_total %d\n", m.budgetExhausted.Load())
	_, _ = fmt.Fprintf(w, "# HELP provider_bulkhead_rejected_total Calls rejected by concurrency bulkhead.\n")
	_, _ = fmt.Fprintf(w, "# TYPE provider_bulkhead_rejected_total counter\nprovider_bulkhead_rejected_total %d\n", m.bulkheadRejected.Load())
	_, _ = fmt.Fprintf(w, "# HELP provider_local_rate_gate_rejected_total Calls rejected before provider egress to protect configured quota.\n")
	_, _ = fmt.Fprintf(w, "# TYPE provider_local_rate_gate_rejected_total counter\nprovider_local_rate_gate_rejected_total %d\n", m.localRateGateRejected.Load())
	_, _ = fmt.Fprintf(w, "# HELP provider_circuit_rejected_total Calls rejected by open circuit.\n")
	_, _ = fmt.Fprintf(w, "# TYPE provider_circuit_rejected_total counter\nprovider_circuit_rejected_total %d\n", m.circuitRejected.Load())
	_, _ = fmt.Fprintf(w, "# HELP provider_geocoder_circuit_rejected_total Address searches rejected by the geocoder circuit.\n")
	_, _ = fmt.Fprintf(w, "# TYPE provider_geocoder_circuit_rejected_total counter\nprovider_geocoder_circuit_rejected_total %d\n", m.geocoderCircuitRejected.Load())
	_, _ = fmt.Fprintf(w, "# HELP provider_geocoder_fallback_total Address searches sent to the configured fallback, by privacy-safe reason.\n")
	_, _ = fmt.Fprintf(w, "# TYPE provider_geocoder_fallback_total counter\n")
	_, _ = fmt.Fprintf(w, "provider_geocoder_fallback_total{reason=\"primary_error\"} %d\n", m.geocoderFallbackError.Load())
	_, _ = fmt.Fprintf(w, "provider_geocoder_fallback_total{reason=\"primary_empty\"} %d\n", m.geocoderFallbackEmpty.Load())
	_, _ = fmt.Fprintf(w, "# HELP provider_in_flight Current outbound provider attempts.\n")
	_, _ = fmt.Fprintf(w, "# TYPE provider_in_flight gauge\nprovider_in_flight %d\n", m.inFlight.Load())
	_, _ = fmt.Fprintf(w, "# HELP provider_circuit_breaker_state Circuit state: 0 closed, 1 open, 2 half-open.\n")
	_, _ = fmt.Fprintf(w, "# TYPE provider_circuit_breaker_state gauge\nprovider_circuit_breaker_state %d\n", m.breakerState.Load())
	_, _ = fmt.Fprintf(w, "# HELP estimated_provider_billable_units_total Estimated billable route responses; provider statistics are authoritative.\n")
	_, _ = fmt.Fprintf(w, "# TYPE estimated_provider_billable_units_total counter\nestimated_provider_billable_units_total %d\n", m.billableUnits.Load())
	_, _ = fmt.Fprintf(w, "# HELP estimated_provider_cost_total Estimated provider cost in deployment-configured accounting currency.\n")
	_, _ = fmt.Fprintf(w, "# TYPE estimated_provider_cost_total counter\nestimated_provider_cost_total %.9f\n", math.Float64frombits(m.estimatedCostBits.Load()))
}
