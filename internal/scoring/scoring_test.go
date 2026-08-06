package scoring

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/greenroute/greenroute/internal/domain"
)

func TestClassifyTrafficThresholdsAndUnknown(t *testing.T) {
	config := DefaultConfig()
	tests := []struct {
		name       string
		ratio      float64
		similarity float64
		want       domain.CongestionClass
	}{
		{"green below boundary", 1.149, 1, domain.CongestionGreen},
		{"yellow inclusive", 1.15, 1, domain.CongestionYellow},
		{"orange inclusive", 1.35, 1, domain.CongestionOrange},
		{"red inclusive", 1.65, 1, domain.CongestionRed},
		{"missing baseline never green", 0, 1, domain.CongestionUnknown},
		{"mismatched geometry never green", 1.01, 0.4, domain.CongestionUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ClassifyTraffic(test.ratio, test.similarity, config); got != test.want {
				t.Fatalf("got %s, want %s", got, test.want)
			}
		})
	}
}

func TestScoringConfigRejectsNonFiniteThresholds(t *testing.T) {
	config := DefaultConfig()
	config.GreenMaxTrafficRatio = math.NaN()
	if err := config.Validate(); err == nil {
		t.Fatal("NaN threshold was accepted")
	}
	config = DefaultConfig()
	config.MinimumGeometrySimilarity = math.Inf(1)
	if err := config.Validate(); err == nil {
		t.Fatal("infinite threshold was accepted")
	}
}

func TestPolicyFileRejectsUnknownFields(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "policy.json")
	payload, err := json.Marshal(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	payload = append(payload[:len(payload)-1], []byte(`,"typoWeight":1}`)...)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfigFile(path); err == nil {
		t.Fatal("unknown scoring field was accepted")
	}
}

func TestPolicyFileIsStrictlyValidated(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "policy.json")
	if err := os.WriteFile(path, []byte(`{"policyVersion":"broken","greenMaxTrafficRatio":1.15,"yellowMaxTrafficRatio":1.35,"orangeMaxTrafficRatio":1.65,"minimumGeometrySimilarity":0.72,"minimumRouteConfidence":0.35,"unknownHighConfidenceMaxPercent":5,"unknownMediumConfidenceMaxPercent":25,"balancedTrafficWeight":0.4,"balancedEtaWeight":0.25,"balancedDistanceWeight":0.15,"balancedUncertaintyWeight":0.15,"balancedTollWeight":0.50,"greenestExtraTimePenalty":0.15}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfigFile(path); err == nil {
		t.Fatal("invalid non-normalized weights were accepted")
	}
}

func TestStrictGreenSelectsOnlyFullyVerifiedGreenRoutes(t *testing.T) {
	config := DefaultConfig()
	fastGreen := evaluatedTrafficCandidate("fast-green", 10_000, 1_000, 950, domain.CongestionGreen, domain.TrafficDataRealtime, true)
	slowGreen := evaluatedTrafficCandidate("slow-green", 12_000, 1_100, 1_050, domain.CongestionGreen, domain.TrafficDataRealtime, true)
	yellow := evaluatedTrafficCandidate("yellow", 9_000, 900, 780, domain.CongestionYellow, domain.TrafficDataRealtime, true)
	ranked := Rank([]domain.RouteCandidate{yellow, slowGreen, fastGreen}, request(domain.RoutingModeStrictGreen), config)
	if ranked.Selected == nil || ranked.Selected.CandidateID != fastGreen.CandidateID {
		t.Fatalf("expected fastest fully green route, got %#v; rejected=%v", ranked.Selected, ranked.Rejected)
	}
	if len(ranked.Eligible) != 2 {
		t.Fatalf("eligible=%d, want only the two verified green routes", len(ranked.Eligible))
	}
	if !hasViolation(ranked.Rejected[yellow.CandidateID], "STRICT_GREEN_NON_GREEN_SEGMENT") {
		t.Fatalf("yellow route was not rejected by strict-green invariant: %v", ranked.Rejected)
	}
}

func TestStrictGreenFailsClosedForUnknownLowConfidenceAndIncompleteCoverage(t *testing.T) {
	config := DefaultConfig()
	valid := evaluatedTrafficCandidate("valid", 1_000, 100, 95, domain.CongestionGreen, domain.TrafficDataRealtime, true)
	unknownType := evaluatedTrafficCandidate("unknown-type", 1_000, 100, 95, domain.CongestionGreen, domain.TrafficDataRealtime, true)
	unknownType.TrafficDataType = domain.TrafficDataUnknown
	unknownSegment := evaluatedTrafficCandidate("unknown-segment", 1_000, 100, 95, domain.CongestionGreen, domain.TrafficDataRealtime, true)
	unknownSegment.Segments[0].CongestionClass = domain.CongestionUnknown
	lowConfidence := evaluatedTrafficCandidate("low-confidence", 1_000, 100, 95, domain.CongestionGreen, domain.TrafficDataRealtime, true)
	lowConfidence.Segments[0].Confidence = domain.Confidence{Level: domain.ConfidenceLow, Score: .54}
	incomplete := evaluatedTrafficCandidate("incomplete", 1_000, 100, 95, domain.CongestionGreen, domain.TrafficDataRealtime, true)
	incomplete.Segments[0].DistanceMeters--

	ranked := Rank([]domain.RouteCandidate{unknownType, unknownSegment, lowConfidence, incomplete, valid}, request(domain.RoutingModeStrictGreen), config)
	if ranked.Selected == nil || ranked.Selected.CandidateID != valid.CandidateID || len(ranked.Eligible) != 1 {
		t.Fatalf("strict green did not isolate the only verified route: selected=%#v eligible=%v rejected=%v", ranked.Selected, ranked.Eligible, ranked.Rejected)
	}
	assertViolation(t, ranked.Rejected[unknownType.CandidateID], "STRICT_GREEN_TRAFFIC_EVIDENCE_REQUIRED")
	assertViolation(t, ranked.Rejected[unknownSegment.CandidateID], "STRICT_GREEN_NON_GREEN_SEGMENT")
	assertViolation(t, ranked.Rejected[lowConfidence.CandidateID], "STRICT_GREEN_SEGMENT_CONFIDENCE_TOO_LOW")
	assertViolation(t, ranked.Rejected[incomplete.CandidateID], "STRICT_GREEN_SEGMENT_COVERAGE_INCOMPLETE")
}

func TestStrictGreenBestEffortRanksAndCapsRejectedCandidates(t *testing.T) {
	candidates := []domain.RouteCandidate{
		bestEffortCandidate("green-98-red", 9_800, domain.CongestionRed),
		bestEffortCandidate("green-97-unknown", 9_700, domain.CongestionUnknown),
		bestEffortCandidate("green-99-red", 9_900, domain.CongestionRed),
		bestEffortCandidate("green-98-yellow", 9_800, domain.CongestionYellow),
		bestEffortCandidate("green-96-orange", 9_600, domain.CongestionOrange),
	}
	ranked := Rank(candidates, request(domain.RoutingModeStrictGreen), DefaultConfig())
	if ranked.Selected != nil || len(ranked.Eligible) != 0 {
		t.Fatalf("best-effort candidates leaked into strict selection: %#v", ranked)
	}
	want := []string{"green-99-red", "green-98-yellow", "green-98-red"}
	if len(ranked.BestEffort) != len(want) {
		t.Fatalf("best effort count=%d, want %d: %#v", len(ranked.BestEffort), len(want), ranked.BestEffort)
	}
	for index, candidate := range ranked.BestEffort {
		if candidate.CandidateID != want[index] {
			t.Fatalf("bestEffort[%d]=%s, want %s", index, candidate.CandidateID, want[index])
		}
		if !slices.Contains(candidate.ReasonCodes, "BEST_EFFORT_NOT_STRICT_GREEN") || !slices.Contains(candidate.ReasonCodes, "STRICT_GREEN_NON_GREEN_SEGMENT") {
			t.Fatalf("best-effort rejection reasons missing: %#v", candidate.ReasonCodes)
		}
		if strings.Contains(candidate.CandidateID, "red") && !slices.Contains(candidate.ReasonCodes, "STRICT_GREEN_RED_SEGMENT") {
			t.Fatalf("red best-effort route lacks precise class reason: %#v", candidate.ReasonCodes)
		}
		if strings.Contains(candidate.CandidateID, "yellow") && !slices.Contains(candidate.ReasonCodes, "STRICT_GREEN_YELLOW_SEGMENT") {
			t.Fatalf("yellow best-effort route lacks precise class reason: %#v", candidate.ReasonCodes)
		}
	}
}

func TestHardConstraintsCannotBeSilentlyViolated(t *testing.T) {
	fastest := candidate("fast", 10_000, 1_000, 600, 100, 0.95)
	detour := candidate("detour", 15_001, 1_100, 0, 0, 0.95)
	request := request(domain.RoutingModeGreenest)
	request.MaxExtraDistanceMeters = 5_000
	ranked := Rank([]domain.RouteCandidate{fastest, detour}, request, DefaultConfig())
	if _, rejected := ranked.Rejected["detour"]; !rejected {
		t.Fatal("route beyond distance limit was not rejected")
	}
	if ranked.Selected == nil || ranked.Selected.CandidateID != "fast" {
		t.Fatalf("expected fastest eligible route, got %#v", ranked.Selected)
	}
	if len(ranked.BestEffort) != 0 {
		t.Fatalf("best-effort proof routes are strict-green only: %#v", ranked.BestEffort)
	}
}

func TestConfidenceDropsForUnknownGeometry(t *testing.T) {
	c := candidate("unknown", 1_000, 100, 0, 0, 0)
	c.Segments = []domain.RouteSegment{{SegmentID: "s", Geometry: c.Geometry, DistanceMeters: 1_000, LiveDurationSeconds: 100, GeometrySimilarity: 0.2}}
	got := Evaluate(c, Evidence{TrafficDataAvailable: false}, DefaultConfig())
	if got.Confidence.Level != domain.ConfidenceLow {
		t.Fatalf("got confidence %v, want LOW", got.Confidence)
	}
	if got.Segments[0].CongestionClass != domain.CongestionUnknown {
		t.Fatalf("UNKNOWN must not be reclassified as green: %s", got.Segments[0].CongestionClass)
	}
}

func TestExplicitUnknownEvidenceCannotProduceGreenTraffic(t *testing.T) {
	c := candidate("unknown-source", 1_000, 120, 0, 0, .9)
	c.BaselineDurationSeconds = 100
	c.Segments = []domain.RouteSegment{{
		SegmentID: "s", Geometry: c.Geometry, DistanceMeters: 1_000,
		LiveDurationSeconds: 120, BaselineDurationSeconds: 100, GeometrySimilarity: 1,
	}}
	got := Evaluate(c, Evidence{TrafficDataAvailable: true, TrafficDataType: domain.TrafficDataUnknown, HasAlternatives: true}, DefaultConfig())
	if got.Segments[0].CongestionClass != domain.CongestionUnknown || got.Metrics.UnknownDistanceMeters != 1_000 {
		t.Fatalf("unknown source was classified as observed traffic: %#v", got)
	}
}

func TestProviderTrafficColorIsPreservedWithoutBaseline(t *testing.T) {
	c := candidate("provider-color", 1_000, 100, 0, 0, 0)
	c.TrafficDataType = domain.TrafficDataRealtime
	c.Segments = []domain.RouteSegment{{
		SegmentID: "provider-color-segment", Geometry: c.Geometry, DistanceMeters: c.DistanceMeters,
		LiveDurationSeconds: c.LiveDurationSeconds, CongestionClass: domain.CongestionGreen,
		GeometrySimilarity: 1, Source: domain.SegmentSourceDGISTrafficColor,
	}}
	got := Evaluate(c, Evidence{TrafficDataAvailable: true, TrafficDataType: domain.TrafficDataRealtime, HasAlternatives: true}, DefaultConfig())
	if got.Segments[0].CongestionClass != domain.CongestionGreen {
		t.Fatalf("provider traffic color was overwritten: %#v", got.Segments[0])
	}
	if got.Segments[0].Confidence.Level != domain.ConfidenceMedium {
		t.Fatalf("inferred provider color confidence=%v, want MEDIUM", got.Segments[0].Confidence)
	}
	if got.Metrics.GreenDistanceMeters != c.DistanceMeters || got.Metrics.UnknownDistanceMeters != 0 {
		t.Fatalf("provider color did not produce green coverage: %#v", got.Metrics)
	}
}

func TestIncompleteSegmentCoverageIsCountedAsUnknown(t *testing.T) {
	c := candidate("partial-provider-color", 1_000, 100, 0, 0, 0)
	c.TrafficDataType = domain.TrafficDataRealtime
	c.Segments = []domain.RouteSegment{{
		SegmentID: "partial-segment", Geometry: c.Geometry, DistanceMeters: 900,
		LiveDurationSeconds: 90, CongestionClass: domain.CongestionGreen,
		Source: domain.SegmentSourceDGISTrafficColor,
	}}
	got := Evaluate(c, Evidence{TrafficDataAvailable: true, TrafficDataType: domain.TrafficDataRealtime, DataAgeKnown: true}, DefaultConfig())
	if got.Metrics.GreenDistancePercent != 90 || got.Metrics.UnknownDistanceMeters != 100 || got.Metrics.UnknownDurationSeconds != 10 {
		t.Fatalf("incomplete evidence was not exposed as UNKNOWN: %#v", got.Metrics)
	}
	if !slices.Contains(got.ReasonCodes, "PARTIAL_TRAFFIC_DATA") {
		t.Fatalf("partial evidence reason missing: %#v", got.ReasonCodes)
	}
}

func TestForecastEvidenceHasLowerConfidenceThanRealtime(t *testing.T) {
	c := candidate("forecast", 1_000, 120, 0, 0, .9)
	c.BaselineDurationSeconds = 100
	c.Segments = []domain.RouteSegment{{
		SegmentID: "s", Geometry: c.Geometry, DistanceMeters: 1_000,
		LiveDurationSeconds: 120, BaselineDurationSeconds: 100, GeometrySimilarity: 1,
	}}
	realtime := Evaluate(c, Evidence{TrafficDataAvailable: true, TrafficDataType: domain.TrafficDataRealtime, HasAlternatives: true}, DefaultConfig())
	forecast := Evaluate(c, Evidence{TrafficDataAvailable: true, TrafficDataType: domain.TrafficDataForecast, HasAlternatives: true}, DefaultConfig())
	if forecast.Confidence.Score >= realtime.Confidence.Score {
		t.Fatalf("forecast confidence %.2f must be below realtime %.2f", forecast.Confidence.Score, realtime.Confidence.Score)
	}
}

func TestUnknownTrafficFreshnessIsNotTreatedAsFresh(t *testing.T) {
	c := candidate("freshness", 1_000, 120, 0, 0, .9)
	c.BaselineDurationSeconds = 100
	c.Segments = []domain.RouteSegment{{
		SegmentID: "s", Geometry: c.Geometry, DistanceMeters: 1_000,
		LiveDurationSeconds: 120, BaselineDurationSeconds: 100, GeometrySimilarity: 1,
	}}

	unknown := Evaluate(c, Evidence{TrafficDataAvailable: true, TrafficDataType: domain.TrafficDataRealtime, HasAlternatives: true}, DefaultConfig())
	known := Evaluate(c, Evidence{TrafficDataAvailable: true, TrafficDataType: domain.TrafficDataRealtime, DataAgeKnown: true, HasAlternatives: true}, DefaultConfig())

	if unknown.Confidence.Score >= known.Confidence.Score {
		t.Fatalf("unknown freshness score=%v, known score=%v", unknown.Confidence.Score, known.Confidence.Score)
	}
	if !slices.Contains(unknown.Confidence.Reasons, "TRAFFIC_FRESHNESS_UNKNOWN") {
		t.Fatalf("expected explicit freshness reason, got %v", unknown.Confidence.Reasons)
	}
}

func candidate(id string, distance, duration, red, orange int64, confidenceScore float64) domain.RouteCandidate {
	return domain.RouteCandidate{
		CandidateID: id, Geometry: []domain.GeoPoint{{Latitude: 55.7, Longitude: 37.5}, {Latitude: 55.8, Longitude: 37.6}},
		DistanceMeters: distance, LiveDurationSeconds: duration,
		Confidence: domain.Confidence{Level: domain.ConfidenceHigh, Score: confidenceScore},
		Metrics:    domain.RouteMetrics{RedDurationSeconds: red, OrangeDurationSeconds: orange, CongestedDurationPercent: float64(red+orange) / float64(duration) * 100},
	}
}

func request(mode domain.RoutingMode) domain.RouteSearchRequest {
	return domain.RouteSearchRequest{RoutingMode: mode, MaxExtraDistanceMeters: 100_000, MaxExtraDistancePercent: 300, MaxExtraTimeSeconds: 100_000, Strictness: .7}
}

func TestGreenestStrictnessChangesDetourTradeoff(t *testing.T) {
	fast := candidate("fast", 10_000, 1_000, 100, 0, .95)
	detour := candidate("detour", 12_000, 1_400, 50, 0, .95)
	low := request(domain.RoutingModeGreenest)
	low.Strictness = 0
	high := low
	high.Strictness = 1
	if selected := Rank([]domain.RouteCandidate{fast, detour}, low, DefaultConfig()).Selected; selected == nil || selected.CandidateID != "fast" {
		t.Fatalf("low strictness should retain faster route, got %#v", selected)
	}
	if selected := Rank([]domain.RouteCandidate{fast, detour}, high, DefaultConfig()).Selected; selected == nil || selected.CandidateID != "detour" {
		t.Fatalf("high strictness should accept smoother detour, got %#v", selected)
	}
}

func TestUnknownTrafficNeverOutranksConfirmedCongestionAsGreener(t *testing.T) {
	known := candidate("known-red", 10_000, 1_000, 120, 0, .95)
	unknown := candidate("unknown", 10_000, 900, 0, 0, .95)
	unknown.Metrics.UnknownDurationSeconds = unknown.LiveDurationSeconds
	unknown.Metrics.UnknownDistanceMeters = unknown.DistanceMeters
	for _, mode := range []domain.RoutingMode{domain.RoutingModeBalanced, domain.RoutingModeGreenest} {
		t.Run(string(mode), func(t *testing.T) {
			ranked := Rank([]domain.RouteCandidate{unknown, known}, request(mode), DefaultConfig())
			if ranked.Selected == nil || ranked.Selected.CandidateID != known.CandidateID {
				t.Fatalf("UNKNOWN was treated as greener than confirmed traffic: %#v", ranked.Selected)
			}
		})
	}

	ranked := Rank([]domain.RouteCandidate{unknown, known}, request(domain.RoutingModeStrictGreen), DefaultConfig())
	if ranked.Selected != nil || len(ranked.Eligible) != 0 {
		t.Fatalf("strict green exposed an uncertain or congested fallback: %#v", ranked)
	}
}

func evaluatedTrafficCandidate(id string, distance, live, baseline int64, class domain.CongestionClass, dataType domain.TrafficDataType, alternatives bool) domain.RouteCandidate {
	c := candidate(id, distance, live, 0, 0, 0)
	c.TrafficDataType = dataType
	c.BaselineDurationSeconds = baseline
	c.Segments = []domain.RouteSegment{{
		SegmentID: id + "-segment", Geometry: c.Geometry, DistanceMeters: distance,
		LiveDurationSeconds: live, BaselineDurationSeconds: baseline, GeometrySimilarity: 1,
		CongestionClass: class, Source: domain.SegmentSourceDGISTrafficColor,
	}}
	return Evaluate(c, Evidence{TrafficDataAvailable: dataType == domain.TrafficDataRealtime || dataType == domain.TrafficDataForecast, TrafficDataType: dataType, DataAgeKnown: true, HasAlternatives: alternatives}, DefaultConfig())
}

func bestEffortCandidate(id string, greenDistance int64, remainderClass domain.CongestionClass) domain.RouteCandidate {
	const distance = int64(10_000)
	const duration = int64(1_000)
	greenDuration := greenDistance / 10
	points := []domain.GeoPoint{{Latitude: 55.70, Longitude: 37.50}, {Latitude: 55.75, Longitude: 37.55}, {Latitude: 55.80, Longitude: 37.60}}
	candidate := domain.RouteCandidate{
		CandidateID: id, Provider: "2gis", TrafficDataType: domain.TrafficDataRealtime,
		Geometry: points, DistanceMeters: distance, LiveDurationSeconds: duration,
		Segments: []domain.RouteSegment{
			{SegmentID: id + "-green", Geometry: points[:2], DistanceMeters: greenDistance, LiveDurationSeconds: greenDuration, CongestionClass: domain.CongestionGreen, Source: domain.SegmentSourceDGISTrafficColor},
			{SegmentID: id + "-remainder", Geometry: points[1:], DistanceMeters: distance - greenDistance, LiveDurationSeconds: duration - greenDuration, CongestionClass: remainderClass, Source: domain.SegmentSourceDGISTrafficColor},
		},
	}
	return Evaluate(candidate, Evidence{TrafficDataAvailable: true, TrafficDataType: domain.TrafficDataRealtime, DataAgeKnown: true, HasAlternatives: true}, DefaultConfig())
}

func hasViolation(violations []ConstraintViolation, code string) bool {
	for _, violation := range violations {
		if violation.Code == code {
			return true
		}
	}
	return false
}

func assertViolation(t *testing.T, violations []ConstraintViolation, code string) {
	t.Helper()
	if !hasViolation(violations, code) {
		t.Fatalf("missing violation %s in %v", code, violations)
	}
}
