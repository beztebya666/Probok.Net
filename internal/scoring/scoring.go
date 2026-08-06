package scoring

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/greenroute/greenroute/internal/domain"
)

var ErrInvalidScoringConfig = errors.New("invalid scoring configuration")

type Evidence struct {
	TrafficDataAvailable bool
	TrafficDataType      domain.TrafficDataType
	DataAgeKnown         bool
	DataAgeSeconds       int64
	ProviderErrors       int
	HasAlternatives      bool
}

func ClassifyTraffic(ratio, geometrySimilarity float64, config Config) domain.CongestionClass {
	if math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio <= 0 || geometrySimilarity < config.MinimumGeometrySimilarity {
		return domain.CongestionUnknown
	}
	switch {
	case ratio < config.GreenMaxTrafficRatio:
		return domain.CongestionGreen
	case ratio < config.YellowMaxTrafficRatio:
		return domain.CongestionYellow
	case ratio < config.OrangeMaxTrafficRatio:
		return domain.CongestionOrange
	default:
		return domain.CongestionRed
	}
}

func Evaluate(candidate domain.RouteCandidate, evidence Evidence, config Config) domain.RouteCandidate {
	if len(candidate.Segments) == 0 {
		candidate.Segments = []domain.RouteSegment{{
			SegmentID: candidate.CandidateID + "-0", Geometry: candidate.Geometry,
			DistanceMeters: candidate.DistanceMeters, LiveDurationSeconds: candidate.LiveDurationSeconds,
			BaselineDurationSeconds: candidate.BaselineDurationSeconds, GeometrySimilarity: 1,
			Source: "route-level-baseline",
		}}
	}
	var metrics domain.RouteMetrics
	var totalDuration, totalDistance int64
	continuousCongestion := int64(0)
	weightedSimilarity := 0.0
	for i := range candidate.Segments {
		segment := &candidate.Segments[i]
		providerClassified := segment.Source == domain.SegmentSourceDGISTrafficColor
		if providerClassified {
			// 2GIS traffic colors are direct per-geometry evidence. There is no
			// traffic-disabled duration for each colored part, so ratio-based
			// classification would erase useful evidence and turn every part into
			// UNKNOWN. Preserve only recognized classes and fail closed otherwise.
			segment.TrafficRatio = 0
			// The traffic class belongs to this exact geometry part; unlike a
			// separately requested baseline, no geometry matching is involved.
			segment.GeometrySimilarity = 1
			if !evidence.TrafficDataAvailable || !validCongestionClass(segment.CongestionClass) {
				segment.CongestionClass = domain.CongestionUnknown
			}
			segment.Confidence = providerClassConfidence(*segment, evidence)
		} else {
			segment.TrafficRatio = 0
			if segment.BaselineDurationSeconds > 0 && segment.LiveDurationSeconds > 0 {
				segment.TrafficRatio = float64(segment.LiveDurationSeconds) / float64(segment.BaselineDurationSeconds)
			}
			classificationSimilarity := segment.GeometrySimilarity
			if !evidence.TrafficDataAvailable || evidence.TrafficDataType == domain.TrafficDataUnknown {
				classificationSimilarity = 0
			}
			segment.CongestionClass = ClassifyTraffic(segment.TrafficRatio, classificationSimilarity, config)
			segment.Confidence = segmentConfidence(*segment, evidence)
		}
		totalDuration += segment.LiveDurationSeconds
		totalDistance += segment.DistanceMeters
		weightedSimilarity += segment.GeometrySimilarity * float64(max64(segment.DistanceMeters, 1))
		delay := int64(0)
		if segment.BaselineDurationSeconds > 0 {
			delay = max64(0, segment.LiveDurationSeconds-segment.BaselineDurationSeconds)
		}
		metrics.TotalTrafficDelaySeconds += delay
		switch segment.CongestionClass {
		case domain.CongestionGreen:
			metrics.GreenDistanceMeters += segment.DistanceMeters
			metrics.GreenDurationSeconds += segment.LiveDurationSeconds
			continuousCongestion = 0
		case domain.CongestionYellow:
			metrics.YellowDistanceMeters += segment.DistanceMeters
			metrics.YellowDurationSeconds += segment.LiveDurationSeconds
			continuousCongestion = 0
		case domain.CongestionOrange:
			metrics.OrangeDistanceMeters += segment.DistanceMeters
			metrics.OrangeDurationSeconds += segment.LiveDurationSeconds
			continuousCongestion += segment.DistanceMeters
			metrics.WorstContinuousCongestionMeters = max64(metrics.WorstContinuousCongestionMeters, continuousCongestion)
		case domain.CongestionRed:
			metrics.RedDistanceMeters += segment.DistanceMeters
			metrics.RedDurationSeconds += segment.LiveDurationSeconds
			continuousCongestion += segment.DistanceMeters
			metrics.WorstContinuousCongestionMeters = max64(metrics.WorstContinuousCongestionMeters, continuousCongestion)
		case domain.CongestionUnknown:
			metrics.UnknownDistanceMeters += segment.DistanceMeters
			metrics.UnknownDurationSeconds += segment.LiveDurationSeconds
			continuousCongestion = 0
		}
	}
	// Missing segment coverage is uncertainty, never implicitly green. This is
	// especially important for best-effort proof routes where incomplete
	// provider evidence must produce an honest percentage below 100%.
	if candidate.DistanceMeters > totalDistance {
		metrics.UnknownDistanceMeters += candidate.DistanceMeters - totalDistance
		totalDistance = candidate.DistanceMeters
	}
	if candidate.LiveDurationSeconds > totalDuration {
		metrics.UnknownDurationSeconds += candidate.LiveDurationSeconds - totalDuration
		totalDuration = candidate.LiveDurationSeconds
	}
	if totalDistance > 0 {
		metrics.CongestedDistancePercent = percent(metrics.OrangeDistanceMeters+metrics.RedDistanceMeters, totalDistance)
		metrics.GreenDistancePercent = percent(metrics.GreenDistanceMeters, totalDistance)
	}
	if totalDuration > 0 {
		metrics.CongestedDurationPercent = percent(metrics.OrangeDurationSeconds+metrics.RedDurationSeconds, totalDuration)
	}
	candidate.Metrics = metrics
	candidate.TrafficDelaySeconds = metrics.TotalTrafficDelaySeconds
	if candidate.BaselineDurationSeconds <= 0 {
		candidate.BaselineDurationSeconds = max64(0, candidate.LiveDurationSeconds-metrics.TotalTrafficDelaySeconds)
	}
	candidate.Confidence = routeConfidence(candidate, evidence, weightedSimilarity, config)
	candidate.ReasonCodes = unique(candidate.ReasonCodes)
	if metrics.UnknownDistanceMeters > 0 {
		candidate.ReasonCodes = appendUnique(candidate.ReasonCodes, "PARTIAL_TRAFFIC_DATA")
	}
	return candidate
}

func providerClassConfidence(segment domain.RouteSegment, evidence Evidence) domain.Confidence {
	if segment.CongestionClass == domain.CongestionUnknown || !evidence.TrafficDataAvailable {
		return confidence(0.25, []string{"PROVIDER_TRAFFIC_CLASS_UNKNOWN"})
	}
	// The provider color is useful current/forecast traffic evidence, but the
	// color-to-class mapping is inferred rather than a documented numeric speed
	// ratio. Keep it MEDIUM so strict mode can use it while exposing uncertainty.
	score := 0.70
	reasons := []string{"PROVIDER_TRAFFIC_COLOR_INFERRED"}
	if evidence.TrafficDataType == domain.TrafficDataForecast {
		score -= 0.08
		reasons = append(reasons, "FORECAST_TRAFFIC_DATA")
	}
	if evidence.DataAgeSeconds > 900 {
		score -= 0.15
		reasons = append(reasons, "STALE_TRAFFIC_DATA")
	} else if !evidence.DataAgeKnown {
		score -= 0.05
		reasons = append(reasons, "TRAFFIC_FRESHNESS_UNKNOWN")
	}
	return confidence(clamp01(score), reasons)
}

func validCongestionClass(value domain.CongestionClass) bool {
	switch value {
	case domain.CongestionGreen, domain.CongestionYellow, domain.CongestionOrange, domain.CongestionRed, domain.CongestionUnknown:
		return true
	default:
		return false
	}
}

func segmentConfidence(segment domain.RouteSegment, evidence Evidence) domain.Confidence {
	score := 1.0
	reasons := make([]string, 0, 3)
	if !evidence.TrafficDataAvailable || segment.BaselineDurationSeconds <= 0 {
		score -= 0.55
		reasons = append(reasons, "MISSING_BASELINE")
	}
	if segment.GeometrySimilarity < 0.9 {
		score -= (0.9 - clamp01(segment.GeometrySimilarity)) * 0.65
		reasons = append(reasons, "GEOMETRY_MISMATCH")
	}
	if evidence.DataAgeSeconds > 900 {
		score -= 0.15
		reasons = append(reasons, "STALE_TRAFFIC_DATA")
	} else if evidence.TrafficDataAvailable && !evidence.DataAgeKnown {
		score -= 0.05
		reasons = append(reasons, "TRAFFIC_FRESHNESS_UNKNOWN")
	}
	if evidence.TrafficDataType == domain.TrafficDataForecast {
		score -= 0.08
		reasons = append(reasons, "FORECAST_TRAFFIC_DATA")
	}
	return confidence(clamp01(score), reasons)
}

func routeConfidence(candidate domain.RouteCandidate, evidence Evidence, weightedSimilarity float64, config Config) domain.Confidence {
	score := 1.0
	reasons := make([]string, 0, 5)
	if !evidence.TrafficDataAvailable {
		score -= 0.45
		reasons = append(reasons, "TRAFFIC_DATA_UNAVAILABLE")
	}
	unknownPercent := percent(candidate.Metrics.UnknownDistanceMeters, candidate.DistanceMeters)
	if unknownPercent > 0 {
		score -= math.Min(0.5, unknownPercent/100*0.65)
		reasons = append(reasons, "UNKNOWN_SEGMENTS")
	}
	if candidate.DistanceMeters > 0 {
		averageSimilarity := weightedSimilarity / float64(candidate.DistanceMeters)
		if averageSimilarity < config.MinimumGeometrySimilarity {
			score -= 0.35
			reasons = append(reasons, "LOW_BASELINE_GEOMETRY_SIMILARITY")
		} else if averageSimilarity < 0.9 {
			score -= (0.9 - averageSimilarity) * 0.5
		}
	}
	if !evidence.HasAlternatives {
		score -= 0.08
		reasons = append(reasons, "NO_COMPARABLE_ALTERNATIVES")
	}
	if evidence.ProviderErrors > 0 {
		score -= math.Min(0.25, float64(evidence.ProviderErrors)*0.08)
		reasons = append(reasons, "PROVIDER_ERRORS")
	}
	if evidence.DataAgeSeconds > 300 {
		score -= math.Min(0.2, float64(evidence.DataAgeSeconds-300)/3600*0.2)
		reasons = append(reasons, "STALE_TRAFFIC_DATA")
	} else if evidence.TrafficDataAvailable && !evidence.DataAgeKnown {
		score -= 0.05
		reasons = append(reasons, "TRAFFIC_FRESHNESS_UNKNOWN")
	}
	if evidence.TrafficDataType == domain.TrafficDataForecast {
		score -= 0.08
		reasons = append(reasons, "FORECAST_TRAFFIC_DATA")
	}
	result := confidence(clamp01(score), unique(reasons))
	if unknownPercent > config.UnknownMediumConfidenceMaxPercent {
		result.Level = domain.ConfidenceLow
	} else if unknownPercent > config.UnknownHighConfidenceMaxPercent && result.Level == domain.ConfidenceHigh {
		result.Level = domain.ConfidenceMedium
	}
	return result
}

func confidence(score float64, reasons []string) domain.Confidence {
	level := domain.ConfidenceLow
	if score >= 0.8 {
		level = domain.ConfidenceHigh
	} else if score >= 0.55 {
		level = domain.ConfidenceMedium
	}
	return domain.Confidence{Level: level, Score: round(score, 4), Reasons: unique(reasons)}
}

type ConstraintViolation struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func HardConstraintViolations(candidate, fastest domain.RouteCandidate, request domain.RouteSearchRequest, config Config) []ConstraintViolation {
	violations := make([]ConstraintViolation, 0, 12)
	if candidate.Blocked {
		violations = append(violations, ConstraintViolation{"ROUTE_BLOCKED", "route contains a blocked section"})
	}
	if len(candidate.Geometry) < 2 || candidate.DistanceMeters <= 0 || candidate.LiveDurationSeconds <= 0 {
		violations = append(violations, ConstraintViolation{"INVALID_GEOMETRY", "route geometry or measurements are invalid"})
	}
	extraDistance := candidate.DistanceMeters - fastest.DistanceMeters
	if request.MaxExtraDistanceMeters >= 0 && extraDistance > request.MaxExtraDistanceMeters {
		violations = append(violations, ConstraintViolation{"EXTRA_DISTANCE_LIMIT_EXCEEDED", "absolute extra distance limit exceeded"})
	}
	if request.MaxExtraDistancePercent > 0 && fastest.DistanceMeters > 0 && percent(extraDistance, fastest.DistanceMeters) > request.MaxExtraDistancePercent {
		violations = append(violations, ConstraintViolation{"EXTRA_DISTANCE_PERCENT_LIMIT_EXCEEDED", "percentage extra distance limit exceeded"})
	}
	if request.MaxExtraTimeSeconds >= 0 && candidate.LiveDurationSeconds-fastest.LiveDurationSeconds > request.MaxExtraTimeSeconds {
		violations = append(violations, ConstraintViolation{"EXTRA_TIME_LIMIT_EXCEEDED", "extra travel time limit exceeded"})
	}
	if candidate.Confidence.Score < config.MinimumRouteConfidence {
		violations = append(violations, ConstraintViolation{"CONFIDENCE_BELOW_MINIMUM", "traffic estimate confidence is below the configured minimum"})
	}
	if request.RoutingMode == domain.RoutingModeStrictGreen {
		violations = append(violations, strictGreenEvidenceViolations(candidate, config)...)
	}
	if request.AvoidTolls && candidate.Tolls {
		violations = append(violations, ConstraintViolation{"TOLLS_NOT_ALLOWED", "route contains toll roads"})
	}
	if request.AvoidUnpaved && candidate.Unpaved {
		violations = append(violations, ConstraintViolation{"UNPAVED_NOT_ALLOWED", "route contains unpaved roads"})
	}
	return violations
}

// strictGreenEvidenceViolations implements the fail-closed contract of
// STRICT_GREEN. A strict result is not "the least congested route": it is a
// route for which traffic evidence covers the complete provider-approved path
// and every meaningful segment is confidently classified as GREEN. UNKNOWN,
// yellow, orange and red segments are all ineligible. Synthetic and baseline-
// only data are not observations and therefore cannot prove an all-green path.
//
// Direction, access and road legality remain the routing provider's concern:
// this predicate only filters provider-returned candidates and never creates or
// mutates route geometry.
func strictGreenEvidenceViolations(candidate domain.RouteCandidate, config Config) []ConstraintViolation {
	violations := make([]ConstraintViolation, 0, 10)
	if candidate.TrafficDataType != domain.TrafficDataRealtime && candidate.TrafficDataType != domain.TrafficDataForecast {
		violations = append(violations, ConstraintViolation{
			"STRICT_GREEN_TRAFFIC_EVIDENCE_REQUIRED",
			"strict green requires observed or forecast traffic evidence",
		})
	}
	if candidate.Confidence.Level != domain.ConfidenceHigh && candidate.Confidence.Level != domain.ConfidenceMedium {
		violations = append(violations, ConstraintViolation{
			"STRICT_GREEN_ROUTE_CONFIDENCE_TOO_LOW",
			"strict green requires at least medium route confidence",
		})
	}

	var coveredDistance, coveredDuration int64
	meaningfulSegments := 0
	nonGreen := false
	unknown, yellow, orange, red := false, false, false, false
	lowConfidence := false
	for _, segment := range candidate.Segments {
		if segment.DistanceMeters <= 0 && segment.LiveDurationSeconds <= 0 {
			continue
		}
		meaningfulSegments++
		coveredDistance += max64(segment.DistanceMeters, 0)
		coveredDuration += max64(segment.LiveDurationSeconds, 0)
		if segment.CongestionClass != domain.CongestionGreen {
			nonGreen = true
			switch segment.CongestionClass {
			case domain.CongestionYellow:
				yellow = true
			case domain.CongestionOrange:
				orange = true
			case domain.CongestionRed:
				red = true
			default:
				unknown = true
			}
		}
		if segment.Confidence.Score < config.MinimumRouteConfidence ||
			(segment.Confidence.Level != domain.ConfidenceHigh && segment.Confidence.Level != domain.ConfidenceMedium) {
			lowConfidence = true
		}
	}
	if meaningfulSegments == 0 {
		violations = append(violations, ConstraintViolation{
			"STRICT_GREEN_SEGMENTS_REQUIRED",
			"strict green requires segment-level traffic evidence",
		})
	}
	if nonGreen {
		violations = append(violations, ConstraintViolation{
			"STRICT_GREEN_NON_GREEN_SEGMENT",
			"strict green rejects unknown, yellow, orange and red segments",
		})
	}
	if red {
		violations = append(violations, ConstraintViolation{"STRICT_GREEN_RED_SEGMENT", "strict green rejects red segments"})
	}
	if orange {
		violations = append(violations, ConstraintViolation{"STRICT_GREEN_ORANGE_SEGMENT", "strict green rejects orange segments"})
	}
	if yellow {
		violations = append(violations, ConstraintViolation{"STRICT_GREEN_YELLOW_SEGMENT", "strict green rejects yellow segments"})
	}
	if unknown {
		violations = append(violations, ConstraintViolation{"STRICT_GREEN_UNKNOWN_SEGMENT", "strict green rejects unknown segments"})
	}
	if lowConfidence {
		violations = append(violations, ConstraintViolation{
			"STRICT_GREEN_SEGMENT_CONFIDENCE_TOO_LOW",
			"strict green requires at least medium confidence for every segment",
		})
	}
	if meaningfulSegments > 0 && (coveredDistance != candidate.DistanceMeters || coveredDuration != candidate.LiveDurationSeconds) {
		violations = append(violations, ConstraintViolation{
			"STRICT_GREEN_SEGMENT_COVERAGE_INCOMPLETE",
			"strict green requires complete segment evidence for the provider route",
		})
	}
	return violations
}

type RankedRoutes struct {
	Selected   *domain.RouteCandidate
	Eligible   []domain.RouteCandidate
	BestEffort []domain.RouteCandidate
	Rejected   map[string][]ConstraintViolation
}

func Rank(candidates []domain.RouteCandidate, request domain.RouteSearchRequest, config Config) RankedRoutes {
	result := RankedRoutes{
		Eligible: []domain.RouteCandidate{}, BestEffort: []domain.RouteCandidate{},
		Rejected: make(map[string][]ConstraintViolation),
	}
	if len(candidates) == 0 {
		return result
	}
	fastest := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.LiveDurationSeconds < fastest.LiveDurationSeconds || (candidate.LiveDurationSeconds == fastest.LiveDurationSeconds && candidate.CandidateID < fastest.CandidateID) {
			fastest = candidate
		}
	}
	for _, candidate := range candidates {
		violations := HardConstraintViolations(candidate, fastest, request, config)
		if len(violations) > 0 {
			result.Rejected[candidate.CandidateID] = violations
			if request.RoutingMode == domain.RoutingModeStrictGreen && hasStrictGreenViolation(violations) {
				candidate.ReasonCodes = appendUnique(candidate.ReasonCodes, "BEST_EFFORT_NOT_STRICT_GREEN")
				for _, violation := range violations {
					candidate.ReasonCodes = appendUnique(candidate.ReasonCodes, violation.Code)
				}
				result.BestEffort = append(result.BestEffort, candidate)
			}
			continue
		}
		candidate.Score = routeScore(candidate, fastest, request, config)
		result.Eligible = append(result.Eligible, candidate)
	}
	slices.SortStableFunc(result.Eligible, func(a, b domain.RouteCandidate) int {
		return compareRoutes(a, b, fastest, request, config)
	})
	if len(result.Eligible) > 0 {
		selected := result.Eligible[0]
		selected.ReasonCodes = explain(selected, fastest, request)
		selected.Explanation = explanation(selected, fastest)
		result.Eligible[0] = selected
		result.Selected = &selected
	}
	slices.SortStableFunc(result.BestEffort, compareBestEffortRoutes)
	if len(result.BestEffort) > 3 {
		result.BestEffort = result.BestEffort[:3]
	}
	return result
}

func hasStrictGreenViolation(violations []ConstraintViolation) bool {
	for _, violation := range violations {
		if strings.HasPrefix(violation.Code, "STRICT_GREEN_") {
			return true
		}
	}
	return false
}

// compareBestEffortRoutes ranks proof-of-search candidates independently from
// route selection. Greater known GREEN coverage wins first. Remaining traffic
// classes are minimized in severity order, followed by uncertainty and a
// bounded preference for a reasonable ETA/distance. These routes never become
// eligible STRICT_GREEN alternatives through this ordering.
func compareBestEffortRoutes(a, b domain.RouteCandidate) int {
	return compareTuple(a, b, []func(domain.RouteCandidate) float64{
		func(c domain.RouteCandidate) float64 {
			return 1 - coverageRatio(c.Metrics.GreenDistanceMeters, c.DistanceMeters)
		},
		func(c domain.RouteCandidate) float64 {
			return 1 - coverageRatio(c.Metrics.GreenDurationSeconds, c.LiveDurationSeconds)
		},
		func(c domain.RouteCandidate) float64 {
			return coverageRatio(c.Metrics.RedDistanceMeters, c.DistanceMeters)
		},
		func(c domain.RouteCandidate) float64 {
			return coverageRatio(c.Metrics.RedDurationSeconds, c.LiveDurationSeconds)
		},
		func(c domain.RouteCandidate) float64 {
			return coverageRatio(c.Metrics.OrangeDistanceMeters, c.DistanceMeters)
		},
		func(c domain.RouteCandidate) float64 {
			return coverageRatio(c.Metrics.OrangeDurationSeconds, c.LiveDurationSeconds)
		},
		func(c domain.RouteCandidate) float64 {
			return coverageRatio(c.Metrics.YellowDistanceMeters, c.DistanceMeters)
		},
		func(c domain.RouteCandidate) float64 {
			return coverageRatio(c.Metrics.YellowDurationSeconds, c.LiveDurationSeconds)
		},
		func(c domain.RouteCandidate) float64 {
			return coverageRatio(c.Metrics.UnknownDistanceMeters, c.DistanceMeters)
		},
		func(c domain.RouteCandidate) float64 {
			return coverageRatio(c.Metrics.UnknownDurationSeconds, c.LiveDurationSeconds)
		},
		func(c domain.RouteCandidate) float64 { return 1 - clamp01(c.Confidence.Score) },
		func(c domain.RouteCandidate) float64 { return float64(c.LiveDurationSeconds) },
		func(c domain.RouteCandidate) float64 { return float64(c.DistanceMeters) },
	})
}

func coverageRatio(value, total int64) float64 {
	if value <= 0 || total <= 0 {
		return 0
	}
	return math.Min(1, float64(value)/float64(total))
}

func compareRoutes(a, b, fastest domain.RouteCandidate, request domain.RouteSearchRequest, config Config) int {
	switch request.RoutingMode {
	case domain.RoutingModeFastest:
		return compareTuple(a, b, []func(domain.RouteCandidate) float64{
			func(c domain.RouteCandidate) float64 { return float64(c.LiveDurationSeconds) },
			func(c domain.RouteCandidate) float64 { return float64(c.DistanceMeters) },
		})
	case domain.RoutingModeStrictGreen:
		return compareTuple(a, b, strictGreenTuple())
	case domain.RoutingModeGreenest:
		extraTimePenalty := config.GreenestExtraTimePenalty * (1.7 - clamp01(request.Strictness))
		return compareTuple(a, b, []func(domain.RouteCandidate) float64{
			unknownDurationRatio,
			unknownDistanceRatio,
			func(c domain.RouteCandidate) float64 {
				return float64(c.Metrics.RedDurationSeconds) + math.Max(0, float64(c.LiveDurationSeconds-fastest.LiveDurationSeconds))*extraTimePenalty
			},
			func(c domain.RouteCandidate) float64 { return float64(c.Metrics.RedDistanceMeters) },
			func(c domain.RouteCandidate) float64 { return float64(c.Metrics.OrangeDurationSeconds) },
			func(c domain.RouteCandidate) float64 { return 1 - c.Confidence.Score },
			func(c domain.RouteCandidate) float64 { return float64(c.Metrics.TotalTrafficDelaySeconds) },
			func(c domain.RouteCandidate) float64 { return float64(c.LiveDurationSeconds) },
			func(c domain.RouteCandidate) float64 { return float64(c.DistanceMeters) },
		})
	default:
		if delta := cmp.Compare(a.Score, b.Score); delta != 0 {
			return delta
		}
	}
	return cmp.Compare(a.CandidateID, b.CandidateID)
}

func strictGreenTuple() []func(domain.RouteCandidate) float64 {
	return []func(domain.RouteCandidate) float64{
		unknownDurationRatio,
		unknownDistanceRatio,
		func(c domain.RouteCandidate) float64 { return float64(c.Metrics.RedDurationSeconds) },
		func(c domain.RouteCandidate) float64 { return float64(c.Metrics.RedDistanceMeters) },
		func(c domain.RouteCandidate) float64 { return float64(c.Metrics.OrangeDurationSeconds) },
		func(c domain.RouteCandidate) float64 { return 1 - c.Confidence.Score },
		func(c domain.RouteCandidate) float64 { return float64(c.Metrics.TotalTrafficDelaySeconds) },
		func(c domain.RouteCandidate) float64 { return float64(c.Metrics.WorstContinuousCongestionMeters) },
		func(c domain.RouteCandidate) float64 { return float64(c.LiveDurationSeconds) },
		func(c domain.RouteCandidate) float64 { return float64(c.DistanceMeters) },
	}
}

func compareTuple(a, b domain.RouteCandidate, getters []func(domain.RouteCandidate) float64) int {
	for _, getter := range getters {
		if value := cmp.Compare(getter(a), getter(b)); value != 0 {
			return value
		}
	}
	return cmp.Compare(a.CandidateID, b.CandidateID)
}

func routeScore(candidate, fastest domain.RouteCandidate, request domain.RouteSearchRequest, config Config) float64 {
	if request.RoutingMode != domain.RoutingModeBalanced {
		return float64(candidate.Metrics.RedDurationSeconds*1_000_000+candidate.Metrics.OrangeDurationSeconds*1_000) + float64(candidate.LiveDurationSeconds)
	}
	strictnessDelta := clamp01(request.Strictness) - .7
	trafficWeight := config.BalancedTrafficWeight * (1 + strictnessDelta)
	etaWeight := config.BalancedETAWeight * (1 - .7*strictnessDelta)
	distanceWeight := config.BalancedDistanceWeight * (1 - .7*strictnessDelta)
	conservativeCongestedPercent := candidate.Metrics.CongestedDurationPercent + percent(candidate.Metrics.UnknownDurationSeconds, candidate.LiveDurationSeconds)
	trafficPenalty := 1 + trafficWeight*math.Min(1, conservativeCongestedPercent/100)
	etaPenalty := 1 + etaWeight*positiveRatio(candidate.LiveDurationSeconds-fastest.LiveDurationSeconds, fastest.LiveDurationSeconds)
	distancePenalty := 1 + distanceWeight*positiveRatio(candidate.DistanceMeters-fastest.DistanceMeters, fastest.DistanceMeters)
	uncertaintyPenalty := 1 + config.BalancedUncertaintyWeight*(1-candidate.Confidence.Score)
	tollPenalty := 1.0
	if candidate.Tolls {
		tollPenalty += config.BalancedTollWeight
	}
	return round(trafficPenalty*etaPenalty*distancePenalty*uncertaintyPenalty*tollPenalty, 8)
}

func explain(selected, fastest domain.RouteCandidate, request domain.RouteSearchRequest) []string {
	reasons := append([]string(nil), selected.ReasonCodes...)
	knownTraffic := selected.Metrics.UnknownDurationSeconds == 0 && selected.Metrics.UnknownDistanceMeters == 0 && selected.Confidence.Level != domain.ConfidenceLow
	if selected.Metrics.RedDurationSeconds == 0 && knownTraffic {
		reasons = appendUnique(reasons, "NO_RED_SEGMENTS")
	}
	lowestRed := selected.Metrics.RedDurationSeconds
	if knownTraffic && lowestRed <= fastest.Metrics.RedDurationSeconds {
		reasons = appendUnique(reasons, "LOWEST_RED_DURATION")
	}
	if selected.DistanceMeters > fastest.DistanceMeters && selected.Metrics.CongestedDurationPercent < fastest.Metrics.CongestedDurationPercent {
		reasons = appendUnique(reasons, "LONGER_BUT_SMOOTHER")
	}
	if selected.DistanceMeters-fastest.DistanceMeters <= request.MaxExtraDistanceMeters {
		reasons = appendUnique(reasons, "WITHIN_EXTRA_DISTANCE_LIMIT")
	}
	if selected.Confidence.Level == domain.ConfidenceLow {
		reasons = appendUnique(reasons, "LOW_CONFIDENCE_BASELINE")
	}
	return reasons
}

func unknownDurationRatio(candidate domain.RouteCandidate) float64 {
	if candidate.Metrics.UnknownDurationSeconds <= 0 {
		return 0
	}
	if candidate.LiveDurationSeconds <= 0 {
		return 1
	}
	return math.Min(1, float64(candidate.Metrics.UnknownDurationSeconds)/float64(candidate.LiveDurationSeconds))
}

func unknownDistanceRatio(candidate domain.RouteCandidate) float64 {
	if candidate.Metrics.UnknownDistanceMeters <= 0 {
		return 0
	}
	if candidate.DistanceMeters <= 0 {
		return 1
	}
	return math.Min(1, float64(candidate.Metrics.UnknownDistanceMeters)/float64(candidate.DistanceMeters))
}

func explanation(selected, fastest domain.RouteCandidate) string {
	extraKM := float64(selected.DistanceMeters-fastest.DistanceMeters) / 1000
	extraMinutes := float64(selected.LiveDurationSeconds-fastest.LiveDurationSeconds) / 60
	redSaved := float64(fastest.Metrics.RedDurationSeconds-selected.Metrics.RedDurationSeconds) / 60
	if redSaved > 0 && (extraKM > 0 || extraMinutes > 0) {
		return fmt.Sprintf("Route is %.1f km longer and %.0f min slower, but reduces estimated heavy-congestion time by about %.0f min.", extraKM, extraMinutes, redSaved)
	}
	if selected.CandidateID == fastest.CandidateID {
		return "Fastest eligible route; no smoother alternative within the selected limits was confirmed."
	}
	return "Best eligible route under the selected traffic, time, distance, and confidence constraints."
}

func percent(value, total int64) float64 {
	if total <= 0 || value <= 0 {
		return 0
	}
	return round(float64(value)/float64(total)*100, 3)
}

func positiveRatio(value, total int64) float64 {
	if value <= 0 || total <= 0 {
		return 0
	}
	return float64(value) / float64(total)
}

func round(value float64, precision int) float64 {
	factor := math.Pow10(precision)
	return math.Round(value*factor) / factor
}

func clamp01(value float64) float64 { return math.Max(0, math.Min(1, value)) }

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func unique(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
