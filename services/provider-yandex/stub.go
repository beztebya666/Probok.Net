package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/greenroute/greenroute/internal/contracts"
	"github.com/greenroute/greenroute/internal/domain"
)

type stubAdapter struct {
	cfg     Config
	metrics *serviceMetrics
}

func newStubAdapter(cfg Config, metrics *serviceMetrics) *stubAdapter {
	return &stubAdapter{cfg: cfg, metrics: metrics}
}

func (a *stubAdapter) Routes(ctx context.Context, request contracts.ProviderRouteRequest) (contracts.ProviderRouteResponse, error) {
	started := time.Now()
	if err := ctx.Err(); err != nil {
		return contracts.ProviderRouteResponse{}, err
	}
	switch a.cfg.StubScenario {
	case "slow":
		if err := sleepContext(ctx, a.cfg.StubDelay); err != nil {
			return contracts.ProviderRouteResponse{}, err
		}
	case "rate_limit":
		a.metrics.providerRequests.Add(1)
		a.metrics.provider429.Add(1)
		err := serviceError("PROVIDER_RATE_LIMITED", "provider rate limit reached", 429, true, nil)
		err.RetryAfter = time.Second
		a.metrics.observeProvider(time.Since(started), err, 0)
		return contracts.ProviderRouteResponse{}, err
	case "outage":
		a.metrics.providerRequests.Add(1)
		err := serviceError("PROVIDER_UNAVAILABLE", "provider is temporarily unavailable", 503, true, nil)
		a.metrics.observeProvider(time.Since(started), err, 0)
		return contracts.ProviderRouteResponse{}, err
	}
	if request.RequestBudget < 1 {
		a.metrics.budgetExhausted.Add(1)
		return contracts.ProviderRouteResponse{}, errBudgetExhausted
	}

	count := request.Alternatives + 1
	if count < 1 {
		count = 1
	}
	if count > a.cfg.YandexMaxResults {
		count = a.cfg.YandexMaxResults
	}
	candidates := make([]domain.RouteCandidate, 0, count)
	for index := 0; index < count; index++ {
		candidates = append(candidates, syntheticCandidate(request, index))
	}
	a.metrics.providerRequests.Add(1)
	a.metrics.observeProvider(time.Since(started), nil, len(candidates))
	return contracts.ProviderRouteResponse{
		Candidates:             candidates,
		RequestsUsed:           1,
		EstimatedBillableUnits: 0,
		BudgetRemaining:        request.RequestBudget - 1,
		Warnings:               []string{"SYNTHETIC_PROVIDER_DATA: deterministic test data; never present it as official Yandex traffic."},
	}, nil
}

func syntheticCandidate(request contracts.ProviderRouteRequest, alternative int) domain.RouteCandidate {
	trafficDataType := domain.TrafficDataSynthetic
	if !request.Traffic {
		trafficDataType = domain.TrafficDataBaseline
	}
	anchors := make([]domain.GeoPoint, 0, len(request.Waypoints)+2)
	anchors = append(anchors, request.Origin)
	anchors = append(anchors, request.Waypoints...)
	anchors = append(anchors, request.Destination)

	geometry := make([]domain.GeoPoint, 0, len(anchors)*2-1)
	segments := make([]domain.RouteSegment, 0, len(anchors)-1)
	var distanceMeters int64
	var liveSeconds int64
	var baselineSeconds int64
	for i := 0; i < len(anchors)-1; i++ {
		start, finish := anchors[i], anchors[i+1]
		mid := syntheticMidpoint(start, finish, alternative, i)
		segmentGeometry := []domain.GeoPoint{start, mid, finish}
		geometry = appendDistinct(geometry, segmentGeometry)

		baseDistance := haversineMeters(start, mid) + haversineMeters(mid, finish)
		segmentDistance := int64(math.Round(baseDistance))
		baseline := int64(math.Max(1, math.Round(baseDistance/15.0)))
		trafficMultiplier := 1.35 - float64(alternative)*0.12 + float64(i%2)*0.08
		if trafficMultiplier < 1.05 {
			trafficMultiplier = 1.05
		}
		live := int64(math.Round(float64(baseline) * trafficMultiplier))
		congestion := domain.CongestionYellow
		if trafficMultiplier >= 1.4 {
			congestion = domain.CongestionOrange
		}
		if !request.Traffic {
			live = 0
			congestion = domain.CongestionUnknown
		}
		segments = append(segments, domain.RouteSegment{
			SegmentID: fmt.Sprintf("stub-%d-%d", alternative+1, i+1), Geometry: segmentGeometry,
			DistanceMeters: segmentDistance, LiveDurationSeconds: live, BaselineDurationSeconds: baseline,
			TrafficRatio: trafficMultiplier, CongestionClass: congestion,
			Confidence:         domain.Confidence{Level: domain.ConfidenceHigh, Score: 1, Reasons: []string{"DETERMINISTIC_SYNTHETIC_FIXTURE"}},
			GeometrySimilarity: 1, Source: "SYNTHETIC_STUB",
		})
		distanceMeters += segmentDistance
		liveSeconds += live
		baselineSeconds += baseline
	}
	if !request.Traffic {
		liveSeconds = 0
	}

	reasonCodes := []string{"SYNTHETIC_STUB", "NOT_PROVIDER_OBSERVED"}
	if !request.Traffic {
		reasonCodes = append(reasonCodes, "TRAFFIC_DISABLED_BASELINE")
	}
	return domain.RouteCandidate{
		CandidateID:             candidateID("stub", alternative, geometry, distanceMeters, maxInt64(liveSeconds, baselineSeconds)),
		Provider:                "stub",
		TrafficDataType:         trafficDataType,
		Geometry:                geometry,
		DistanceMeters:          distanceMeters,
		LiveDurationSeconds:     liveSeconds,
		BaselineDurationSeconds: baselineSeconds,
		TrafficDelaySeconds:     maxInt64(0, liveSeconds-baselineSeconds),
		Segments:                segments,
		Confidence:              domain.Confidence{Level: domain.ConfidenceHigh, Score: 1, Reasons: []string{"DETERMINISTIC_SYNTHETIC_FIXTURE"}},
		ReasonCodes:             reasonCodes,
		Explanation:             "Детерминированный синтетический маршрут provider stub; это не данные Яндекса.",
		GeneratedBy:             "SYNTHETIC_STUB",
		ProviderRequestCount:    1,
	}
}

func syntheticMidpoint(start, finish domain.GeoPoint, alternative, segment int) domain.GeoPoint {
	middle := domain.GeoPoint{
		Latitude:  (start.Latitude + finish.Latitude) / 2,
		Longitude: (start.Longitude + finish.Longitude) / 2,
	}
	if alternative == 0 {
		return middle
	}
	offset := 0.008 * float64(alternative)
	if segment%2 == 1 {
		offset = -offset
	}
	middle.Latitude = clamp(middle.Latitude+offset, -89.999999, 89.999999)
	middle.Longitude = wrapLongitude(middle.Longitude - offset)
	return middle
}

func haversineMeters(a, b domain.GeoPoint) float64 {
	const earthRadiusMeters = 6_371_000.0
	lat1 := a.Latitude * math.Pi / 180
	lat2 := b.Latitude * math.Pi / 180
	deltaLat := (b.Latitude - a.Latitude) * math.Pi / 180
	deltaLon := (b.Longitude - a.Longitude) * math.Pi / 180
	h := math.Sin(deltaLat/2)*math.Sin(deltaLat/2) + math.Cos(lat1)*math.Cos(lat2)*math.Sin(deltaLon/2)*math.Sin(deltaLon/2)
	return 2 * earthRadiusMeters * math.Asin(math.Sqrt(h))
}

func clamp(value, minimum, maximum float64) float64 {
	return math.Max(minimum, math.Min(maximum, value))
}

func wrapLongitude(value float64) float64 {
	for value > 180 {
		value -= 360
	}
	for value < -180 {
		value += 360
	}
	return value
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func (a *stubAdapter) Suggest(ctx context.Context, query, _ string, limit int) (contracts.GeosuggestResponse, error) {
	started := time.Now()
	if err := ctx.Err(); err != nil {
		return contracts.GeosuggestResponse{}, err
	}
	switch a.cfg.StubScenario {
	case "slow":
		if err := sleepContext(ctx, a.cfg.StubDelay); err != nil {
			return contracts.GeosuggestResponse{}, err
		}
	case "rate_limit":
		a.metrics.geocoderRequests.Add(1)
		a.metrics.provider429.Add(1)
		err := serviceError("PROVIDER_RATE_LIMITED", "provider rate limit reached", 429, true, nil)
		err.RetryAfter = time.Second
		a.metrics.observeGeocoder(time.Since(started), err)
		return contracts.GeosuggestResponse{}, err
	case "outage":
		a.metrics.geocoderRequests.Add(1)
		err := serviceError("PROVIDER_UNAVAILABLE", "provider is temporarily unavailable", 503, true, nil)
		a.metrics.observeGeocoder(time.Since(started), err)
		return contracts.GeosuggestResponse{}, err
	}
	normalized := strings.TrimSpace(query)
	digest := sha256.Sum256([]byte(strings.ToLower(normalized)))
	latOffset := float64(int32(binary.BigEndian.Uint32(digest[0:4]))%10_000) / 1_000_000
	lonOffset := float64(int32(binary.BigEndian.Uint32(digest[4:8]))%10_000) / 1_000_000
	if limit > 3 {
		limit = 3
	}
	items := make([]domain.GeoSuggestion, 0, limit)
	for i := 0; i < limit; i++ {
		items = append(items, domain.GeoSuggestion{
			ID:       fmt.Sprintf("stub-suggestion-%x-%d", digest[:6], i+1),
			Label:    fmt.Sprintf("%s — тестовый вариант %d", normalized, i+1),
			Subtitle: "Синтетические данные provider stub",
			Point:    domain.GeoPoint{Latitude: 55.751244 + latOffset + float64(i)*0.003, Longitude: 37.618423 + lonOffset - float64(i)*0.003},
		})
	}
	a.metrics.geocoderRequests.Add(1)
	a.metrics.observeGeocoder(time.Since(started), nil)
	return contracts.GeosuggestResponse{Suggestions: items}, nil
}

func (a *stubAdapter) Capabilities() capabilityDocument {
	document := officialCapabilities(a.cfg, providerModeStub)
	document.Provider = "stub"
	document.Geosuggest = true
	document.APIIntegrations = nil
	document.OfficialEndpoint = ""
	document.AddressSearchProvider = ""
	document.AddressSearchEndpoint = ""
	document.OfficialDocumentation = nil
	if a.cfg.StubScenario != "normal" {
		document.Mode = "stub:" + a.cfg.StubScenario
	}
	document.Limitations = append(document.Limitations, "Stub output is synthetic and must not be described as Yandex traffic data.")
	return document
}

func (a *stubAdapter) Ready() error {
	if a.cfg.StubScenario == "outage" {
		return errCircuitOpen
	}
	return nil
}
