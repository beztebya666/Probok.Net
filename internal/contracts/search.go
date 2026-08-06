package contracts

import (
	"fmt"
	"time"

	"github.com/greenroute/greenroute/internal/domain"
)

// RouteSearchInput is the presence-aware wire contract used at both public and
// internal HTTP boundaries. Pointers distinguish an omitted required value
// from a deliberately supplied zero value before safe optional defaults apply.
type RouteSearchInput struct {
	RequestID               string              `json:"requestId,omitempty"`
	Origin                  *domain.GeoPoint    `json:"origin"`
	Destination             *domain.GeoPoint    `json:"destination"`
	Waypoints               []domain.GeoPoint   `json:"waypoints,omitempty"`
	DepartureTime           *time.Time          `json:"departureTime,omitempty"`
	RoutingMode             *domain.RoutingMode `json:"routingMode"`
	MaxExtraDistanceMeters  *int64              `json:"maxExtraDistanceMeters"`
	MaxExtraDistancePercent *float64            `json:"maxExtraDistancePercent,omitempty"`
	MaxExtraTimeSeconds     *int64              `json:"maxExtraTimeSeconds"`
	AvoidTolls              bool                `json:"avoidTolls,omitempty"`
	AvoidUnpaved            bool                `json:"avoidUnpaved,omitempty"`
	Strictness              *float64            `json:"strictness,omitempty"`
	MaxProviderRequests     *int                `json:"maxProviderRequests,omitempty"`
	SearchDeadlineMS        *int                `json:"searchDeadlineMs,omitempty"`
}

func (input RouteSearchInput) Request() (domain.RouteSearchRequest, error) {
	missing := ""
	switch {
	case input.Origin == nil:
		missing = "origin"
	case input.Destination == nil:
		missing = "destination"
	case input.RoutingMode == nil:
		missing = "routingMode"
	case input.MaxExtraDistanceMeters == nil:
		missing = "maxExtraDistanceMeters"
	case input.MaxExtraTimeSeconds == nil:
		missing = "maxExtraTimeSeconds"
	}
	if missing != "" {
		return domain.RouteSearchRequest{}, fmt.Errorf("required property %q is missing", missing)
	}
	request := domain.RouteSearchRequest{
		RequestID: input.RequestID, Origin: *input.Origin, Destination: *input.Destination,
		Waypoints: append([]domain.GeoPoint(nil), input.Waypoints...), DepartureTime: input.DepartureTime,
		RoutingMode: *input.RoutingMode, MaxExtraDistanceMeters: *input.MaxExtraDistanceMeters,
		MaxExtraTimeSeconds: *input.MaxExtraTimeSeconds, AvoidTolls: input.AvoidTolls, AvoidUnpaved: input.AvoidUnpaved,
		Strictness: .7, MaxProviderRequests: 8, SearchDeadlineMS: 10_000,
	}
	if input.MaxExtraDistancePercent != nil {
		request.MaxExtraDistancePercent = *input.MaxExtraDistancePercent
	}
	if input.Strictness != nil {
		request.Strictness = *input.Strictness
	}
	if input.MaxProviderRequests != nil {
		request.MaxProviderRequests = *input.MaxProviderRequests
	}
	if input.SearchDeadlineMS != nil {
		request.SearchDeadlineMS = *input.SearchDeadlineMS
	}
	return request, nil
}
