package main

import (
	"fmt"
	"math"
	"sort"

	"github.com/greenroute/greenroute/internal/contracts"
	"github.com/greenroute/greenroute/internal/domain"
	"github.com/greenroute/greenroute/internal/geometry"
)

// This file implements the green-corridor search: the part of the product that
// is deliberately *not* delegated to the provider's own routing preference.
// The provider only answers "give me a route through / around these places".
// Deciding which places must be avoided, how far sideways it is worth reaching
// for a parallel street, and when to give up is done here, from the observed
// per-segment traffic classes of every candidate collected so far.

const (
	clusterMinRadiusMeters   = 180
	clusterMaxRadiusMeters   = 2_500
	clusterZoneBufferMeters  = 160
	clusterMergeMeters       = 420
	endpointProtectionMeters = 700
	maxZonesPerPlan          = 12
	// How far sideways a single anchor may be placed. A ring road sits ten to
	// twenty kilometres out from a city centre, and the old 12 km ceiling meant
	// the search could never reach one deliberately — however much detour the
	// user had allowed.
	maxLateralOffsetMeters = 25_000
	minLateralOffsetMeters = 400
	// Below this an anchor off the middle of the trip is not a corridor, only a
	// nudge the lateral plans already make.
	minCorridorOffsetMeters = 3_000
)

// congestionCluster is one contiguous non-green stretch observed on at least
// one candidate. Clusters are the atoms the search reasons about: every plan is
// some combination of "forbid these clusters" and "reach sideways around them".
type congestionCluster struct {
	center      domain.GeoPoint
	heading     float64
	radiusM     float64
	distanceM   int64
	lostSeconds int64
	severity    int
	weight      float64
}

// detourPlan is a single provider request shape. The label is stable so an
// adaptive loop never repeats the same experiment twice.
type detourPlan struct {
	label   string
	anchors []domain.GeoPoint
	zones   []contracts.AvoidZone
}

func congestionSeverity(class domain.CongestionClass) int {
	switch class {
	case domain.CongestionRed:
		return 3
	case domain.CongestionOrange:
		return 2
	case domain.CongestionYellow:
		return 1
	default:
		return 0
	}
}

// congestionClusters collects non-green stretches from every candidate in the
// pool, not only from the currently selected route. A stretch that ruined
// alternative #2 is exactly the stretch the next detour must not walk into.
func congestionClusters(candidates []domain.RouteCandidate, request domain.RouteSearchRequest, minimumSeverity int) []congestionCluster {
	clusters := make([]congestionCluster, 0, 16)
	for index := range candidates {
		clusters = append(clusters, candidateClusters(candidates[index], minimumSeverity)...)
	}
	clusters = mergeClusters(clusters)
	filtered := clusters[:0]
	for _, cluster := range clusters {
		// A hard exclusion that swallows the origin or the destination makes the
		// provider snap that endpoint outside the polygon and answer a different
		// question — it returns a real route between the wrong points. The zone
		// is shrunk to keep clear of both endpoints, and dropped when that leaves
		// nothing meaningful to forbid.
		clearance := math.Min(
			geometry.DistanceMeters(cluster.center, request.Origin),
			geometry.DistanceMeters(cluster.center, request.Destination),
		) - endpointProtectionMeters - clusterZoneBufferMeters
		if clearance < clusterMinRadiusMeters {
			continue
		}
		cluster.radiusM = math.Min(cluster.radiusM, clearance)
		filtered = append(filtered, cluster)
	}
	sort.SliceStable(filtered, func(i, j int) bool { return filtered[i].weight > filtered[j].weight })
	return filtered
}

func candidateClusters(candidate domain.RouteCandidate, minimumSeverity int) []congestionCluster {
	clusters := make([]congestionCluster, 0, 8)
	run := make([]domain.GeoPoint, 0, 64)
	var runDistance, runLost int64
	runSeverity := 0

	flush := func() {
		if len(run) >= 2 && runDistance > 0 {
			clusters = append(clusters, newCluster(run, runDistance, runLost, runSeverity))
		}
		run = run[:0]
		runDistance, runLost, runSeverity = 0, 0, 0
	}
	for _, segment := range candidate.Segments {
		severity := congestionSeverity(segment.CongestionClass)
		if severity < minimumSeverity || len(segment.Geometry) < 2 {
			flush()
			continue
		}
		run = append(run, segment.Geometry...)
		runDistance += max64(0, segment.DistanceMeters)
		if segment.BaselineDurationSeconds > 0 {
			runLost += max64(0, segment.LiveDurationSeconds-segment.BaselineDurationSeconds)
		}
		if severity > runSeverity {
			runSeverity = severity
		}
	}
	flush()
	return clusters
}

func newCluster(points []domain.GeoPoint, distanceMeters, lostSeconds int64, severity int) congestionCluster {
	geometryCopy := append([]domain.GeoPoint(nil), points...)
	center := geometryCopy[len(geometryCopy)/2]
	radius := 0.0
	for _, point := range geometryCopy {
		radius = math.Max(radius, geometry.DistanceMeters(center, point))
	}
	radius = math.Max(clusterMinRadiusMeters, math.Min(clusterMaxRadiusMeters, radius))
	first, last := geometryCopy[0], geometryCopy[len(geometryCopy)-1]
	return congestionCluster{
		center:      center,
		heading:     math.Atan2(last.Latitude-first.Latitude, (last.Longitude-first.Longitude)*math.Cos(radians(center.Latitude))),
		radiusM:     radius,
		distanceM:   distanceMeters,
		lostSeconds: lostSeconds,
		severity:    severity,
		// Severity dominates, length breaks ties, and measured lost time is used
		// whenever the provider supplied a baseline for the stretch.
		weight: float64(severity)*10_000 + float64(distanceMeters) + float64(lostSeconds)*20,
	}
}

func mergeClusters(clusters []congestionCluster) []congestionCluster {
	merged := make([]congestionCluster, 0, len(clusters))
	for _, cluster := range clusters {
		absorbed := false
		for index := range merged {
			if geometry.DistanceMeters(merged[index].center, cluster.center) > math.Max(clusterMergeMeters, merged[index].radiusM) {
				continue
			}
			merged[index].radiusM = math.Min(clusterMaxRadiusMeters, math.Max(merged[index].radiusM, cluster.radiusM))
			merged[index].distanceM = max64(merged[index].distanceM, cluster.distanceM)
			merged[index].lostSeconds = max64(merged[index].lostSeconds, cluster.lostSeconds)
			if cluster.severity > merged[index].severity {
				merged[index].severity = cluster.severity
				merged[index].heading = cluster.heading
			}
			merged[index].weight = math.Max(merged[index].weight, cluster.weight)
			absorbed = true
			break
		}
		if !absorbed {
			merged = append(merged, cluster)
		}
	}
	return merged
}

func metersToLatitude(meters float64) float64 { return meters / 111_320 }

func metersToLongitude(meters float64, latitude float64) float64 {
	scale := math.Cos(radians(latitude))
	if math.Abs(scale) < 0.01 {
		scale = 0.01
	}
	return meters / (111_320 * scale)
}

func radians(value float64) float64 { return value * math.Pi / 180 }

// zoneAround produces the closed polygon the provider must not route through.
// An octagon is used instead of a square so a hard exclusion around a junction
// does not needlessly forbid the diagonal side streets next to it.
func zoneAround(cluster congestionCluster) contracts.AvoidZone {
	radius := math.Min(clusterMaxRadiusMeters, cluster.radiusM+clusterZoneBufferMeters)
	latitudeRadius := metersToLatitude(radius)
	longitudeRadius := metersToLongitude(radius, cluster.center.Latitude)
	points := make([]domain.GeoPoint, 0, 9)
	for corner := 0; corner < 8; corner++ {
		angle := float64(corner) * math.Pi / 4
		point := domain.GeoPoint{
			Latitude:  cluster.center.Latitude + latitudeRadius*math.Sin(angle),
			Longitude: cluster.center.Longitude + longitudeRadius*math.Cos(angle),
		}
		if point.Validate() != nil {
			return contracts.AvoidZone{}
		}
		points = append(points, point)
	}
	points = append(points, points[0])
	return contracts.AvoidZone{Points: points}
}

func zonesFor(clusters []congestionCluster, limit int) []contracts.AvoidZone {
	zones := make([]contracts.AvoidZone, 0, limit)
	for _, cluster := range clusters {
		if len(zones) >= limit {
			break
		}
		zone := zoneAround(cluster)
		if len(zone.Points) == 0 {
			continue
		}
		zones = append(zones, zone)
	}
	return zones
}

// lateralAnchor reaches sideways from the jam, perpendicular to the direction
// of travel through it. A fixed north/south offset would push the route back
// onto the same avenue whenever the street grid runs east/west; the
// perpendicular offset is what actually lands on a parallel street or a yard.
func lateralAnchor(cluster congestionCluster, offsetMeters float64, side int) (domain.GeoPoint, bool) {
	perpendicular := cluster.heading + math.Pi/2*float64(side)
	latitudeOffset := metersToLatitude(offsetMeters * math.Sin(perpendicular))
	longitudeOffset := metersToLongitude(offsetMeters*math.Cos(perpendicular), cluster.center.Latitude)
	anchor := domain.GeoPoint{
		Latitude:  cluster.center.Latitude + latitudeOffset,
		Longitude: cluster.center.Longitude + longitudeOffset,
	}
	if anchor.Validate() != nil {
		return domain.GeoPoint{}, false
	}
	return anchor, true
}

// offsetPoint moves a point sideways from a heading, which is how every anchor
// in this file is placed.
func offsetPoint(from domain.GeoPoint, heading float64, offsetMeters float64, side int) (domain.GeoPoint, bool) {
	perpendicular := heading + math.Pi/2*float64(side)
	anchor := domain.GeoPoint{
		Latitude:  from.Latitude + metersToLatitude(offsetMeters*math.Sin(perpendicular)),
		Longitude: from.Longitude + metersToLongitude(offsetMeters*math.Cos(perpendicular), from.Latitude),
	}
	if anchor.Validate() != nil {
		return domain.GeoPoint{}, false
	}
	return anchor, true
}

// corridorPlans asks a question the cluster ladder cannot: "is there a greener
// way round the whole city?"
//
// Every other plan is a reaction to congestion that was already observed, so on
// a route that is mostly green it produces nothing and the search stops with the
// first corridor it was given — even when a different corridor would have been
// greener still. These anchors are placed off the middle of the trip itself, at
// ring-road distances, and describe no particular road: whatever orbital or
// bypass exists between A and B is what the provider will use to reach them.
func corridorPlans(request domain.RouteSearchRequest, anchorCapacity int) []detourPlan {
	if anchorCapacity < 1 {
		return nil
	}
	middle := domain.GeoPoint{
		Latitude:  (request.Origin.Latitude + request.Destination.Latitude) / 2,
		Longitude: (request.Origin.Longitude + request.Destination.Longitude) / 2,
	}
	heading := math.Atan2(
		request.Destination.Latitude-request.Origin.Latitude,
		(request.Destination.Longitude-request.Origin.Longitude)*math.Cos(radians(middle.Latitude)),
	)
	span := geometry.DistanceMeters(request.Origin, request.Destination)
	plans := make([]detourPlan, 0, 4)
	for _, offset := range corridorOffsets(request, span) {
		for _, side := range []int{1, -1} {
			anchor, ok := offsetPoint(middle, heading, offset, side)
			if !ok {
				continue
			}
			direction := "LEFT"
			if side < 0 {
				direction = "RIGHT"
			}
			plans = append(plans, detourPlan{
				label:   fmt.Sprintf("CORRIDOR_%s_%dM", direction, int(offset)),
				anchors: []domain.GeoPoint{anchor},
			})
		}
	}
	return plans
}

// corridorOffsets are wide by design: a quarter to half the trip's own length,
// bounded by what the user allowed and by the sideways ceiling. Below the floor
// it would not be a corridor at all, only a nudge the lateral plans already
// make — and it would spend a provider request to repeat them.
func corridorOffsets(request domain.RouteSearchRequest, spanMeters float64) []float64 {
	allowance := float64(request.MaxExtraDistanceMeters)
	if allowance <= 0 {
		return nil
	}
	offsets := make([]float64, 0, 2)
	for _, fraction := range []float64{0.25, 0.5} {
		offset := math.Min(maxLateralOffsetMeters, math.Min(spanMeters*fraction, allowance/2))
		if offset < minCorridorOffsetMeters {
			continue
		}
		offset = math.Round(offset)
		if len(offsets) > 0 && math.Abs(offsets[len(offsets)-1]-offset) < 500 {
			continue
		}
		offsets = append(offsets, offset)
	}
	return offsets
}

func lateralOffsets(request domain.RouteSearchRequest) []float64 {
	// The user's own detour allowance is the search radius. Someone who accepts
	// +150 km wants the search to look far off-corridor; someone who accepts
	// +5 km must not be sent through the next district.
	allowance := float64(request.MaxExtraDistanceMeters) / 3
	if allowance <= 0 {
		allowance = 3_000
	}
	offsets := make([]float64, 0, 4)
	for _, fraction := range []float64{0.12, 0.3, 0.62, 1.0} {
		offset := math.Round(math.Max(minLateralOffsetMeters, math.Min(maxLateralOffsetMeters, allowance*fraction)))
		if len(offsets) > 0 && math.Abs(offsets[len(offsets)-1]-offset) < 150 {
			continue
		}
		offsets = append(offsets, offset)
	}
	return offsets
}

// greenDetourPlans is the search ladder. Earlier plans are cheap and blunt
// ("forbid every red stretch at once"); later plans are narrower and reach
// further sideways. The order matters because the request budget usually
// allows only the first few.
func greenDetourPlans(
	clusters []congestionCluster,
	request domain.RouteSearchRequest,
	zonesAllowed bool,
	anchorsAllowed bool,
	anchorCapacity int,
) []detourPlan {
	if len(clusters) == 0 {
		return nil
	}
	red := filterClusters(clusters, 3)
	redOrange := filterClusters(clusters, 2)
	plans := make([]detourPlan, 0, 16)
	add := func(plan detourPlan) {
		if len(plan.zones) == 0 && len(plan.anchors) == 0 {
			return
		}
		plans = append(plans, plan)
	}

	if zonesAllowed {
		if zones := zonesFor(red, maxZonesPerPlan); len(zones) > 0 {
			add(detourPlan{label: fmt.Sprintf("AVOID_ALL_RED_%d", len(zones)), zones: zones})
		}
		if zones := zonesFor(redOrange, maxZonesPerPlan); len(zones) > len(red) {
			add(detourPlan{label: fmt.Sprintf("AVOID_RED_AND_ORANGE_%d", len(zones)), zones: zones})
		}
	}

	offsets := lateralOffsets(request)
	worst := clusters[0]
	if anchorsAllowed && anchorCapacity >= 1 {
		for offsetIndex, offset := range offsets {
			for _, side := range []int{1, -1} {
				anchor, ok := lateralAnchor(worst, offset, side)
				if !ok {
					continue
				}
				direction := "LEFT"
				if side < 0 {
					direction = "RIGHT"
				}
				plan := detourPlan{
					label:   fmt.Sprintf("LATERAL_%s_%dM", direction, int(offset)),
					anchors: []domain.GeoPoint{anchor},
				}
				// From the second ring outwards the sideways anchor alone is not
				// enough: without also forbidding the jam the provider happily
				// routes back onto it right after the anchor.
				if zonesAllowed && offsetIndex >= 1 {
					plan.label = "GUARDED_" + plan.label
					plan.zones = zonesFor(red, 2)
				}
				add(plan)
			}
		}
	}

	if zonesAllowed && anchorsAllowed && anchorCapacity >= 2 && len(clusters) > 1 && len(offsets) > 0 {
		anchors := make([]domain.GeoPoint, 0, 2)
		for _, cluster := range clusters[:minInt(2, len(clusters))] {
			if anchor, ok := lateralAnchor(cluster, offsets[0], 1); ok {
				anchors = append(anchors, anchor)
			}
		}
		add(detourPlan{label: "MULTI_CLUSTER_BYPASS", anchors: anchors, zones: zonesFor(clusters, maxZonesPerPlan)})
	}

	// When nothing is red or orange the remaining obstacle is the yellow tail of
	// an otherwise green route, and forbidding it is the only way to reach 100%.
	if zonesAllowed && (request.RoutingMode == domain.RoutingModeStrictGreen || len(redOrange) == 0) {
		if zones := zonesFor(clusters, maxZonesPerPlan); len(zones) > 0 {
			add(detourPlan{label: fmt.Sprintf("AVOID_EVERY_NON_GREEN_%d", len(zones)), zones: zones})
		}
	}

	// Tried after the local reactions and before the relaxation tail: a wide
	// corridor is a different answer, not a weaker version of the same one.
	if anchorsAllowed {
		for _, plan := range corridorPlans(request, anchorCapacity) {
			add(plan)
		}
	}

	// Relaxation tail: a full exclusion set is often unroutable (the jam sits on
	// the only bridge). Shrinking the set keeps the search productive instead of
	// spending the rest of the budget on requests that cannot succeed.
	if zonesAllowed {
		for _, size := range []int{3, 1} {
			if zones := zonesFor(clusters, size); len(zones) == size {
				add(detourPlan{label: fmt.Sprintf("AVOID_WORST_%d", size), zones: zones})
			}
		}
	}
	return plans
}

// connectsSameTrip rejects a provider answer that no longer starts and ends
// where the trip does. An exclusion polygon or an unreachable anchor can make
// the provider relocate an endpoint and return a perfectly valid route between
// the wrong points; such a route must never enter the candidate pool, because
// downstream it would look like a very short, very green option.
func connectsSameTrip(candidate, reference domain.RouteCandidate, toleranceMeters float64) bool {
	if len(candidate.Geometry) < 2 || len(reference.Geometry) < 2 {
		return false
	}
	start := geometry.DistanceMeters(candidate.Geometry[0], reference.Geometry[0])
	finish := geometry.DistanceMeters(candidate.Geometry[len(candidate.Geometry)-1], reference.Geometry[len(reference.Geometry)-1])
	return start <= toleranceMeters && finish <= toleranceMeters
}

func filterClusters(clusters []congestionCluster, minimumSeverity int) []congestionCluster {
	result := make([]congestionCluster, 0, len(clusters))
	for _, cluster := range clusters {
		if cluster.severity >= minimumSeverity {
			result = append(result, cluster)
		}
	}
	return result
}

func nextUntriedPlan(plans []detourPlan, tried map[string]bool) (detourPlan, bool) {
	for _, plan := range plans {
		if !tried[plan.label] {
			return plan, true
		}
	}
	return detourPlan{}, false
}

// greenShare reports how much of a candidate is confidently green, by time and
// by distance. It is the number the product is judged on, so it is computed
// from segment evidence and never from a provider preference flag.
func greenShare(candidate domain.RouteCandidate) (durationPercent float64, distancePercent float64) {
	var greenDuration, totalDuration, greenDistance, totalDistance int64
	for _, segment := range candidate.Segments {
		totalDuration += max64(0, segment.LiveDurationSeconds)
		totalDistance += max64(0, segment.DistanceMeters)
		if segment.CongestionClass == domain.CongestionGreen {
			greenDuration += max64(0, segment.LiveDurationSeconds)
			greenDistance += max64(0, segment.DistanceMeters)
		}
	}
	// Uncovered length is uncertainty, never implicitly green.
	totalDuration = max64(totalDuration, candidate.LiveDurationSeconds)
	totalDistance = max64(totalDistance, candidate.DistanceMeters)
	if totalDuration > 0 {
		durationPercent = float64(greenDuration) / float64(totalDuration) * 100
	}
	if totalDistance > 0 {
		distancePercent = float64(greenDistance) / float64(totalDistance) * 100
	}
	return durationPercent, distancePercent
}
