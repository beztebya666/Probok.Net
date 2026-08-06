package domain

import (
	"errors"
	"fmt"
	"math"
	"time"
)

var (
	ErrInvalidRequest = errors.New("invalid route search request")
	ErrInvalidRoute   = errors.New("invalid route candidate")
)

type ValidationLimits struct {
	MaxWaypoints            int
	MaxExtraDistanceMeters  int64
	MaxExtraDistancePercent float64
	MaxExtraTimeSeconds     int64
	MaxProviderRequests     int
	MinSearchDeadlineMS     int
	MaxSearchDeadlineMS     int
}

func DefaultValidationLimits() ValidationLimits {
	return ValidationLimits{
		MaxWaypoints: 16, MaxExtraDistanceMeters: 150_000,
		MaxExtraDistancePercent: 300, MaxExtraTimeSeconds: 18_000,
		MaxProviderRequests: 20, MinSearchDeadlineMS: 500, MaxSearchDeadlineMS: 30_000,
	}
}

func (p GeoPoint) Validate() error {
	if math.IsNaN(p.Latitude) || math.IsInf(p.Latitude, 0) || p.Latitude < -90 || p.Latitude > 90 {
		return fmt.Errorf("%w: latitude must be between -90 and 90", ErrInvalidRequest)
	}
	if math.IsNaN(p.Longitude) || math.IsInf(p.Longitude, 0) || p.Longitude < -180 || p.Longitude > 180 {
		return fmt.Errorf("%w: longitude must be between -180 and 180", ErrInvalidRequest)
	}
	return nil
}

func (r *RouteSearchRequest) ApplySafeDefaults() {
	if r.MaxProviderRequests == 0 {
		r.MaxProviderRequests = 8
	}
	if r.SearchDeadlineMS == 0 {
		r.SearchDeadlineMS = 10_000
	}
}

func (r RouteSearchRequest) Validate(limits ValidationLimits) error {
	if err := r.Origin.Validate(); err != nil {
		return fmt.Errorf("origin: %w", err)
	}
	if err := r.Destination.Validate(); err != nil {
		return fmt.Errorf("destination: %w", err)
	}
	if r.Origin == r.Destination {
		return fmt.Errorf("%w: origin and destination must differ", ErrInvalidRequest)
	}
	if !r.RoutingMode.Valid() {
		return fmt.Errorf("%w: unsupported routingMode %q", ErrInvalidRequest, r.RoutingMode)
	}
	if len(r.Waypoints) > limits.MaxWaypoints {
		return fmt.Errorf("%w: at most %d waypoints are allowed", ErrInvalidRequest, limits.MaxWaypoints)
	}
	for i, p := range r.Waypoints {
		if err := p.Validate(); err != nil {
			return fmt.Errorf("waypoints[%d]: %w", i, err)
		}
	}
	if r.MaxExtraDistanceMeters < 0 || r.MaxExtraDistanceMeters > limits.MaxExtraDistanceMeters {
		return fmt.Errorf("%w: maxExtraDistanceMeters out of range", ErrInvalidRequest)
	}
	if math.IsNaN(r.MaxExtraDistancePercent) || math.IsInf(r.MaxExtraDistancePercent, 0) || r.MaxExtraDistancePercent < 0 || r.MaxExtraDistancePercent > limits.MaxExtraDistancePercent {
		return fmt.Errorf("%w: maxExtraDistancePercent out of range", ErrInvalidRequest)
	}
	if r.MaxExtraTimeSeconds < 0 || r.MaxExtraTimeSeconds > limits.MaxExtraTimeSeconds {
		return fmt.Errorf("%w: maxExtraTimeSeconds out of range", ErrInvalidRequest)
	}
	if math.IsNaN(r.Strictness) || math.IsInf(r.Strictness, 0) || r.Strictness < 0 || r.Strictness > 1 {
		return fmt.Errorf("%w: strictness must be between 0 and 1", ErrInvalidRequest)
	}
	if r.MaxProviderRequests < 1 || r.MaxProviderRequests > limits.MaxProviderRequests {
		return fmt.Errorf("%w: maxProviderRequests must be between 1 and %d", ErrInvalidRequest, limits.MaxProviderRequests)
	}
	if r.SearchDeadlineMS < limits.MinSearchDeadlineMS || r.SearchDeadlineMS > limits.MaxSearchDeadlineMS {
		return fmt.Errorf("%w: searchDeadlineMs must be between %d and %d", ErrInvalidRequest, limits.MinSearchDeadlineMS, limits.MaxSearchDeadlineMS)
	}
	if r.DepartureTime != nil && r.DepartureTime.Before(time.Now().Add(-5*time.Minute)) {
		return fmt.Errorf("%w: departureTime cannot be in the past", ErrInvalidRequest)
	}
	return nil
}

func (r RouteCandidate) Validate() error {
	if r.CandidateID == "" {
		return fmt.Errorf("%w: candidateId is required", ErrInvalidRoute)
	}
	if len(r.Geometry) < 2 {
		return fmt.Errorf("%w: geometry needs at least two points", ErrInvalidRoute)
	}
	for i, point := range r.Geometry {
		if err := point.Validate(); err != nil {
			return fmt.Errorf("%w: geometry[%d]: %v", ErrInvalidRoute, i, err)
		}
	}
	if r.DistanceMeters <= 0 || (r.LiveDurationSeconds <= 0 && r.BaselineDurationSeconds <= 0) {
		return fmt.Errorf("%w: positive distance and at least one duration are required", ErrInvalidRoute)
	}
	if math.IsNaN(r.Confidence.Score) || math.IsInf(r.Confidence.Score, 0) || r.Confidence.Score < 0 || r.Confidence.Score > 1 {
		return fmt.Errorf("%w: confidence score out of range", ErrInvalidRoute)
	}
	return nil
}
