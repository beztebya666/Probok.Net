package domain

import "testing"

func TestDefaultValidationLimitsAllowConfiguredDetourMaximums(t *testing.T) {
	request := validRouteSearchRequest()
	request.MaxExtraDistanceMeters = 150_000
	request.MaxExtraTimeSeconds = 18_000
	if err := request.Validate(DefaultValidationLimits()); err != nil {
		t.Fatalf("documented maximums were rejected: %v", err)
	}
}

func TestDefaultValidationLimitsRejectExcessiveDetours(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*RouteSearchRequest)
	}{
		{"distance", func(request *RouteSearchRequest) { request.MaxExtraDistanceMeters = 150_001 }},
		{"time", func(request *RouteSearchRequest) { request.MaxExtraTimeSeconds = 18_001 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := validRouteSearchRequest()
			test.mutate(&request)
			if err := request.Validate(DefaultValidationLimits()); err == nil {
				t.Fatal("request above the documented detour maximum was accepted")
			}
		})
	}
}

func validRouteSearchRequest() RouteSearchRequest {
	return RouteSearchRequest{
		Origin: GeoPoint{Latitude: 55.70, Longitude: 37.50}, Destination: GeoPoint{Latitude: 55.80, Longitude: 37.60},
		RoutingMode: RoutingModeStrictGreen, MaxExtraDistanceMeters: 30_000, MaxExtraDistancePercent: 300,
		MaxExtraTimeSeconds: 3_600, Strictness: 1, MaxProviderRequests: 8, SearchDeadlineMS: 10_000,
	}
}
