package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/greenroute/greenroute/internal/contracts"
	"github.com/greenroute/greenroute/internal/domain"
)

// The live 2GIS Routing API opens every route with a zero-measure `begin`
// maneuver whose only geometry part is a single repeated point. Treating that
// marker as a malformed response rejected every real route, so this fixture
// pins the shape that reaches production.
func TestDGISRouteWithZeroMeasureMarkersNormalizes(t *testing.T) {
	var wire dgisRouteResponse
	if err := json.Unmarshal(readDGISFixture(t, "dgis-routing-v7-markers.synthetic.json"), &wire); err != nil {
		t.Fatal(err)
	}
	result, err := normalizeDGISRoutes(validProviderRequest(), wire)
	if err != nil {
		t.Fatalf("a route with start/finish markers must normalize: %v", err)
	}
	if len(result.Candidates) != 1 {
		t.Fatalf("candidates=%d, want 1", len(result.Candidates))
	}
	candidate := result.Candidates[0]
	if len(candidate.Segments) != 2 {
		t.Fatalf("the zero-length marker part must not become a segment: %#v", candidate.Segments)
	}
	var coveredDistance, coveredDuration int64
	for _, segment := range candidate.Segments {
		if segment.CongestionClass == domain.CongestionUnknown {
			t.Fatalf("marker colour leaked into the traffic evidence: %+v", segment)
		}
		coveredDistance += segment.DistanceMeters
		coveredDuration += segment.LiveDurationSeconds
	}
	if coveredDistance != candidate.DistanceMeters || coveredDuration != candidate.LiveDurationSeconds {
		t.Fatalf("segment coverage distance=%d/%d duration=%d/%d", coveredDistance, candidate.DistanceMeters, coveredDuration, candidate.LiveDurationSeconds)
	}
	if candidate.Geometry[0].Longitude != 37.612568 {
		t.Fatalf("driving geometry must start at the first real point: %#v", candidate.Geometry[0])
	}
}

func TestDGISSyntheticRoutingContract(t *testing.T) {
	data := readDGISFixture(t, "dgis-routing-v7.synthetic.json")
	var wire dgisRouteResponse
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	result, err := normalizeDGISRoutes(validProviderRequest(), wire)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) != 2 {
		t.Fatalf("candidates=%d, want 2", len(result.Candidates))
	}
	first := result.Candidates[0]
	if first.Provider != "2gis" || first.ProviderRouteReference != "synthetic/2gis/route-1" {
		t.Fatalf("unexpected provider identity: %+v", first)
	}
	if first.DistanceMeters != 3200 || first.LiveDurationSeconds != 420 || first.BaselineDurationSeconds != 0 {
		t.Fatalf("unexpected route measurements: %+v", first)
	}
	if first.TrafficDataType != domain.TrafficDataRealtime || !first.Tolls {
		t.Fatalf("unexpected traffic/toll normalization: %+v", first)
	}
	if len(first.Geometry) != 5 || len(first.Segments) != 3 {
		t.Fatalf("geometry=%d segments=%d", len(first.Geometry), len(first.Segments))
	}
	// Driving geometry excludes begin/end pedestrian connectors. Each detailed
	// geometry part retains the traffic evidence returned for that exact part.
	if first.Geometry[0].Longitude != 37.6101 || first.Geometry[len(first.Geometry)-1].Longitude != 37.64 {
		t.Fatalf("pedestrian connector leaked into driving geometry: %#v", first.Geometry)
	}
	wantClasses := []domain.CongestionClass{domain.CongestionGreen, domain.CongestionRed, domain.CongestionYellow}
	var coveredDistance, coveredDuration int64
	for index, segment := range first.Segments {
		if segment.CongestionClass != wantClasses[index] || segment.Source != domain.SegmentSourceDGISTrafficColor {
			t.Fatalf("segment[%d] traffic evidence=%+v", index, segment)
		}
		coveredDistance += segment.DistanceMeters
		coveredDuration += segment.LiveDurationSeconds
	}
	if coveredDistance != first.DistanceMeters || coveredDuration != first.LiveDurationSeconds {
		t.Fatalf("segment coverage distance=%d/%d duration=%d/%d", coveredDistance, first.DistanceMeters, coveredDuration, first.LiveDurationSeconds)
	}
}

func TestDGISTrafficDisabledUsesShortestBaseline(t *testing.T) {
	cfg := dgisTestConfig()
	request := validProviderRequest()
	request.Traffic = false
	endpoint, body, err := buildDGISRouteRequest(cfg, request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(endpoint, "key=dgis-test-key") {
		t.Fatal("server-side key missing from official provider request")
	}
	var wire dgisRouteRequest
	if err := json.Unmarshal(body, &wire); err != nil {
		t.Fatal(err)
	}
	if wire.RouteMode != "shortest" || wire.TrafficMode != "" || wire.Alternative != 1 {
		t.Fatalf("unexpected baseline request: %+v", wire)
	}

	var responseWire dgisRouteResponse
	if err := json.Unmarshal(readDGISFixture(t, "dgis-routing-v7.synthetic.json"), &responseWire); err != nil {
		t.Fatal(err)
	}
	result, err := normalizeDGISRoutes(request, responseWire)
	if err != nil {
		t.Fatal(err)
	}
	if result.Candidates[0].TrafficDataType != domain.TrafficDataBaseline ||
		result.Candidates[0].LiveDurationSeconds != 0 || result.Candidates[0].BaselineDurationSeconds != 420 {
		t.Fatalf("unexpected baseline candidate: %+v", result.Candidates[0])
	}
}

func TestDGISRequestUsesFixedEndpointAndDocumentedBody(t *testing.T) {
	cfg := dgisTestConfig()
	request := validProviderRequest()
	request.DepartureUnix = time.Now().Add(time.Hour).Unix()
	request.AvoidTolls = true
	request.AvoidUnpaved = true
	request.AvoidZones = []contracts.AvoidZone{{Points: []domain.GeoPoint{
		{Latitude: 55.70, Longitude: 37.50},
		{Latitude: 55.71, Longitude: 37.51},
		{Latitude: 55.72, Longitude: 37.50},
	}}}
	endpoint, body, err := buildDGISRouteRequest(cfg, request)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "https" || parsed.Host != "routing.api.2gis.com" || parsed.Path != "/routing/7.0.0/global" {
		t.Fatalf("unsafe endpoint: %s", parsed.Redacted())
	}
	if len(parsed.Query()) != 1 || parsed.Query().Get("key") != cfg.DGISAPIKey {
		t.Fatalf("unexpected routing query keys: %v", parsed.Query())
	}
	var wire dgisRouteRequest
	if err := json.Unmarshal(body, &wire); err != nil {
		t.Fatal(err)
	}
	if wire.Transport != "driving" || wire.Output != "detailed" || wire.TrafficMode != "statistics" || wire.UTC != request.DepartureUnix {
		t.Fatalf("unexpected route request: %+v", wire)
	}
	if wire.AllowLockedRoads {
		t.Fatal("ordinary driving route must never enable locked roads")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatal(err)
	}
	if _, exists := fields["type"]; exists {
		t.Fatal("request used response field type instead of documented traffic_mode")
	}
	if !wire.NeedPaymentInfo || wire.Alternative != request.Alternatives {
		t.Fatalf("full routing options missing: %+v", wire)
	}
	if len(wire.Filters) != 2 || len(wire.Exclude) != 1 || len(wire.Exclude[0].Points) != 4 {
		t.Fatalf("avoidance request was not preserved: %+v", wire)
	}
	if first := wire.Points[0]; first.Lat != request.Origin.Latitude || first.Lon != request.Origin.Longitude {
		t.Fatalf("coordinate order changed: %+v", first)
	}
}

func TestDGISImmediateDepartureUsesLiveJamTraffic(t *testing.T) {
	request := validProviderRequest()
	request.DepartureUnix = time.Now().Unix()
	_, body, err := buildDGISRouteRequest(dgisTestConfig(), request)
	if err != nil {
		t.Fatal(err)
	}
	var wire dgisRouteRequest
	if err := json.Unmarshal(body, &wire); err != nil {
		t.Fatal(err)
	}
	if wire.TrafficMode != "jam" || wire.UTC != 0 || dgisTrafficDataType(request) != domain.TrafficDataRealtime {
		t.Fatalf("immediate trip was not live: %+v type=%s", wire, dgisTrafficDataType(request))
	}
}

func TestDGISTrafficColorMappingIsFailClosed(t *testing.T) {
	tests := map[string]domain.CongestionClass{
		"fast": domain.CongestionGreen, "free": domain.CongestionGreen,
		"normal": domain.CongestionYellow, "fluid": domain.CongestionOrange,
		"slow": domain.CongestionRed, "slow-jams": domain.CongestionRed,
		"ignore": domain.CongestionUnknown, "no-traffic": domain.CongestionUnknown,
		"future-provider-value": domain.CongestionUnknown, "": domain.CongestionUnknown,
	}
	for color, want := range tests {
		got, confidence, source := normalizeDGISTrafficColor(true, color)
		if got != want || source != domain.SegmentSourceDGISTrafficColor {
			t.Fatalf("color=%q got=%s source=%s, want=%s", color, got, source, want)
		}
		if want == domain.CongestionUnknown && confidence.Level != domain.ConfidenceLow {
			t.Fatalf("unknown color=%q was not fail-closed: %#v", color, confidence)
		}
	}
}

func TestDGISMeasurementAllocationPreservesExactTotals(t *testing.T) {
	got, ok := allocateDGISMeasurements(420, []float64{700, 800, 1700})
	if !ok || len(got) != 3 || got[0]+got[1]+got[2] != 420 {
		t.Fatalf("allocation=%v ok=%v", got, ok)
	}
	if _, ok := allocateDGISMeasurements(2, []float64{1, 1, 1}); ok {
		t.Fatal("accepted a total too small to cover every geometry part")
	}
}

func TestDGISHTTPAccessStatusesRemainDistinguishable(t *testing.T) {
	tests := []struct {
		status int
		code   string
	}{
		{status: http.StatusUnauthorized, code: "PROVIDER_AUTHENTICATION_FAILED"},
		{status: http.StatusPaymentRequired, code: "PROVIDER_SUBSCRIPTION_REQUIRED"},
		{status: http.StatusForbidden, code: "PROVIDER_ACCESS_FORBIDDEN"},
	}
	for _, tt := range tests {
		err := normalizeProviderError(mapDGISStatus(tt.status, "", time.Now()))
		if err.Code != tt.code || err.Retryable || err.HTTPStatus != http.StatusServiceUnavailable {
			t.Fatalf("status=%d mapped to %#v", tt.status, err)
		}
		if !isDGISCredentialAccessError(err.Code) {
			t.Fatalf("status=%d did not latch credential readiness", tt.status)
		}
	}
}

func TestDGISAdapterRetriesAndBillsOnePointSet(t *testing.T) {
	fixture := readDGISFixture(t, "dgis-routing-v7.synthetic.json")
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return response(http.StatusInternalServerError, `{}`), nil
		}
		return response(http.StatusOK, string(fixture)), nil
	})
	cfg := dgisTestConfig()
	adapter := newDGISAdapter(cfg, &http.Client{Transport: transport}, &serviceMetrics{})
	adapter.sleep = func(context.Context, time.Duration) error { return nil }
	request := validProviderRequest()
	request.RequestBudget = 2
	result, err := adapter.Routes(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || result.RequestsUsed != 2 || result.EstimatedBillableUnits != 1 {
		t.Fatalf("calls=%d used=%d billable=%d", calls, result.RequestsUsed, result.EstimatedBillableUnits)
	}
}

func TestDGISRollingQuotaGateFailsFast(t *testing.T) {
	gate := newSlidingWindowGate(2, time.Minute)
	now := time.Unix(1_700_000_000, 0)
	if allowed, _ := gate.Try(now); !allowed {
		t.Fatal("first request rejected")
	}
	if allowed, _ := gate.Try(now.Add(time.Second)); !allowed {
		t.Fatal("second request rejected")
	}
	if allowed, retryAfter := gate.Try(now.Add(2 * time.Second)); allowed || retryAfter != 58*time.Second {
		t.Fatalf("allowed=%v retryAfter=%s", allowed, retryAfter)
	}
	if allowed, _ := gate.Try(now.Add(time.Minute)); !allowed {
		t.Fatal("expired rolling-window entry was not released")
	}
}

func TestDGISAdapterQuotaGateAvoidsSecondEgress(t *testing.T) {
	fixture := readDGISFixture(t, "dgis-routing-v7.synthetic.json")
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return response(http.StatusOK, string(fixture)), nil
	})
	cfg := dgisTestConfig()
	cfg.DGISRateLimitPerMinute = 1
	metrics := &serviceMetrics{}
	adapter := newDGISAdapter(cfg, &http.Client{Transport: transport}, metrics)
	now := time.Unix(1_700_000_000, 0)
	adapter.now = func() time.Time { return now }
	if _, err := adapter.Routes(context.Background(), validProviderRequest()); err != nil {
		t.Fatal(err)
	}
	_, err := adapter.Routes(context.Background(), validProviderRequest())
	providerErr := normalizeProviderError(err)
	if providerErr.Code != "PROVIDER_RATE_LIMITED" || providerErr.RetryAfter != time.Minute {
		t.Fatalf("unexpected local quota error: %#v", providerErr)
	}
	if calls != 1 || metrics.localRateGateRejected.Load() != 1 {
		t.Fatalf("calls=%d local_rejections=%d", calls, metrics.localRateGateRejected.Load())
	}
}

func TestDGISWKTParserIsStrict(t *testing.T) {
	geometry, err := parseDGISLineString("LINESTRING(37.61 55.75 100, 37.62 55.76 110)", 10)
	if err != nil || len(geometry) != 2 || geometry[0].Latitude != 55.75 || geometry[0].Longitude != 37.61 {
		t.Fatalf("geometry=%+v err=%v", geometry, err)
	}
	for _, value := range []string{
		"MULTILINESTRING((37.61 55.75, 37.62 55.76))",
		"LINESTRING(37.61 95, 37.62 55.76)",
		"LINESTRING(37.61 55.75)",
	} {
		if _, err := parseDGISLineString(value, 10); err == nil {
			t.Fatalf("malformed WKT accepted: %s", value)
		}
	}
}

func TestDGISGeocoderContract(t *testing.T) {
	var wire dgisGeocoderResponse
	if err := json.Unmarshal(readDGISFixture(t, "dgis-geocoder-v3.synthetic.json"), &wire); err != nil {
		t.Fatal(err)
	}
	result, err := normalizeDGISGeocoder(wire, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Suggestions) != 2 {
		t.Fatalf("suggestions=%d", len(result.Suggestions))
	}
	first := result.Suggestions[0]
	if first.Label != "Москва, Красная площадь" || first.Subtitle != "Красная площадь" || first.Point.Latitude != 55.75396 {
		t.Fatalf("unexpected suggestion: %+v", first)
	}
}

func TestDGISGeocoderMetaNotFoundIsEmptySuccess(t *testing.T) {
	var wire dgisGeocoderResponse
	wire.Meta.Code = http.StatusNotFound
	result, err := normalizeDGISGeocoder(wire, 5)
	if err != nil {
		t.Fatal(err)
	}
	if result.Suggestions == nil || len(result.Suggestions) != 0 {
		t.Fatalf("not-found result=%#v", result)
	}
}

func TestDGISGeocoderURLUsesMoscowRankingHintWithoutRewritingQuery(t *testing.T) {
	originalQuery := "Санкт-Петербург, Невский проспект, 1"
	endpoint, err := buildDGISGeocoderURL(dgisTestConfig(), originalQuery, "en_US", 7)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if parsed.Host != "catalog.api.2gis.com" || parsed.Path != "/3.0/items/geocode" || query.Get("locale") != "en_RU" || query.Get("page_size") != "7" {
		t.Fatalf("unexpected geocoder URL: %s", parsed.Redacted())
	}
	if query.Get("location") != defaultDGISGeocoderLocationBias || query.Get("point") != "" || query.Get("q") != originalQuery {
		t.Fatalf("unexpected geocoder ranking/query parameters: %v", query)
	}
}

func TestDGISCapabilitiesExposeSelectedAddressProvider(t *testing.T) {
	cfg := dgisTestConfig()
	cfg.AddressProviderMode = addressProviderAuto
	cfg.YandexGeocoderAPIKey = "dedicated-geocoder-test-key"
	adapter := newDGISAdapter(cfg, &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("capability lookup must not call an upstream")
		return nil, nil
	})}, &serviceMetrics{})
	document := adapter.Capabilities()
	if document.Provider != "2gis" || document.AddressSearchProvider != addressProviderYandex || document.AddressSearchEndpoint != yandexGeocoderEndpoint {
		t.Fatalf("unexpected composite capabilities: %#v", document)
	}
	if len(document.APIIntegrations) != 3 {
		t.Fatalf("api integrations=%#v, want routing, primary geocoder, and fallback geocoder", document.APIIntegrations)
	}
	primary := document.APIIntegrations[1]
	fallback := document.APIIntegrations[2]
	if primary.ID != "yandex-http-geocoder" || primary.APIVersion != "v1" || primary.Role != apiRolePrimary || primary.State != apiStateActive {
		t.Fatalf("unexpected primary address integration: %#v", primary)
	}
	if fallback.ID != "2gis-geocoder" || fallback.APIVersion != "3.0" || fallback.Role != apiRoleFallback || fallback.State != apiStateStandby {
		t.Fatalf("unexpected fallback address integration: %#v", fallback)
	}
}

func readDGISFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "tests", "contract", "provider-yandex", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func dgisTestConfig() Config {
	cfg := testConfig()
	cfg.ProviderMode = providerModeDGIS
	cfg.AddressProviderMode = addressProviderDGIS
	cfg.DGISAPIKey = "dgis-test-key"
	cfg.DGISRoutingBaseURL = "https://routing.api.2gis.com"
	cfg.DGISMaxResults = 3
	cfg.DGISRateLimitPerMinute = 100
	cfg.DGISMonthlyLimit = 1000
	cfg.DGISGeocoderRatePerMinute = 600
	cfg.DGISGeocoderLocationBias = defaultDGISGeocoderLocationBias
	return cfg
}
