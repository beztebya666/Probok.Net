package scoring

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/greenroute/greenroute/internal/domain"
	"github.com/greenroute/greenroute/internal/geometry"
)

type goldenCase struct {
	Name                   string             `json:"name"`
	Mode                   domain.RoutingMode `json:"mode"`
	MaxExtraDistanceMeters int64              `json:"maxExtraDistanceMeters"`
	MaxExtraTimeSeconds    int64              `json:"maxExtraTimeSeconds"`
	Dedupe                 bool               `json:"dedupe"`
	Candidates             []goldenCandidate  `json:"candidates"`
	Selected               string             `json:"selected"`
	Rejected               []string           `json:"rejected"`
	Deduplicated           int                `json:"deduplicated"`
}

type goldenCandidate struct {
	ID         string  `json:"id"`
	Distance   int64   `json:"distance"`
	Live       int64   `json:"live"`
	Baseline   int64   `json:"baseline"`
	Similarity float64 `json:"similarity"`
	Offset     float64 `json:"offset"`
}

func TestGoldenRoutePolicy(t *testing.T) {
	payload, err := os.ReadFile("testdata/golden-routes.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []goldenCase
	if err := json.Unmarshal(payload, &cases); err != nil {
		t.Fatal(err)
	}
	config := DefaultConfig()
	for _, test := range cases {
		t.Run(test.Name, func(t *testing.T) {
			candidates := make([]domain.RouteCandidate, 0, len(test.Candidates))
			for _, fixture := range test.Candidates {
				candidate := goldenRoute(fixture)
				candidate = Evaluate(candidate, Evidence{TrafficDataAvailable: fixture.Baseline > 0, TrafficDataType: domain.TrafficDataRealtime, DataAgeKnown: true, HasAlternatives: len(test.Candidates) > 1}, config)
				candidates = append(candidates, candidate)
			}
			deduplicated := 0
			if test.Dedupe {
				candidates, deduplicated = geometry.Deduplicate(candidates, geometry.DefaultDedupeConfig())
			}
			if deduplicated != test.Deduplicated {
				t.Fatalf("deduplicated=%d, want %d", deduplicated, test.Deduplicated)
			}
			request := domain.RouteSearchRequest{
				RoutingMode: test.Mode, MaxExtraDistanceMeters: test.MaxExtraDistanceMeters,
				MaxExtraDistancePercent: 300, MaxExtraTimeSeconds: test.MaxExtraTimeSeconds, Strictness: .7,
			}
			ranked := Rank(candidates, request, config)
			if test.Selected == "" {
				if ranked.Selected != nil || len(ranked.Eligible) != 0 {
					t.Fatalf("selected=%#v, want no strict-green fallback; eligible=%v rejected=%v", ranked.Selected, ranked.Eligible, ranked.Rejected)
				}
			} else if ranked.Selected == nil || ranked.Selected.CandidateID != test.Selected {
				t.Fatalf("selected=%#v, want %s; rejected=%v", ranked.Selected, test.Selected, ranked.Rejected)
			}
			for _, expected := range test.Rejected {
				if _, ok := ranked.Rejected[expected]; !ok {
					t.Fatalf("expected %s to be rejected; got %v", expected, ranked.Rejected)
				}
			}
			for id := range ranked.Rejected {
				if len(test.Rejected) > 0 && !slices.Contains(test.Rejected, id) {
					t.Fatalf("unexpected rejection of %s", id)
				}
			}
		})
	}
}

func goldenRoute(fixture goldenCandidate) domain.RouteCandidate {
	points := []domain.GeoPoint{
		{Latitude: 55.70 + fixture.Offset, Longitude: 37.50},
		{Latitude: 55.75 + fixture.Offset, Longitude: 37.55},
		{Latitude: 55.80 + fixture.Offset, Longitude: 37.60},
	}
	return domain.RouteCandidate{
		CandidateID: fixture.ID, Provider: "synthetic", Geometry: points,
		TrafficDataType: domain.TrafficDataRealtime,
		DistanceMeters:  fixture.Distance, LiveDurationSeconds: fixture.Live, BaselineDurationSeconds: fixture.Baseline,
		Segments: []domain.RouteSegment{{
			SegmentID: fixture.ID + "-segment", Geometry: points, DistanceMeters: fixture.Distance,
			LiveDurationSeconds: fixture.Live, BaselineDurationSeconds: fixture.Baseline,
			GeometrySimilarity: fixture.Similarity, Source: "SYNTHETIC_GOLDEN",
		}},
	}
}
