package contracts

import (
	"testing"

	"github.com/greenroute/greenroute/internal/domain"
)

func TestRouteSearchInputRequiresPresenceAndPreservesExplicitZeroStrictness(t *testing.T) {
	origin := domain.GeoPoint{Latitude: 55.75, Longitude: 37.61}
	destination := domain.GeoPoint{Latitude: 55.77, Longitude: 37.64}
	mode := domain.RoutingModeGreenest
	distance, duration := int64(0), int64(0)
	zero := 0.0
	complete := RouteSearchInput{
		Origin: &origin, Destination: &destination, RoutingMode: &mode,
		MaxExtraDistanceMeters: &distance, MaxExtraTimeSeconds: &duration, Strictness: &zero,
	}
	request, err := complete.Request()
	if err != nil || request.Strictness != 0 {
		t.Fatalf("explicit zero strictness was lost: request=%#v err=%v", request, err)
	}
	tests := []struct {
		name  string
		input RouteSearchInput
	}{
		{"origin", func() RouteSearchInput { value := complete; value.Origin = nil; return value }()},
		{"destination", func() RouteSearchInput { value := complete; value.Destination = nil; return value }()},
		{"routingMode", func() RouteSearchInput { value := complete; value.RoutingMode = nil; return value }()},
		{"maxExtraDistanceMeters", func() RouteSearchInput { value := complete; value.MaxExtraDistanceMeters = nil; return value }()},
		{"maxExtraTimeSeconds", func() RouteSearchInput { value := complete; value.MaxExtraTimeSeconds = nil; return value }()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := test.input.Request(); err == nil {
				t.Fatal("missing required property was accepted")
			}
		})
	}
}
