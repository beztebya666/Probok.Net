package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/greenroute/greenroute/internal/contracts"
	"github.com/greenroute/greenroute/internal/domain"
)

func TestSyntheticOfficialResponseContract(t *testing.T) {
	data := readContractFixture(t)
	var response yandexRouteResponse
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}

	normalized, err := normalizeYandexRoutes(validProviderRequest(), response)
	if err != nil {
		t.Fatalf("normalize response: %v", err)
	}
	if len(normalized.Candidates) != 2 {
		t.Fatalf("got %d candidates, want 2", len(normalized.Candidates))
	}
	first := normalized.Candidates[0]
	if first.Provider != "yandex" || first.DistanceMeters != 4001 || first.LiveDurationSeconds != 301 {
		t.Fatalf("unexpected first candidate: %+v", first)
	}
	if first.BaselineDurationSeconds != 0 {
		t.Fatalf("live response invented baseline duration: %d", first.BaselineDurationSeconds)
	}
	if len(first.Geometry) != 4 {
		t.Fatalf("polyline boundary point was not deduplicated: %d points", len(first.Geometry))
	}
	for _, segment := range first.Segments {
		if segment.CongestionClass != domain.CongestionUnknown {
			t.Fatalf("provider congestion must remain UNKNOWN, got %s", segment.CongestionClass)
		}
	}
	if !normalized.Candidates[1].Tolls {
		t.Fatal("toll flag was not normalized")
	}
}

func TestTrafficDisabledResponsePopulatesOnlyBaseline(t *testing.T) {
	data := readContractFixture(t)
	var response yandexRouteResponse
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatal(err)
	}
	response.TrafficType = "disabled"
	request := validProviderRequest()
	request.Traffic = false
	normalized, err := normalizeYandexRoutes(request, response)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.Candidates[0].LiveDurationSeconds != 0 || normalized.Candidates[0].BaselineDurationSeconds != 301 {
		t.Fatalf("unexpected durations: %+v", normalized.Candidates[0])
	}
}

func TestYandexAdapterRetries500WithinBudget(t *testing.T) {
	fixture := readContractFixture(t)
	var mu sync.Mutex
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		mu.Lock()
		calls++
		call := calls
		mu.Unlock()
		if call == 1 {
			return response(http.StatusInternalServerError, `{}`), nil
		}
		return response(http.StatusOK, string(fixture)), nil
	})
	cfg := testConfig()
	cfg.MaxRetries = 2
	metrics := &serviceMetrics{}
	adapter := newYandexAdapter(cfg, &http.Client{Transport: transport}, metrics)
	adapter.sleep = func(context.Context, time.Duration) error { return nil }
	request := validProviderRequest()
	request.RequestBudget = 2

	result, err := adapter.Routes(context.Background(), request)
	if err != nil {
		t.Fatalf("route failed: %v", err)
	}
	if result.RequestsUsed != 2 || calls != 2 {
		t.Fatalf("requests used=%d calls=%d, want 2", result.RequestsUsed, calls)
	}
	if result.EstimatedBillableUnits != 2 || result.BudgetRemaining != 0 {
		t.Fatalf("unexpected usage estimate: billable=%d remaining=%d", result.EstimatedBillableUnits, result.BudgetRemaining)
	}
	if metrics.providerRequests.Load() != 2 || metrics.providerSuccesses.Load() != 1 {
		t.Fatalf("unexpected metrics: attempts=%d successes=%d", metrics.providerRequests.Load(), metrics.providerSuccesses.Load())
	}
}

func TestTransient5xxStatusesAreRetryable(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			err := normalizeProviderError(mapYandexStatus(status, "", time.Now()))
			if err.Code != "PROVIDER_UNAVAILABLE" || !err.Retryable {
				t.Fatalf("status %d mapped to %#v", status, err)
			}
		})
	}
}

func TestCallerCancellationDoesNotPoisonCircuitBreaker(t *testing.T) {
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	cfg := testConfig()
	cfg.CircuitBreakerThreshold = 1
	adapter := newYandexAdapter(cfg, &http.Client{Transport: transport}, &serviceMetrics{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := adapter.Routes(ctx, validProviderRequest())
	if err == nil || normalizeProviderError(err).Code != "REQUEST_CANCELLED" {
		t.Fatalf("got %v, want caller cancellation", err)
	}
	if state := adapter.breaker.State(); state != breakerClosed {
		t.Fatalf("caller cancellation opened breaker: %v", state)
	}
}

func TestYandexAdapterDoesNotRetryPastBudget(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		return response(http.StatusInternalServerError, `{}`), nil
	})
	cfg := testConfig()
	cfg.MaxRetries = 2
	metrics := &serviceMetrics{}
	adapter := newYandexAdapter(cfg, &http.Client{Transport: transport}, metrics)
	adapter.sleep = func(context.Context, time.Duration) error { return nil }
	request := validProviderRequest()
	request.RequestBudget = 1

	_, err := adapter.Routes(context.Background(), request)
	if err == nil || normalizeProviderError(err).Code != "PROVIDER_BUDGET_EXHAUSTED" {
		t.Fatalf("got %v, want budget exhausted", err)
	}
	if calls != 1 {
		t.Fatalf("got %d calls, want 1", calls)
	}
}

func TestYandexAdapterHonorsRetryAfter(t *testing.T) {
	fixture := readContractFixture(t)
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			result := response(http.StatusTooManyRequests, `{}`)
			result.Header.Set("Retry-After", "3")
			return result, nil
		}
		return response(http.StatusOK, string(fixture)), nil
	})
	cfg := testConfig()
	metrics := &serviceMetrics{}
	adapter := newYandexAdapter(cfg, &http.Client{Transport: transport}, metrics)
	adapter.cooldownJitter = func(time.Duration) time.Duration { return 0 }
	var slept time.Duration
	adapter.sleep = func(_ context.Context, duration time.Duration) error {
		slept = duration
		return nil
	}
	request := validProviderRequest()
	request.RequestBudget = 2
	if _, err := adapter.Routes(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if slept != 3*time.Second {
		t.Fatalf("slept %s, want Retry-After 3s", slept)
	}
	if metrics.provider429.Load() != 1 {
		t.Fatalf("429 metric=%d, want 1", metrics.provider429.Load())
	}
}

func TestYandexAdapterDoesNotRetry400(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		return response(http.StatusBadRequest, `{"errors":["sensitive upstream details"]}`), nil
	})
	adapter := newYandexAdapter(testConfig(), &http.Client{Transport: transport}, &serviceMetrics{})
	adapter.sleep = func(context.Context, time.Duration) error {
		t.Fatal("non-retryable 400 attempted to sleep")
		return nil
	}
	_, err := adapter.Routes(context.Background(), validProviderRequest())
	if err == nil || normalizeProviderError(err).Code != "PROVIDER_REJECTED_REQUEST" {
		t.Fatalf("got %v, want rejected request", err)
	}
	if strings.Contains(normalizeProviderError(err).Message, "sensitive") {
		t.Fatal("raw provider error leaked into normalized message")
	}
	if calls != 1 {
		t.Fatalf("got %d calls, want 1", calls)
	}
}

func TestBuildYandexRouteURLIsFixedAndProviderKeyStaysServerSide(t *testing.T) {
	cfg := testConfig()
	cfg.YandexAPIKey = "server-secret"
	request := validProviderRequest()
	request.Alternatives = 2
	request.AvoidTolls = true
	request.AvoidUnpaved = true
	request.AvoidZones = []contracts.AvoidZone{{Points: []domain.GeoPoint{
		{Latitude: 55.70, Longitude: 37.50},
		{Latitude: 55.71, Longitude: 37.51},
		{Latitude: 55.72, Longitude: 37.50},
	}}}

	raw, err := buildYandexRouteURL(cfg, request)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "https" || parsed.Host != "api.routing.yandex.net" || parsed.Path != "/v2/route" {
		t.Fatalf("unsafe endpoint: %s", parsed.Redacted())
	}
	query := parsed.Query()
	if query.Get("apikey") != "server-secret" || query.Get("results") != "3" || query.Get("mode") != "driving" {
		t.Fatalf("unexpected query: %#v", query)
	}
	if query.Get("waypoints") != "55.7500000,37.6100000|55.7700000,37.6400000" {
		t.Fatalf("coordinate order changed: %q", query.Get("waypoints"))
	}
	if len(query["avoid_zones"]) != 1 {
		t.Fatalf("avoid zones missing: %#v", query)
	}
}

func TestYandexCapabilitiesExposeExactSelectedAPIVersions(t *testing.T) {
	document := officialCapabilities(testConfig(), providerModeYandex)
	if len(document.APIIntegrations) != 2 {
		t.Fatalf("api integrations=%#v, want router and geocoder", document.APIIntegrations)
	}
	router := document.APIIntegrations[0]
	geocoder := document.APIIntegrations[1]
	if router.ID != "yandex-route-details" || router.APIVersion != yandexRouterAPIVersion || router.Capability != apiCapabilityRouting || router.Role != apiRolePrimary || router.State != apiStateActive {
		t.Fatalf("unexpected router integration: %#v", router)
	}
	if geocoder.ID != "yandex-http-geocoder" || geocoder.APIVersion != yandexGeocoderAPIVersion || geocoder.Capability != apiCapabilityAddressSearch || geocoder.Role != apiRolePrimary || geocoder.State != apiStateActive {
		t.Fatalf("unexpected geocoder integration: %#v", geocoder)
	}
}

func TestCircuitBreakerRejectsAfterThreshold(t *testing.T) {
	breaker := newCircuitBreaker(2, time.Minute)
	if !breaker.Allow() {
		t.Fatal("fresh breaker rejected request")
	}
	breaker.Failure()
	if !breaker.Allow() {
		t.Fatal("breaker opened before threshold")
	}
	breaker.Failure()
	if breaker.Allow() {
		t.Fatal("open breaker allowed request")
	}
}

func readContractFixture(t *testing.T) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "tests", "contract", "provider-yandex", "yandex-route-v2.synthetic.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

func validProviderRequest() contracts.ProviderRouteRequest {
	return contracts.ProviderRouteRequest{
		RequestID:     "test-request-1",
		Origin:        domain.GeoPoint{Latitude: 55.75, Longitude: 37.61},
		Destination:   domain.GeoPoint{Latitude: 55.77, Longitude: 37.64},
		Traffic:       true,
		Alternatives:  1,
		RequestBudget: 3,
		DeadlineMS:    5_000,
	}
}

func testConfig() Config {
	return Config{
		Environment: "test", HTTPAddr: ":8082", ProviderMode: providerModeYandex, AddressProviderMode: addressProviderAuto, YandexAPIKey: "test-key", YandexRouterBaseURL: "https://api.routing.yandex.net", YandexGeocoderAPIKey: "geocoder-test-key", YandexGeocoderBaseURL: "https://geocode-maps.yandex.ru", YandexMaxResults: 3,
		DGISRoutingBaseURL: "https://routing.api.2gis.com", DGISMaxResults: 3, DGISRateLimitPerMinute: 5, DGISMonthlyLimit: 1000, DGISGeocoderRatePerMinute: 600, DGISGeocoderLocationBias: defaultDGISGeocoderLocationBias,
		ConnectTimeout: time.Second, RequestTimeout: time.Second, ResponseHeaderTimeout: time.Second,
		BulkheadWaitTimeout: 0, MaxRetries: 2, RetryBaseDelay: time.Millisecond, RetryMaxDelay: 10 * time.Millisecond,
		MaxConcurrency: 4, CircuitBreakerThreshold: 5, CircuitBreakerOpenDuration: time.Minute,
		ShutdownTimeout: time.Second, MaxRequestBodyBytes: 128 << 10, MaxProviderResponseBytes: 4 << 20,
		ProviderCostPerBillableUnit: 0, StubScenario: "normal", StubDelay: time.Millisecond,
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func response(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
