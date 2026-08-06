package geometry

import (
	"math"
	"testing"

	"github.com/greenroute/greenroute/internal/domain"
)

func TestDeduplicateRetainsFastestEquivalentRoute(t *testing.T) {
	a := domain.RouteCandidate{CandidateID: "slow", DistanceMeters: 10_000, LiveDurationSeconds: 1_000, Geometry: line(0)}
	b := domain.RouteCandidate{CandidateID: "fast", DistanceMeters: 10_050, LiveDurationSeconds: 900, Geometry: line(0.0001)}
	unique, dropped := Deduplicate([]domain.RouteCandidate{a, b}, DefaultDedupeConfig())
	if dropped != 1 || len(unique) != 1 || unique[0].CandidateID != "fast" {
		t.Fatalf("unexpected result: dropped=%d candidates=%v", dropped, unique)
	}
}

func TestDensifyCapsAntipodalAndZigzagInput(t *testing.T) {
	antipodal := []domain.GeoPoint{{Latitude: 0, Longitude: 0}, {Latitude: 0, Longitude: 179.999}}
	if sampled := Densify(antipodal, 1); len(sampled) != MaxSampledPoints {
		t.Fatalf("antipodal line was not bounded: %d samples", len(sampled))
	}
	zigzag := make([]domain.GeoPoint, 20_000)
	for index := range zigzag {
		zigzag[index] = domain.GeoPoint{Latitude: 55 + float64(index%2), Longitude: 30 + float64(index)/10_000}
	}
	sampled := Densify(zigzag, 1)
	if len(sampled) > MaxSampledPoints {
		t.Fatalf("zigzag line was not bounded: %d samples", len(sampled))
	}
	if similarity := Similarity(sampled, sampled, 750); math.IsNaN(similarity) || similarity < .99 {
		t.Fatalf("bounded self-similarity is invalid: %f", similarity)
	}
}

func TestSimplifyBoundsAdversarialInput(t *testing.T) {
	points := make([]domain.GeoPoint, 20_000)
	for index := range points {
		points[index] = domain.GeoPoint{Latitude: 50 + float64(index%2), Longitude: 20 + float64(index)/20_000}
	}
	result := Simplify(points, 1)
	if len(result) > maxSimplifyInputPoints || result[0] != points[0] || result[len(result)-1] != points[len(points)-1] {
		t.Fatalf("simplification was not bounded or lost endpoints: %d", len(result))
	}
}

func TestDistinctCorridorsAreNotDeduplicated(t *testing.T) {
	a := domain.RouteCandidate{CandidateID: "a", DistanceMeters: 10_000, LiveDurationSeconds: 1_000, Geometry: line(0)}
	b := domain.RouteCandidate{CandidateID: "b", DistanceMeters: 10_000, LiveDurationSeconds: 1_000, Geometry: line(0.05)}
	unique, _ := Deduplicate([]domain.RouteCandidate{a, b}, DefaultDedupeConfig())
	if len(unique) != 2 {
		t.Fatalf("distinct corridors were collapsed: %v", unique)
	}
}

func line(offset float64) []domain.GeoPoint {
	return []domain.GeoPoint{{Latitude: 55.7 + offset, Longitude: 37.5}, {Latitude: 55.75 + offset, Longitude: 37.55}, {Latitude: 55.8 + offset, Longitude: 37.6}}
}
