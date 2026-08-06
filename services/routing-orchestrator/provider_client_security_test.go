package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/greenroute/greenroute/internal/contracts"
	"github.com/greenroute/greenroute/internal/domain"
	"github.com/greenroute/greenroute/internal/telemetry"
)

func TestProviderClientNeverForwardsTokenAcrossRedirect(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalls.Add(1)
		if token := r.Header.Get("Authorization"); token != "" {
			t.Errorf("redirect target received Authorization=%q", token)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token := r.Header.Get("Authorization"); token != "Bearer internal-secret" {
			t.Errorf("configured provider received Authorization=%q", token)
		}
		http.Redirect(w, r, target.URL+"/token-sink", http.StatusTemporaryRedirect)
	}))
	defer redirector.Close()

	client, err := newProviderClient(redirector.URL, "internal-secret", telemetry.NewMetrics("provider-redirect-test"))
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.doJSON(context.Background(), http.MethodGet, "/redirect", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("status=%d, want redirect response", response.StatusCode)
	}
	if calls := targetCalls.Load(); calls != 0 {
		t.Fatalf("redirect target was called %d times", calls)
	}
	if err := rejectInternalRedirect(nil, nil); err != http.ErrUseLastResponse {
		t.Fatalf("redirect policy error=%v, want http.ErrUseLastResponse", err)
	}
}

func TestProviderClientTransportIgnoresAmbientProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://attacker.invalid:8080")
	t.Setenv("HTTPS_PROXY", "http://attacker.invalid:8080")
	if proxy := newInternalProviderTransport().Proxy; proxy != nil {
		t.Fatal("internal provider transport configured an ambient proxy function")
	}
}

func TestProviderClientRejectsNonStrictOrOversizedRouteJSON(t *testing.T) {
	valid, err := json.Marshal(validProviderRouteResponse())
	if err != nil {
		t.Fatal(err)
	}
	unknown := append([]byte(nil), valid[:len(valid)-1]...)
	unknown = append(unknown, []byte(`,"unexpected":true}`)...)
	trailing := append(append([]byte(nil), valid...), []byte(` {}`)...)
	oversized := append([]byte(nil), valid...)
	oversized = append(oversized, bytes.Repeat([]byte(" "), int(maxProviderRouteResponseBytes)-len(valid)+1)...)

	tests := []struct {
		name string
		body []byte
	}{
		{name: "unknown field", body: unknown},
		{name: "trailing JSON", body: trailing},
		{name: "oversized body", body: oversized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := providerClientReturning(t, tt.body)
			_, err := client.routes(context.Background(), contracts.ProviderRouteRequest{RequestBudget: 2})
			if !errors.Is(err, errProviderContract) {
				t.Fatalf("routes() error=%v, want errProviderContract", err)
			}
		})
	}
}

func TestValidateProviderRouteResponse(t *testing.T) {
	request := contracts.ProviderRouteRequest{RequestBudget: 2}
	tests := []struct {
		name   string
		mutate func(*contracts.ProviderRouteResponse)
	}{
		{name: "zero requests", mutate: func(r *contracts.ProviderRouteResponse) { r.RequestsUsed = 0 }},
		{name: "requests over budget", mutate: func(r *contracts.ProviderRouteResponse) { r.RequestsUsed = 3 }},
		{name: "negative remaining budget", mutate: func(r *contracts.ProviderRouteResponse) { r.BudgetRemaining = -1 }},
		{name: "inconsistent remaining budget", mutate: func(r *contracts.ProviderRouteResponse) { r.BudgetRemaining = 2 }},
		{name: "negative billable units", mutate: func(r *contracts.ProviderRouteResponse) { r.EstimatedBillableUnits = -1 }},
		{name: "too many billable units", mutate: func(r *contracts.ProviderRouteResponse) { r.EstimatedBillableUnits = 2 }},
		{name: "no candidates", mutate: func(r *contracts.ProviderRouteResponse) { r.Candidates = nil }},
		{name: "too many candidates", mutate: func(r *contracts.ProviderRouteResponse) {
			for len(r.Candidates) <= maxProviderCandidates {
				r.Candidates = append(r.Candidates, validProviderCandidate("candidate-"+string(rune('1'+len(r.Candidates)))))
			}
		}},
		{name: "missing candidate id", mutate: func(r *contracts.ProviderRouteResponse) { r.Candidates[0].CandidateID = "" }},
		{name: "duplicate candidate id", mutate: func(r *contracts.ProviderRouteResponse) {
			r.Candidates = append(r.Candidates, validProviderCandidate(r.Candidates[0].CandidateID))
			r.EstimatedBillableUnits = len(r.Candidates)
		}},
		{name: "missing provider", mutate: func(r *contracts.ProviderRouteResponse) { r.Candidates[0].Provider = "" }},
		{name: "too many geometry points", mutate: func(r *contracts.ProviderRouteResponse) {
			r.Candidates[0].Geometry = make([]domain.GeoPoint, maxProviderGeometryPoints+1)
		}},
		{name: "invalid geometry point", mutate: func(r *contracts.ProviderRouteResponse) { r.Candidates[0].Geometry[0].Latitude = 91 }},
		{name: "negative distance", mutate: func(r *contracts.ProviderRouteResponse) { r.Candidates[0].DistanceMeters = -1 }},
		{name: "zero durations", mutate: func(r *contracts.ProviderRouteResponse) { r.Candidates[0].LiveDurationSeconds = 0 }},
		{name: "non-finite confidence", mutate: func(r *contracts.ProviderRouteResponse) { r.Candidates[0].Confidence.Score = math.NaN() }},
		{name: "non-finite score", mutate: func(r *contracts.ProviderRouteResponse) { r.Candidates[0].Score = math.Inf(1) }},
		{name: "request count mismatch", mutate: func(r *contracts.ProviderRouteResponse) { r.Candidates[0].ProviderRequestCount = 2 }},
		{name: "too many segments", mutate: func(r *contracts.ProviderRouteResponse) {
			r.Candidates[0].Segments = make([]domain.RouteSegment, maxProviderSegments+1)
		}},
		{name: "missing segment id", mutate: func(r *contracts.ProviderRouteResponse) {
			segment := validProviderSegment("segment-1")
			segment.SegmentID = ""
			r.Candidates[0].Segments = []domain.RouteSegment{segment}
		}},
		{name: "duplicate segment id", mutate: func(r *contracts.ProviderRouteResponse) {
			r.Candidates[0].Segments = []domain.RouteSegment{validProviderSegment("segment-1"), validProviderSegment("segment-1")}
		}},
		{name: "invalid segment metric", mutate: func(r *contracts.ProviderRouteResponse) {
			segment := validProviderSegment("segment-1")
			segment.TrafficRatio = math.NaN()
			r.Candidates[0].Segments = []domain.RouteSegment{segment}
		}},
		{name: "negative route metric", mutate: func(r *contracts.ProviderRouteResponse) { r.Candidates[0].Metrics.RedDistanceMeters = -1 }},
		{name: "non-finite route metric", mutate: func(r *contracts.ProviderRouteResponse) { r.Candidates[0].Metrics.GreenDistancePercent = math.Inf(1) }},
		{name: "too many warnings", mutate: func(r *contracts.ProviderRouteResponse) { r.Warnings = make([]string, maxProviderWarnings+1) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := validProviderRouteResponse()
			tt.mutate(&response)
			if err := validateProviderRouteResponse(response, request); err == nil {
				t.Fatal("invalid provider response was accepted")
			}
		})
	}
}

func TestValidateProviderRouteResponseAcceptsOneBillablePointSetWithAlternatives(t *testing.T) {
	response := validProviderRouteResponse()
	response.Candidates = append(response.Candidates,
		validProviderCandidate("candidate-2"),
		validProviderCandidate("candidate-3"),
	)
	response.EstimatedBillableUnits = 1

	if err := validateProviderRouteResponse(response, contracts.ProviderRouteRequest{RequestBudget: 2}); err != nil {
		t.Fatalf("one billable point set with alternatives was rejected: %v", err)
	}
}

func TestProviderClientAcceptsValidStrictResponse(t *testing.T) {
	body, err := json.Marshal(validProviderRouteResponse())
	if err != nil {
		t.Fatal(err)
	}
	client := providerClientReturning(t, body)
	response, err := client.routes(context.Background(), contracts.ProviderRouteRequest{RequestBudget: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Candidates) != 1 || response.RequestsUsed != 1 {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestValidateProviderCapabilitiesDocument(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*providerCapabilitiesDocument)
	}{
		{name: "missing contract version", mutate: func(d *providerCapabilitiesDocument) { d.ContractVersion = "" }},
		{name: "future contract version", mutate: func(d *providerCapabilitiesDocument) { d.ContractVersion = "v2" }},
		{name: "missing provider", mutate: func(d *providerCapabilitiesDocument) { d.Provider = "" }},
		{name: "missing mode", mutate: func(d *providerCapabilitiesDocument) { d.Mode = "" }},
		{name: "negative alternatives", mutate: func(d *providerCapabilitiesDocument) { d.MaxAlternatives = -1 }},
		{name: "too many alternatives", mutate: func(d *providerCapabilitiesDocument) { d.MaxAlternatives = 3 }},
		{name: "negative waypoints", mutate: func(d *providerCapabilitiesDocument) { d.MaxWaypoints = -1 }},
		{name: "too many waypoints", mutate: func(d *providerCapabilitiesDocument) { d.MaxWaypoints = 51 }},
		{name: "negative minute quota", mutate: func(d *providerCapabilitiesDocument) { d.RequestsPerMinute = -1 }},
		{name: "zero monthly quota", mutate: func(d *providerCapabilitiesDocument) { value := 0; d.MonthlyRequestLimit = &value }},
		{name: "unknown address provider", mutate: func(d *providerCapabilitiesDocument) { d.AddressSearchProvider = "untrusted" }},
		{name: "address provider without endpoint", mutate: func(d *providerCapabilitiesDocument) {
			d.AddressSearchProvider = "yandex"
			d.AddressSearchEndpoint = ""
		}},
		{name: "unknown integration capability", mutate: func(d *providerCapabilitiesDocument) {
			d.APIIntegrations[0].Capability = "traffic-scraping"
		}},
		{name: "invalid fallback state", mutate: func(d *providerCapabilitiesDocument) {
			d.APIIntegrations[0].Role = "fallback"
			d.APIIntegrations[0].State = "active"
		}},
		{name: "duplicate integration", mutate: func(d *providerCapabilitiesDocument) {
			d.APIIntegrations = append(d.APIIntegrations, d.APIIntegrations[0])
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			document := validProviderCapabilitiesDocument()
			tt.mutate(&document)
			if err := validateProviderCapabilitiesDocument(document); err == nil {
				t.Fatal("invalid capabilities document was accepted")
			}
		})
	}
}

func TestProviderClientAcceptsCurrentCapabilitiesContract(t *testing.T) {
	body, err := json.Marshal(validProviderCapabilitiesDocument())
	if err != nil {
		t.Fatal(err)
	}
	client := providerClientReturning(t, body)
	capabilities, err := client.capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if capabilities.Provider != "stub" || capabilities.Mode != "stub" || len(capabilities.APIIntegrations) != 1 || capabilities.APIIntegrations[0].APIVersion != "v1" {
		t.Fatalf("unexpected capabilities: %#v", capabilities)
	}
}

func providerClientReturning(t *testing.T, body []byte) *providerClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	client, err := newProviderClient(server.URL, "", telemetry.NewMetrics("provider-client-test"))
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func validProviderRouteResponse() contracts.ProviderRouteResponse {
	return contracts.ProviderRouteResponse{
		Candidates:   []domain.RouteCandidate{validProviderCandidate("candidate-1")},
		RequestsUsed: 1, EstimatedBillableUnits: 1, BudgetRemaining: 1,
	}
}

func validProviderCandidate(id string) domain.RouteCandidate {
	return domain.RouteCandidate{
		CandidateID: id, Provider: "stub", TrafficDataType: domain.TrafficDataSynthetic,
		Geometry:       []domain.GeoPoint{{Latitude: 55.70, Longitude: 37.50}, {Latitude: 55.80, Longitude: 37.60}},
		DistanceMeters: 10_000, LiveDurationSeconds: 900, Confidence: domain.Confidence{Level: domain.ConfidenceHigh, Score: 1},
		ProviderRequestCount: 1,
	}
}

func validProviderSegment(id string) domain.RouteSegment {
	return domain.RouteSegment{
		SegmentID: id, Geometry: []domain.GeoPoint{{Latitude: 55.70, Longitude: 37.50}, {Latitude: 55.80, Longitude: 37.60}},
		DistanceMeters: 10_000, LiveDurationSeconds: 900, Confidence: domain.Confidence{Level: domain.ConfidenceHigh, Score: 1},
		GeometrySimilarity: 1,
	}
}

func validProviderCapabilitiesDocument() providerCapabilitiesDocument {
	monthlyLimit := 1_000
	return providerCapabilitiesDocument{
		ProviderCapabilities: contracts.ProviderCapabilities{
			ContractVersion: contracts.InternalContractVersion, Provider: "stub", Mode: "stub", MaxAlternatives: 2, MaxWaypoints: 50,
			APIIntegrations: []contracts.APIIntegration{{
				ID: "test-address", Provider: "test", Product: "Test Geocoder", APIVersion: "v1",
				Capability: "address-search", Role: "primary", State: "active",
			}},
		},
		AddressSearchProvider: addressProviderYandexForTest,
		AddressSearchEndpoint: "https://geocode-maps.yandex.ru/v1/",
		RequestsPerMinute:     5, MonthlyRequestLimit: &monthlyLimit,
	}
}

const addressProviderYandexForTest = "yandex"
