package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/greenroute/greenroute/internal/contracts"
	"github.com/greenroute/greenroute/internal/domain"
)

func TestStubIsDeterministicAndClearlySynthetic(t *testing.T) {
	metrics := &serviceMetrics{}
	stub := newStubAdapter(testConfig(), metrics)
	request := validProviderRequest()
	request.Alternatives = 2

	first, err := stub.Routes(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := stub.Routes(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, _ := json.Marshal(first)
	secondJSON, _ := json.Marshal(second)
	if !bytes.Equal(firstJSON, secondJSON) {
		t.Fatalf("stub is nondeterministic:\n%s\n%s", firstJSON, secondJSON)
	}
	if len(first.Candidates) != 3 || first.Candidates[0].Provider != "stub" {
		t.Fatalf("unexpected stub response: %+v", first)
	}
	if !strings.Contains(strings.Join(first.Warnings, " "), "SYNTHETIC") {
		t.Fatal("stub response is not clearly marked synthetic")
	}
}

func TestStubFailureScenarios(t *testing.T) {
	tests := []struct {
		scenario string
		code     string
	}{
		{"rate_limit", "PROVIDER_RATE_LIMITED"},
		{"outage", "PROVIDER_UNAVAILABLE"},
	}
	for _, test := range tests {
		t.Run(test.scenario, func(t *testing.T) {
			cfg := testConfig()
			cfg.StubScenario = test.scenario
			stub := newStubAdapter(cfg, &serviceMetrics{})
			_, err := stub.Routes(context.Background(), validProviderRequest())
			if err == nil || normalizeProviderError(err).Code != test.code {
				t.Fatalf("got %v, want %s", err, test.code)
			}
		})
	}
}

func TestSlowStubHonorsCancellation(t *testing.T) {
	cfg := testConfig()
	cfg.StubScenario = "slow"
	cfg.StubDelay = time.Hour
	stub := newStubAdapter(cfg, &serviceMetrics{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := stub.Routes(ctx, validProviderRequest())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want cancellation", err)
	}
}

func TestRouteEndpointRejectsUnknownFieldsWithoutCallingProvider(t *testing.T) {
	fake := &fakeProvider{capabilities: officialCapabilities(testConfig(), providerModeStub)}
	handler, _ := testHandler(fake)
	body := `{"requestId":"safe-id","origin":{"latitude":55.75,"longitude":37.61},"destination":{"latitude":55.77,"longitude":37.64},"traffic":true,"alternatives":0,"requestBudget":1,"deadlineMs":1000,"unexpected":true}`
	request := httptest.NewRequest(http.MethodPost, "/internal/v1/routes", strings.NewReader(body))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if fake.routeCalls() != 0 {
		t.Fatal("provider called for invalid request")
	}
}

func TestRouteEndpointRejectsEachMissingRequiredProperty(t *testing.T) {
	required := []string{"requestId", "origin", "destination", "traffic", "alternatives", "avoidTolls", "avoidUnpaved", "requestBudget", "deadlineMs"}
	complete := map[string]any{
		"requestId": "safe-id", "origin": map[string]any{"latitude": 55.75, "longitude": 37.61},
		"destination": map[string]any{"latitude": 55.77, "longitude": 37.64}, "traffic": false,
		"alternatives": 0, "avoidTolls": false, "avoidUnpaved": false, "requestBudget": 1, "deadlineMs": 1000,
	}
	for _, missing := range required {
		t.Run(missing, func(t *testing.T) {
			body := make(map[string]any, len(complete)-1)
			for key, value := range complete {
				if key != missing {
					body[key] = value
				}
			}
			payload, _ := json.Marshal(body)
			fake := &fakeProvider{capabilities: officialCapabilities(testConfig(), providerModeStub)}
			handler, _ := testHandler(fake)
			request := httptest.NewRequest(http.MethodPost, "/internal/v1/routes", bytes.NewReader(payload))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest || fake.routeCalls() != 0 {
				t.Fatalf("status=%d calls=%d body=%s", response.Code, fake.routeCalls(), response.Body.String())
			}
		})
	}
}

func TestRouteEndpointSanitizesProviderFailureAndLogsNoCoordinates(t *testing.T) {
	fake := &fakeProvider{
		capabilities: officialCapabilities(testConfig(), providerModeYandex),
		routeErr:     serviceError("PROVIDER_AUTHENTICATION_FAILED", "provider credentials are invalid or lack access", http.StatusServiceUnavailable, false, errors.New("raw payload with api key secret-key")),
	}
	handler, logs := testHandler(fake)
	body := `{"requestId":"safe-id","origin":{"latitude":55.751244,"longitude":37.618423},"destination":{"latitude":55.77,"longitude":37.64},"traffic":true,"alternatives":0,"avoidTolls":false,"avoidUnpaved":false,"requestBudget":1,"deadlineMs":1000}`
	request := httptest.NewRequest(http.MethodPost, "/internal/v1/routes", strings.NewReader(body))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "secret-key") || strings.Contains(response.Body.String(), "raw payload") {
		t.Fatalf("raw provider failure leaked: %s", response.Body.String())
	}
	if strings.Contains(logs.String(), "55.751244") || strings.Contains(logs.String(), "37.618423") || strings.Contains(logs.String(), "secret-key") {
		t.Fatalf("coordinates or secret leaked to logs: %s", logs.String())
	}
}

func TestHealthCapabilitiesAndMetricsEndpoints(t *testing.T) {
	stub := newStubAdapter(testConfig(), &serviceMetrics{})
	capabilities := stub.Capabilities()
	if len(capabilities.APIIntegrations) != 0 || capabilities.AddressSearchProvider != "" || capabilities.AddressSearchEndpoint != "" {
		t.Fatalf("stub must not claim official API integrations: %#v", capabilities)
	}
	handler, _ := testHandler(stub)
	tests := []struct {
		path        string
		contentType string
		contains    string
	}{
		{"/health/live", "application/json", `"status":"ok"`},
		{"/health/ready", "application/json", `"status":"ready"`},
		{"/internal/v1/capabilities", "application/json", `"contractVersion":"v1"`},
		{"/metrics", "text/plain", "provider_requests_total"},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if !strings.Contains(response.Header().Get("Content-Type"), test.contentType) || !strings.Contains(response.Body.String(), test.contains) {
				t.Fatalf("headers=%v body=%s", response.Header(), response.Body.String())
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("responses must not be cached by intermediaries")
			}
		})
	}
}

func TestInternalEndpointsRequireConfiguredBearerToken(t *testing.T) {
	cfg := testConfig()
	cfg.InternalAPIToken = strings.Repeat("a", 32)
	metrics := &serviceMetrics{}
	provider := newStubAdapter(cfg, metrics)
	handler := newAPIServer(cfg, provider, metrics, slog.New(slog.NewJSONHandler(io.Discard, nil)))

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/internal/v1/capabilities", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", unauthorized.Code, unauthorized.Body.String())
	}

	authorizedRequest := httptest.NewRequest(http.MethodGet, "/internal/v1/capabilities", nil)
	authorizedRequest.Header.Set("Authorization", "Bearer "+cfg.InternalAPIToken)
	authorized := httptest.NewRecorder()
	handler.ServeHTTP(authorized, authorizedRequest)
	if authorized.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", authorized.Code, authorized.Body.String())
	}

	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/health/live", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("health was incorrectly protected: %d", health.Code)
	}
}

func TestYandexReadinessFailsWhenCircuitIsOpen(t *testing.T) {
	cfg := testConfig()
	adapter := newYandexAdapter(cfg, &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("offline")
	})}, &serviceMetrics{})
	adapter.breaker.threshold = 1
	adapter.breaker.Failure()
	handler, _ := testHandler(adapter)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), "offline") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestRouteValidationAvoidsInvalidProviderQueries(t *testing.T) {
	request := validProviderRequest()
	request.AvoidZones = []contracts.AvoidZone{{Points: []domain.GeoPoint{
		{Latitude: 55, Longitude: 37},
		{Latitude: 55, Longitude: 37},
		{Latitude: 55, Longitude: 37},
	}}}
	if err := validateRouteRequest(request, 2, time.Now()); err == nil {
		t.Fatal("zone with fewer than three distinct points was accepted")
	}
	request = validProviderRequest()
	request.DepartureUnix = time.Now().Add(time.Hour).Unix()
	request.Traffic = false
	if err := validateRouteRequest(request, 2, time.Now()); err == nil {
		t.Fatal("departure time with traffic disabled was accepted")
	}
}

func TestMetricsDoNotContainCoordinates(t *testing.T) {
	metrics := &serviceMetrics{}
	metrics.providerRequests.Add(1)
	metrics.addBillableUnits(2, 0.5)
	var output bytes.Buffer
	metrics.writePrometheus(&output)
	if strings.Contains(output.String(), "latitude") || strings.Contains(output.String(), "longitude") || strings.Contains(output.String(), "55.") {
		t.Fatalf("metrics contain location-like labels: %s", output.String())
	}
	if !strings.Contains(output.String(), "estimated_provider_billable_units_total 2") || !strings.Contains(output.String(), "estimated_provider_cost_total 1.000000000") {
		t.Fatalf("cost metrics missing: %s", output.String())
	}
}

func testHandler(provider Provider) (http.Handler, *bytes.Buffer) {
	cfg := testConfig()
	metrics := &serviceMetrics{}
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	return newAPIServer(cfg, provider, metrics, logger), &logs
}

type fakeProvider struct {
	mu           sync.Mutex
	calls        int
	capabilities capabilityDocument
	response     contracts.ProviderRouteResponse
	routeErr     error
	readyErr     error
}

func (f *fakeProvider) Routes(context.Context, contracts.ProviderRouteRequest) (contracts.ProviderRouteResponse, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	return f.response, f.routeErr
}

func (f *fakeProvider) Suggest(context.Context, string, string, int) (contracts.GeosuggestResponse, error) {
	return contracts.GeosuggestResponse{}, errUnsupported
}

func (f *fakeProvider) Capabilities() capabilityDocument { return f.capabilities }
func (f *fakeProvider) Ready() error                     { return f.readyErr }
func (f *fakeProvider) routeCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}
