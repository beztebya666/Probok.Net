package main

import (
	"math"
	"strings"
	"testing"

	"github.com/greenroute/greenroute/internal/domain"
	"github.com/greenroute/greenroute/internal/geometry"
)

// jamCandidate builds a candidate whose middle stretch carries the requested
// congestion class, on a west-to-east corridor so a perpendicular detour is a
// north/south offset and can be asserted unambiguously.
func jamCandidate(id string, jam domain.CongestionClass) domain.RouteCandidate {
	west := domain.GeoPoint{Latitude: 55.75, Longitude: 37.40}
	jamStart := domain.GeoPoint{Latitude: 55.75, Longitude: 37.55}
	jamEnd := domain.GeoPoint{Latitude: 55.75, Longitude: 37.60}
	east := domain.GeoPoint{Latitude: 55.75, Longitude: 37.75}
	return domain.RouteCandidate{
		CandidateID: id, Provider: "2gis", TrafficDataType: domain.TrafficDataRealtime,
		Geometry: []domain.GeoPoint{west, jamStart, jamEnd, east}, DistanceMeters: 22_000, LiveDurationSeconds: 2_000,
		Confidence: domain.Confidence{Level: domain.ConfidenceMedium, Score: .7},
		Segments: []domain.RouteSegment{
			{SegmentID: id + "-a", Geometry: []domain.GeoPoint{west, jamStart}, DistanceMeters: 9_000, LiveDurationSeconds: 600, CongestionClass: domain.CongestionGreen, Source: domain.SegmentSourceDGISTrafficColor},
			{SegmentID: id + "-b", Geometry: []domain.GeoPoint{jamStart, jamEnd}, DistanceMeters: 4_000, LiveDurationSeconds: 900, CongestionClass: jam, Source: domain.SegmentSourceDGISTrafficColor},
			{SegmentID: id + "-c", Geometry: []domain.GeoPoint{jamEnd, east}, DistanceMeters: 9_000, LiveDurationSeconds: 500, CongestionClass: domain.CongestionGreen, Source: domain.SegmentSourceDGISTrafficColor},
		},
	}
}

func jamRequest() domain.RouteSearchRequest {
	return domain.RouteSearchRequest{
		Origin:      domain.GeoPoint{Latitude: 55.75, Longitude: 37.40},
		Destination: domain.GeoPoint{Latitude: 55.75, Longitude: 37.75},
		RoutingMode: domain.RoutingModeGreenest, MaxExtraDistanceMeters: 30_000,
		MaxExtraDistancePercent: 300, MaxExtraTimeSeconds: 3_600,
	}
}

func TestCongestionClustersMergeAcrossCandidatesAndSpareEndpoints(t *testing.T) {
	shared := congestionClusters(
		[]domain.RouteCandidate{jamCandidate("first", domain.CongestionRed), jamCandidate("second", domain.CongestionOrange)},
		jamRequest(),
		2,
	)
	if len(shared) != 1 {
		t.Fatalf("the same jam seen on two candidates must merge into one cluster, got %d", len(shared))
	}
	if shared[0].severity != 3 {
		t.Fatalf("merged cluster severity=%d, want the worst observed class", shared[0].severity)
	}

	atOrigin := jamCandidate("origin-jam", domain.CongestionRed)
	atOrigin.Segments[0].CongestionClass = domain.CongestionRed
	atOrigin.Segments[0].Geometry = []domain.GeoPoint{{Latitude: 55.75, Longitude: 37.4001}, {Latitude: 55.75, Longitude: 37.4004}}
	atOrigin.Segments[1].CongestionClass = domain.CongestionGreen
	atOrigin.Segments[2].CongestionClass = domain.CongestionGreen
	if clusters := congestionClusters([]domain.RouteCandidate{atOrigin}, jamRequest(), 2); len(clusters) != 0 {
		t.Fatalf("a jam on the departure point cannot be routed around, got %#v", clusters)
	}
}

func TestGreenDetourPlansForbidTheJamBeforeReachingSideways(t *testing.T) {
	request := jamRequest()
	clusters := congestionClusters([]domain.RouteCandidate{jamCandidate("red", domain.CongestionRed)}, request, 2)
	plans := greenDetourPlans(clusters, request, true, true, 8)
	if len(plans) < 3 {
		t.Fatalf("expected a search ladder, got %d plans", len(plans))
	}
	if !strings.HasPrefix(plans[0].label, "AVOID_ALL_RED") || len(plans[0].zones) == 0 {
		t.Fatalf("the first probe must forbid the red stretch outright, got %q", plans[0].label)
	}

	labels := make(map[string]bool, len(plans))
	for _, plan := range plans {
		if labels[plan.label] {
			t.Fatalf("duplicate plan label %q would waste a provider request", plan.label)
		}
		labels[plan.label] = true
	}

	var lateral *detourPlan
	for index := range plans {
		if strings.Contains(plans[index].label, "LATERAL_LEFT") {
			lateral = &plans[index]
			break
		}
	}
	if lateral == nil || len(lateral.anchors) != 1 {
		t.Fatal("no sideways probe was generated for the jam")
	}
	// The corridor runs west to east, so a perpendicular anchor must move the
	// route north or south rather than further along the same avenue.
	anchor := lateral.anchors[0]
	if math.Abs(anchor.Latitude-clusters[0].center.Latitude) < math.Abs(anchor.Longitude-clusters[0].center.Longitude) {
		t.Fatalf("lateral anchor %v is not perpendicular to the congested corridor", anchor)
	}
	if offset := geometry.DistanceMeters(anchor, clusters[0].center); offset < minLateralOffsetMeters {
		t.Fatalf("lateral anchor offset=%.0fm is too small to reach a parallel street", offset)
	}
}

func TestGreenDetourPlansStayWithinTheUserDetourAllowance(t *testing.T) {
	request := jamRequest()
	request.MaxExtraDistanceMeters = 3_000
	clusters := congestionClusters([]domain.RouteCandidate{jamCandidate("red", domain.CongestionRed)}, request, 2)
	for _, plan := range greenDetourPlans(clusters, request, true, true, 8) {
		for _, anchor := range plan.anchors {
			if offset := geometry.DistanceMeters(anchor, clusters[0].center); offset > float64(request.MaxExtraDistanceMeters) {
				t.Fatalf("plan %q reaches %.0fm sideways on a %dm allowance", plan.label, offset, request.MaxExtraDistanceMeters)
			}
		}
	}
}

func TestGreenDetourPlansWithoutZonesStillProbeSideways(t *testing.T) {
	request := jamRequest()
	clusters := congestionClusters([]domain.RouteCandidate{jamCandidate("red", domain.CongestionRed)}, request, 2)
	plans := greenDetourPlans(clusters, request, false, true, 8)
	if len(plans) == 0 {
		t.Fatal("a provider without exclusion support must still get corridor probes")
	}
	for _, plan := range plans {
		if len(plan.zones) != 0 {
			t.Fatalf("plan %q sent exclusion zones to a provider that does not support them", plan.label)
		}
	}
}

func TestExclusionZonesKeepClearOfTheTripEndpoints(t *testing.T) {
	request := jamRequest()
	nearOrigin := jamCandidate("near-origin", domain.CongestionRed)
	// A jam that begins just after the departure point: the cluster is usable,
	// but a polygon around it must never swallow the origin itself.
	nearOrigin.Segments[1].Geometry = []domain.GeoPoint{
		{Latitude: 55.75, Longitude: 37.4150},
		{Latitude: 55.75, Longitude: 37.4400},
	}
	clusters := congestionClusters([]domain.RouteCandidate{nearOrigin}, request, 1)
	for _, cluster := range clusters {
		zone := zoneAround(cluster)
		for _, point := range zone.Points {
			if geometry.DistanceMeters(point, request.Origin) < 1 {
				t.Fatalf("zone %v touches the origin", zone.Points)
			}
		}
		if geometry.DistanceMeters(cluster.center, request.Origin) <= cluster.radiusM+clusterZoneBufferMeters {
			t.Fatalf("zone around %v still contains the origin", cluster.center)
		}
	}
}

func TestConnectsSameTripRejectsRelocatedEndpoints(t *testing.T) {
	reference := jamCandidate("reference", domain.CongestionRed)
	detour := jamCandidate("detour", domain.CongestionGreen)
	if !connectsSameTrip(detour, reference, detourEndpointToleranceMeters) {
		t.Fatal("a detour between the same endpoints must be accepted")
	}

	truncated := jamCandidate("truncated", domain.CongestionGreen)
	truncated.Geometry = truncated.Geometry[1:]
	if connectsSameTrip(truncated, reference, detourEndpointToleranceMeters) {
		t.Fatal("a route that no longer starts at the trip origin must be rejected")
	}

	snapped := jamCandidate("snapped", domain.CongestionGreen)
	snapped.Geometry[0] = domain.GeoPoint{Latitude: 55.7508, Longitude: 37.4005}
	if !connectsSameTrip(snapped, reference, detourEndpointToleranceMeters) {
		t.Fatal("ordinary provider snapping onto the road network must stay acceptable")
	}
}

func TestNextUntriedPlanNeverRepeatsAProbe(t *testing.T) {
	plans := []detourPlan{{label: "A", anchors: []domain.GeoPoint{{Latitude: 1, Longitude: 1}}}, {label: "B", anchors: []domain.GeoPoint{{Latitude: 2, Longitude: 2}}}}
	tried := map[string]bool{"A": true}
	plan, ok := nextUntriedPlan(plans, tried)
	if !ok || plan.label != "B" {
		t.Fatalf("nextUntriedPlan()=%q,%v want B", plan.label, ok)
	}
	tried["B"] = true
	if _, ok := nextUntriedPlan(plans, tried); ok {
		t.Fatal("an exhausted ladder must stop the search instead of looping")
	}
}

func TestTopGreenRoutesRanksByMeasuredGreenShare(t *testing.T) {
	best := providerColorCandidate("mostly-green", 9_500, domain.CongestionRed, 37.61)
	middle := providerColorCandidate("half-green", 5_000, domain.CongestionRed, 37.62)
	worst := providerColorCandidate("barely-green", 1_000, domain.CongestionRed, 37.63)
	unverifiable := providerColorCandidate("no-evidence", 9_900, domain.CongestionRed, 37.64)
	unverifiable.TrafficDataType = domain.TrafficDataUnknown

	ranked := topGreenRoutes([]domain.RouteCandidate{worst, unverifiable, best, middle}, greenTopRouteCount)
	want := []string{"mostly-green", "half-green", "barely-green"}
	if len(ranked) != len(want) {
		t.Fatalf("ranked %d routes, want %d: %#v", len(ranked), len(want), ranked)
	}
	for index, candidate := range ranked {
		if candidate.CandidateID != want[index] {
			t.Fatalf("greenTop[%d]=%s, want %s", index, candidate.CandidateID, want[index])
		}
		rank := "GREEN_RANK_" + string(rune('1'+index))
		if !containsString(candidate.ReasonCodes, rank) {
			t.Fatalf("route %s is missing %s: %v", candidate.CandidateID, rank, candidate.ReasonCodes)
		}
	}
}

func TestTopGreenRoutesNeverRepeatsTheSameProviderRoute(t *testing.T) {
	rediscovered := providerColorCandidate("same-route", 9_000, domain.CongestionRed, 37.61)
	other := providerColorCandidate("other-route", 4_000, domain.CongestionRed, 37.62)

	ranked := topGreenRoutes([]domain.RouteCandidate{rediscovered, other, rediscovered, rediscovered}, greenTopRouteCount)
	if len(ranked) != 2 {
		t.Fatalf("a route rediscovered by several probes must appear once: %#v", ranked)
	}
	if ranked[0].CandidateID != "same-route" || ranked[1].CandidateID != "other-route" {
		t.Fatalf("unexpected ranking: %s, %s", ranked[0].CandidateID, ranked[1].CandidateID)
	}
}

func TestGreenShareCountsUncoveredLengthAsNotGreen(t *testing.T) {
	candidate := providerColorCandidate("partial", 10_000, domain.CongestionGreen, 37.61)
	candidate.DistanceMeters = 20_000
	candidate.LiveDurationSeconds = 2_000
	durationPercent, distancePercent := greenShare(candidate)
	if durationPercent > 51 || distancePercent > 51 {
		t.Fatalf("missing evidence was counted as green: time=%.1f%% distance=%.1f%%", durationPercent, distancePercent)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestCorridorPlansProposeAWayRoundWhenNothingIsCongested(t *testing.T) {
	// The cluster ladder needs congestion to react to. On an already-green route
	// it produces nothing, and without a corridor hypothesis the search would
	// accept the first corridor it was handed and never look for a greener one.
	request := domain.RouteSearchRequest{
		Origin:                 domain.GeoPoint{Latitude: 55.692, Longitude: 37.663},
		Destination:            domain.GeoPoint{Latitude: 55.694, Longitude: 37.327},
		MaxExtraDistanceMeters: 150_000,
	}
	plans := greenDetourPlans(nil, request, true, true, 4)
	if len(plans) != 0 {
		t.Fatalf("no clusters must still mean no cluster plans, got %d", len(plans))
	}

	corridor := corridorPlans(request, 4)
	if len(corridor) < 2 {
		t.Fatalf("expected corridor plans on both sides, got %d", len(corridor))
	}
	span := geometry.DistanceMeters(request.Origin, request.Destination)
	for _, plan := range corridor {
		if len(plan.anchors) != 1 {
			t.Fatalf("a corridor plan is a single anchor, got %d", len(plan.anchors))
		}
		offset := geometry.DistanceMeters(plan.anchors[0], domain.GeoPoint{
			Latitude:  (request.Origin.Latitude + request.Destination.Latitude) / 2,
			Longitude: (request.Origin.Longitude + request.Destination.Longitude) / 2,
		})
		// Wide enough to reach an orbital road, and never wider than the ceiling.
		if offset < minCorridorOffsetMeters || offset > maxLateralOffsetMeters+1 {
			t.Fatalf("anchor %v sits %.0f m off the middle of a %.0f m trip", plan.anchors[0], offset, span)
		}
	}
}

func TestCorridorPlansRespectTheDetourAllowance(t *testing.T) {
	request := domain.RouteSearchRequest{
		Origin:                 domain.GeoPoint{Latitude: 55.692, Longitude: 37.663},
		Destination:            domain.GeoPoint{Latitude: 55.694, Longitude: 37.327},
		MaxExtraDistanceMeters: 2_000,
	}
	// Half of a two-kilometre allowance is far below the corridor floor, so a
	// user who accepts almost no detour is not sent around the city — and no
	// provider request is spent asking.
	if plans := corridorPlans(request, 4); len(plans) != 0 {
		t.Fatalf("a small allowance must not produce corridor plans, got %d", len(plans))
	}
}
