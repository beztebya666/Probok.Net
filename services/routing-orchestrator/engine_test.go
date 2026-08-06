package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/greenroute/greenroute/internal/contracts"
	"github.com/greenroute/greenroute/internal/domain"
	"github.com/greenroute/greenroute/internal/scoring"
	"github.com/greenroute/greenroute/internal/searchstore"
	"github.com/greenroute/greenroute/internal/telemetry"
)

func TestEngineSelectsLongerSmootherRouteWithinBudget(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/internal/v1/capabilities":
			_ = json.NewEncoder(w).Encode(providerCapabilitiesDocument{
				ProviderCapabilities: contracts.ProviderCapabilities{ContractVersion: contracts.InternalContractVersion, Provider: "stub", Mode: "stub", MaxAlternatives: 2, MaxWaypoints: 50, RealtimeTraffic: true, TrafficDisabledBaseline: true},
			})
		case "/health/ready":
			w.WriteHeader(http.StatusOK)
		case "/internal/v1/routes":
			var request contracts.ProviderRouteRequest
			_ = json.NewDecoder(r.Body).Decode(&request)
			if request.Traffic {
				_ = json.NewEncoder(w).Encode(contracts.ProviderRouteResponse{RequestsUsed: 1, Candidates: []domain.RouteCandidate{
					routeFixture("fast-heavy", 10_000, 1_000, 37.55),
					routeFixture("smooth-detour", 12_000, 1_100, 37.65),
				}})
				return
			}
			longitude := 37.55
			baseline := int64(500)
			if len(request.Waypoints) > 0 && request.Waypoints[0].Longitude > 37.60 {
				longitude, baseline = 37.65, 1_020
			}
			candidate := routeFixture("baseline", 10_000, 0, longitude)
			candidate.BaselineDurationSeconds = baseline
			_ = json.NewEncoder(w).Encode(contracts.ProviderRouteResponse{RequestsUsed: 1, Candidates: []domain.RouteCandidate{candidate}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	metrics := telemetry.NewMetrics("test")
	client, err := newProviderClient(provider.URL, "", metrics)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config{StateTTL: time.Minute, EnableEnhancedSearch: false, MaxActiveCandidates: 10, MaxEnhancedIterations: 0, SSEHeartbeat: time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := searchstore.NewMemory()
	engine := newEngine(ctx, cfg, store, client, metrics)
	if err := engine.discoverCapabilities(ctx); err != nil {
		t.Fatal(err)
	}
	request := domain.RouteSearchRequest{
		Origin: domain.GeoPoint{Latitude: 55.70, Longitude: 37.50}, Destination: domain.GeoPoint{Latitude: 55.80, Longitude: 37.60},
		RoutingMode: domain.RoutingModeStrictGreen, MaxExtraDistanceMeters: 5_000, MaxExtraDistancePercent: 50,
		MaxExtraTimeSeconds: 600, Strictness: .9, MaxProviderRequests: 5, SearchDeadlineMS: 5_000,
	}
	accepted, err := engine.start(request, "")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		result, err := store.Get(ctx, accepted.SearchID)
		if err != nil {
			t.Fatal(err)
		}
		if terminal(result.Status) {
			if result.SelectedRoute == nil || result.SelectedRoute.CandidateID != "smooth-detour" {
				t.Fatalf("expected smooth detour, got %#v; warnings=%v", result.SelectedRoute, result.Warnings)
			}
			if result.ProviderUsage.RequestsUsed > request.MaxProviderRequests {
				t.Fatalf("request budget exceeded: %d > %d", result.ProviderUsage.RequestsUsed, request.MaxProviderRequests)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("search did not complete")
}

func TestProviderErrorChargesFullRetryLease(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer provider.Close()
	metrics := telemetry.NewMetrics("budget-error-test")
	client, err := newProviderClient(provider.URL, "", metrics)
	if err != nil {
		t.Fatal(err)
	}
	engine := newEngine(context.Background(), config{}, searchstore.NewMemory(), client, metrics)
	budget := newRequestBudget(5)
	_, err = engine.routeWithBudget(context.Background(), budget, contracts.ProviderRouteRequest{RequestID: "request-123", RequestBudget: 5})
	if err == nil {
		t.Fatal("expected provider error")
	}
	used, remaining, _ := budget.snapshot()
	if used != 2 || remaining != 3 {
		t.Fatalf("error must charge full retry lease: used=%d remaining=%d", used, remaining)
	}
}

func TestEvaluateCandidateUsesCompleteProviderTrafficClassesWithoutBaselineCall(t *testing.T) {
	engine := newEngine(context.Background(), config{}, searchstore.NewMemory(), nil, telemetry.NewMetrics("provider-color-evaluation-test"))
	points := []domain.GeoPoint{{Latitude: 55.70, Longitude: 37.50}, {Latitude: 55.75, Longitude: 37.55}, {Latitude: 55.80, Longitude: 37.60}}
	candidate := domain.RouteCandidate{
		CandidateID: "provider-colors", Provider: "2gis", TrafficDataType: domain.TrafficDataRealtime,
		Geometry: points, DistanceMeters: 1_000, LiveDurationSeconds: 100,
		Segments: []domain.RouteSegment{
			{SegmentID: "green-1", Geometry: points[:2], DistanceMeters: 500, LiveDurationSeconds: 50, CongestionClass: domain.CongestionGreen, GeometrySimilarity: 1, Source: domain.SegmentSourceDGISTrafficColor},
			{SegmentID: "green-2", Geometry: points[1:], DistanceMeters: 500, LiveDurationSeconds: 50, CongestionClass: domain.CongestionGreen, GeometrySimilarity: 1, Source: domain.SegmentSourceDGISTrafficColor},
		},
	}
	budget := newRequestBudget(2)
	evaluated := engine.evaluateCandidate(context.Background(), domain.RouteSearchRequest{}, candidate, budget, true)
	used, _, _ := budget.snapshot()
	if used != 0 {
		t.Fatalf("provider-classified route spent %d baseline requests, want 0", used)
	}
	if evaluated.Metrics.GreenDistanceMeters != candidate.DistanceMeters || evaluated.Confidence.Level == domain.ConfidenceLow {
		t.Fatalf("provider traffic classes were not evaluated as usable evidence: %#v", evaluated)
	}
}

func TestCloneResultSerializesNoEligibleAlternativesAsEmptyList(t *testing.T) {
	result := &domain.RouteSearchResult{
		SearchID: "76d78dc8-b2f3-4964-812a-cf2532fefc00", RequestID: "35aa95f5-9f14-44b9-8589-b488cb108aa9",
		Status: domain.SearchDegraded, Alternatives: nil, Warnings: []string{"NO_ROUTE_WITHIN_HARD_CONSTRAINTS"},
		GeneratedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Minute),
	}

	payload, err := json.Marshal(cloneResult(result))
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Alternatives     json.RawMessage `json:"alternatives"`
		BestEffortRoutes json.RawMessage `json:"bestEffortRoutes"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatal(err)
	}
	if string(envelope.Alternatives) != "[]" {
		t.Fatalf("route-search contract requires an array for no eligible alternatives, got %s", envelope.Alternatives)
	}
	if string(envelope.BestEffortRoutes) != "[]" {
		t.Fatalf("route-search contract requires an array for no best-effort routes, got %s", envelope.BestEffortRoutes)
	}
}

func TestSanitizeProviderCandidatesRejectsMalformedAndCaps(t *testing.T) {
	valid := routeFixture("valid", 10_000, 1_000, 37.55)
	blocked := routeFixture("blocked", 10_000, 1_000, 37.56)
	blocked.Blocked = true
	emptyGeometry := routeFixture("empty", 10_000, 1_000, 37.57)
	emptyGeometry.Geometry = nil
	negativeSegment := routeFixture("bad-segment", 10_000, 1_000, 37.58)
	negativeSegment.Segments = []domain.RouteSegment{{DistanceMeters: -1}}
	secondValid := routeFixture("second", 10_000, 1_000, 37.59)

	candidates, rejected := sanitizeProviderCandidates(
		[]domain.RouteCandidate{blocked, emptyGeometry, negativeSegment, valid, secondValid},
		1,
		"ENHANCED_DETOUR",
	)
	if len(candidates) != 1 || candidates[0].CandidateID != "valid" {
		t.Fatalf("expected first valid candidate only, got %#v", candidates)
	}
	if candidates[0].GeneratedBy != "ENHANCED_DETOUR" || rejected != 4 {
		t.Fatalf("unexpected normalization: generatedBy=%q rejected=%d", candidates[0].GeneratedBy, rejected)
	}
}

func TestEnhancedSearchAdoptsLexicographicallyBetterStrictGreenRoute(t *testing.T) {
	better := routeFixture("verified-green-detour", 11_000, 1_000, 37.67)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/routes" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(contracts.ProviderRouteResponse{RequestsUsed: 1, Candidates: []domain.RouteCandidate{better}})
	}))
	defer provider.Close()

	metrics := telemetry.NewMetrics("enhanced-ranking-test")
	client, err := newProviderClient(provider.URL, "", metrics)
	if err != nil {
		t.Fatal(err)
	}
	store := searchstore.NewMemory()
	cfg := config{
		StateTTL: time.Minute, EnableEnhancedSearch: true, EnableReranking: true, EnableAvoidZones: true,
		MaxActiveCandidates: 10, MaxEnhancedIterations: 1, MinimumScoreImprovement: .02,
	}
	engine := newEngine(context.Background(), cfg, store, client, metrics)
	engine.capabilities = contracts.ProviderCapabilities{AvoidZones: true, MaxWaypoints: 50}
	previous := congestionFixture("previous", 10_000, 900, 1_000, 37.56)
	previous = scoring.Evaluate(previous, scoring.Evidence{TrafficDataAvailable: true, TrafficDataType: domain.TrafficDataRealtime, HasAlternatives: true}, scoring.DefaultConfig())
	result := domain.RouteSearchResult{SearchID: "search-ranking", Alternatives: []domain.RouteCandidate{}, FastestReferenceRoute: cloneCandidate(&previous)}
	if err := store.Create(context.Background(), result, time.Minute); err != nil {
		t.Fatal(err)
	}
	request := domain.RouteSearchRequest{
		RequestID: "request-ranking", Origin: previous.Geometry[0], Destination: previous.Geometry[len(previous.Geometry)-1],
		RoutingMode: domain.RoutingModeStrictGreen, MaxExtraDistanceMeters: 5_000, MaxExtraDistancePercent: 50,
		MaxExtraTimeSeconds: 600, MaxProviderRequests: 4, SearchDeadlineMS: 5_000,
	}
	updated := engine.enhance(context.Background(), request, result, []domain.RouteCandidate{previous}, newRequestBudget(4))
	if updated.SelectedRoute == nil || updated.SelectedRoute.CandidateID != better.CandidateID {
		t.Fatalf("verified green detour was not adopted from a nil strict selection: %#v", updated.SelectedRoute)
	}
	if len(updated.BestEffortRoutes) != 1 || updated.BestEffortRoutes[0].CandidateID != previous.CandidateID {
		t.Fatalf("rejected initial route was not preserved as best effort: %#v", updated.BestEffortRoutes)
	}
}

func TestEnhancedSearchPreservesInitialAndEnhancedBestEffortRoutes(t *testing.T) {
	near99 := providerColorCandidate("enhanced-99", 9_900, domain.CongestionRed, 37.67)
	near98 := providerColorCandidate("enhanced-98", 9_800, domain.CongestionYellow, 37.68)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/routes" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(contracts.ProviderRouteResponse{RequestsUsed: 1, Candidates: []domain.RouteCandidate{near98, near99}})
	}))
	defer provider.Close()

	metrics := telemetry.NewMetrics("enhanced-best-effort-test")
	client, err := newProviderClient(provider.URL, "", metrics)
	if err != nil {
		t.Fatal(err)
	}
	store := searchstore.NewMemory()
	engine := newEngine(context.Background(), config{
		StateTTL: time.Minute, EnableEnhancedSearch: true, EnableReranking: true, EnableAvoidZones: true,
		MaxActiveCandidates: 10, MaxEnhancedIterations: 1,
	}, store, client, metrics)
	engine.capabilities = contracts.ProviderCapabilities{AvoidZones: true, MaxWaypoints: 50}
	initial := congestionFixture("initial-90", 10_000, 1_000, 1_000, 37.56)
	initial = scoring.Evaluate(initial, scoring.Evidence{TrafficDataAvailable: true, TrafficDataType: domain.TrafficDataRealtime, DataAgeKnown: true, HasAlternatives: true}, scoring.DefaultConfig())
	result := domain.RouteSearchResult{
		SearchID: "search-best-effort", Alternatives: []domain.RouteCandidate{}, BestEffortRoutes: []domain.RouteCandidate{},
		FastestReferenceRoute: cloneCandidate(&initial),
	}
	if err := store.Create(context.Background(), result, time.Minute); err != nil {
		t.Fatal(err)
	}
	request := domain.RouteSearchRequest{
		RequestID: "request-best-effort", Origin: initial.Geometry[0], Destination: initial.Geometry[len(initial.Geometry)-1],
		RoutingMode: domain.RoutingModeStrictGreen, MaxExtraDistanceMeters: 5_000, MaxExtraDistancePercent: 50,
		MaxExtraTimeSeconds: 600, MaxProviderRequests: 4, SearchDeadlineMS: 5_000,
	}
	updated := engine.enhance(context.Background(), request, result, []domain.RouteCandidate{initial}, newRequestBudget(4))
	if updated.SelectedRoute != nil || len(updated.Alternatives) != 0 {
		t.Fatalf("near-green proof routes leaked into strict result: %#v", updated)
	}
	want := []string{near99.CandidateID, near98.CandidateID, initial.CandidateID}
	if len(updated.BestEffortRoutes) != len(want) {
		t.Fatalf("best effort routes=%d, want %d: %#v", len(updated.BestEffortRoutes), len(want), updated.BestEffortRoutes)
	}
	for index, candidate := range updated.BestEffortRoutes {
		if candidate.CandidateID != want[index] {
			t.Fatalf("bestEffort[%d]=%s, want %s", index, candidate.CandidateID, want[index])
		}
	}
}

// Several probes of the green ladder routinely rediscover the same provider
// route. The pool must stay deduplicated across iterations, otherwise the
// repeat reaches the public result and the edge contract rejects the whole
// response as invalid.
func TestEnhancedSearchKeepsTheCandidatePoolDeduplicatedAcrossProbes(t *testing.T) {
	repeated := providerColorCandidate("rediscovered", 9_500, domain.CongestionYellow, 37.70)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/routes" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(contracts.ProviderRouteResponse{RequestsUsed: 1, Candidates: []domain.RouteCandidate{repeated}})
	}))
	defer provider.Close()

	metrics := telemetry.NewMetrics("enhanced-dedupe-test")
	client, err := newProviderClient(provider.URL, "", metrics)
	if err != nil {
		t.Fatal(err)
	}
	store := searchstore.NewMemory()
	engine := newEngine(context.Background(), config{
		StateTTL: time.Minute, EnableEnhancedSearch: true, EnableReranking: true, EnableAvoidZones: true,
		EnableCorridorAnchors: true, MaxActiveCandidates: 20, MaxEnhancedIterations: 5,
	}, store, client, metrics)
	engine.capabilities = contracts.ProviderCapabilities{AvoidZones: true, MaxWaypoints: 10}
	initial := congestionFixture("initial", 10_000, 1_000, 1_000, 37.56)
	initial = scoring.Evaluate(initial, scoring.Evidence{TrafficDataAvailable: true, TrafficDataType: domain.TrafficDataRealtime, HasAlternatives: true}, scoring.DefaultConfig())
	result := domain.RouteSearchResult{
		SearchID: "search-dedupe", Alternatives: []domain.RouteCandidate{}, BestEffortRoutes: []domain.RouteCandidate{},
		GreenTopRoutes: []domain.RouteCandidate{}, FastestReferenceRoute: cloneCandidate(&initial),
	}
	if err := store.Create(context.Background(), result, time.Minute); err != nil {
		t.Fatal(err)
	}
	request := domain.RouteSearchRequest{
		RequestID: "request-dedupe", Origin: initial.Geometry[0], Destination: initial.Geometry[len(initial.Geometry)-1],
		RoutingMode: domain.RoutingModeGreenest, MaxExtraDistanceMeters: 30_000, MaxExtraDistancePercent: 300,
		MaxExtraTimeSeconds: 3_600, MaxProviderRequests: 8, SearchDeadlineMS: 5_000,
	}
	updated := engine.enhance(context.Background(), request, result, []domain.RouteCandidate{initial}, newRequestBudget(8))

	for name, routes := range map[string][]domain.RouteCandidate{
		"greenTopRoutes":   updated.GreenTopRoutes,
		"bestEffortRoutes": updated.BestEffortRoutes,
		"alternatives":     updated.Alternatives,
	} {
		seen := make(map[string]struct{}, len(routes))
		for _, candidate := range routes {
			if _, duplicate := seen[candidate.CandidateID]; duplicate {
				t.Fatalf("%s contains %s twice", name, candidate.CandidateID)
			}
			seen[candidate.CandidateID] = struct{}{}
		}
	}
}

func TestStrictGreenEngineKeepsUnverifiedRouteAsReferenceOnly(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/internal/v1/capabilities":
			_ = json.NewEncoder(w).Encode(providerCapabilitiesDocument{ProviderCapabilities: contracts.ProviderCapabilities{
				ContractVersion: contracts.InternalContractVersion, Provider: "stub", Mode: "stub",
				MaxAlternatives: 0, MaxWaypoints: 50, RealtimeTraffic: true, TrafficDisabledBaseline: true,
			}})
		case "/internal/v1/routes":
			candidate := routeFixture("unverified", 10_000, 900, 37.55)
			candidate.TrafficDataType = domain.TrafficDataUnknown
			_ = json.NewEncoder(w).Encode(contracts.ProviderRouteResponse{RequestsUsed: 1, Candidates: []domain.RouteCandidate{candidate}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()

	metrics := telemetry.NewMetrics("strict-green-reference-test")
	client, err := newProviderClient(provider.URL, "", metrics)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := searchstore.NewMemory()
	engine := newEngine(ctx, config{StateTTL: time.Minute, EnableEnhancedSearch: false, MaxActiveCandidates: 10}, store, client, metrics)
	if err := engine.discoverCapabilities(ctx); err != nil {
		t.Fatal(err)
	}
	request := domain.RouteSearchRequest{
		Origin: domain.GeoPoint{Latitude: 55.70, Longitude: 37.50}, Destination: domain.GeoPoint{Latitude: 55.80, Longitude: 37.60},
		RoutingMode: domain.RoutingModeStrictGreen, MaxExtraDistanceMeters: 5_000, MaxExtraDistancePercent: 50,
		MaxExtraTimeSeconds: 600, Strictness: 1, MaxProviderRequests: 4, SearchDeadlineMS: 5_000,
	}
	accepted, err := engine.start(request, "")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		result, err := store.Get(ctx, accepted.SearchID)
		if err != nil {
			t.Fatal(err)
		}
		if !terminal(result.Status) {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if result.Status != domain.SearchDegraded || result.SelectedRoute != nil || len(result.Alternatives) != 0 {
			t.Fatalf("unverified strict route leaked as a result: %#v", result)
		}
		if len(result.BestEffortRoutes) != 1 || result.BestEffortRoutes[0].CandidateID != "unverified" || !slices.Contains(result.BestEffortRoutes[0].ReasonCodes, "BEST_EFFORT_NOT_STRICT_GREEN") {
			t.Fatalf("unverified route was not retained as labelled best effort: %#v", result.BestEffortRoutes)
		}
		if result.FastestReferenceRoute == nil || result.FastestReferenceRoute.CandidateID != "unverified" {
			t.Fatalf("diagnostic fastest reference missing: %#v", result.FastestReferenceRoute)
		}
		if !slices.Contains(result.Warnings, "NO_VERIFIED_STRICT_GREEN_ROUTE") {
			t.Fatalf("strict-green warning missing: %v", result.Warnings)
		}
		return
	}
	t.Fatal("search did not complete")
}

func TestEvaluateAllIsolatesWorkerPanic(t *testing.T) {
	metrics := telemetry.NewMetrics("candidate-panic-test")
	store := panicAppendStore{Store: searchstore.NewMemory()}
	engine := newEngine(context.Background(), config{}, store, nil, metrics)
	candidate := congestionFixture("panic-event", 10_000, 900, 500, 37.60)

	result := engine.evaluateAll(
		context.Background(),
		"search-panic",
		domain.RouteSearchRequest{MaxProviderRequests: 2},
		[]domain.RouteCandidate{candidate},
		newRequestBudget(2),
		true,
	)
	if len(result) != 0 {
		t.Fatalf("panicking candidate worker must be excluded, got %#v", result)
	}
}

func TestRecoverStaleBatchFinalizesInterruptedSearch(t *testing.T) {
	store := searchstore.NewMemory()
	metrics := telemetry.NewMetrics("stale-recovery-test")
	engine := newEngine(context.Background(), config{StateTTL: time.Minute, StaleSearchAfter: 35 * time.Second}, store, nil, metrics)
	now := time.Now().UTC()
	initial := domain.RouteSearchResult{
		SearchID: "interrupted-search", Status: domain.SearchSearching,
		GeneratedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute), Warnings: []string{},
	}
	if err := store.Create(context.Background(), initial, time.Minute); err != nil {
		t.Fatal(err)
	}

	engine.recoverStaleBatch(context.Background(), now)
	result, err := store.Get(context.Background(), initial.SearchID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != domain.SearchFailed || !slices.Contains(result.Warnings, "WORKER_INTERRUPTED") {
		t.Fatalf("interrupted search was not truthfully finalized: %#v", result)
	}
	events, err := store.EventsAfter(context.Background(), initial.SearchID, 0)
	if err != nil || len(events) != 1 || events[0].Type != domain.EventSearchFailed {
		t.Fatalf("terminal recovery event missing: %#v err=%v", events, err)
	}
}

type panicAppendStore struct {
	searchstore.Store
}

func (panicAppendStore) AppendEvent(context.Context, domain.SearchEvent, time.Duration) (domain.SearchEvent, error) {
	panic("simulated event-store panic")
}

func congestionFixture(id string, distance, live, redDistance int64, middleLongitude float64) domain.RouteCandidate {
	geometry := []domain.GeoPoint{{Latitude: 55.70, Longitude: 37.50}, {Latitude: 55.75, Longitude: middleLongitude}, {Latitude: 55.80, Longitude: 37.60}}
	redDuration := int64(100)
	greenDuration := live - redDuration
	redGeometry := []domain.GeoPoint{geometry[0], geometry[1]}
	greenGeometry := []domain.GeoPoint{geometry[1], geometry[2]}
	return domain.RouteCandidate{
		CandidateID: id, Provider: "stub", Geometry: geometry, DistanceMeters: distance, LiveDurationSeconds: live,
		TrafficDataType:         domain.TrafficDataRealtime,
		BaselineDurationSeconds: live - 50, Confidence: domain.Confidence{Level: domain.ConfidenceHigh, Score: 1},
		Segments: []domain.RouteSegment{
			{SegmentID: id + "-red", Geometry: redGeometry, DistanceMeters: redDistance, LiveDurationSeconds: redDuration, BaselineDurationSeconds: 50, GeometrySimilarity: 1},
			{SegmentID: id + "-green", Geometry: greenGeometry, DistanceMeters: distance - redDistance, LiveDurationSeconds: greenDuration, BaselineDurationSeconds: greenDuration, GeometrySimilarity: 1},
		},
	}
}

func routeFixture(id string, distance, live int64, middleLongitude float64) domain.RouteCandidate {
	geometry := []domain.GeoPoint{{Latitude: 55.70, Longitude: 37.50}, {Latitude: 55.75, Longitude: middleLongitude}, {Latitude: 55.80, Longitude: 37.60}}
	return domain.RouteCandidate{CandidateID: id, Provider: "stub", TrafficDataType: domain.TrafficDataRealtime, Geometry: geometry, DistanceMeters: distance, LiveDurationSeconds: live, Confidence: domain.Confidence{Level: domain.ConfidenceHigh, Score: 1}}
}

func providerColorCandidate(id string, greenDistance int64, remainderClass domain.CongestionClass, middleLongitude float64) domain.RouteCandidate {
	const distance = int64(10_000)
	const duration = int64(1_000)
	greenDuration := greenDistance / 10
	geometry := []domain.GeoPoint{{Latitude: 55.70, Longitude: 37.50}, {Latitude: 55.75, Longitude: middleLongitude}, {Latitude: 55.80, Longitude: 37.60}}
	return domain.RouteCandidate{
		CandidateID: id, Provider: "2gis", TrafficDataType: domain.TrafficDataRealtime,
		Geometry: geometry, DistanceMeters: distance, LiveDurationSeconds: duration,
		Segments: []domain.RouteSegment{
			{SegmentID: id + "-green", Geometry: geometry[:2], DistanceMeters: greenDistance, LiveDurationSeconds: greenDuration, CongestionClass: domain.CongestionGreen, Source: domain.SegmentSourceDGISTrafficColor},
			{SegmentID: id + "-remainder", Geometry: geometry[1:], DistanceMeters: distance - greenDistance, LiveDurationSeconds: duration - greenDuration, CongestionClass: remainderClass, Source: domain.SegmentSourceDGISTrafficColor},
		},
		Confidence: domain.Confidence{Level: domain.ConfidenceMedium, Score: .65},
	}
}
