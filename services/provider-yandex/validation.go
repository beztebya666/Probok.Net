package main

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/greenroute/greenroute/internal/contracts"
	"github.com/greenroute/greenroute/internal/domain"
)

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

func validateRouteRequest(request contracts.ProviderRouteRequest, maxAlternatives int, now time.Time) error {
	if !requestIDPattern.MatchString(request.RequestID) {
		return fmt.Errorf("requestId must be 1-128 safe identifier characters")
	}
	if err := request.Origin.Validate(); err != nil {
		return fmt.Errorf("origin: %w", err)
	}
	if err := request.Destination.Validate(); err != nil {
		return fmt.Errorf("destination: %w", err)
	}
	if request.Origin == request.Destination {
		return fmt.Errorf("origin and destination must differ")
	}
	if len(request.Waypoints) > 48 {
		return fmt.Errorf("at most 48 intermediate waypoints are allowed")
	}
	for index, point := range request.Waypoints {
		if err := point.Validate(); err != nil {
			return fmt.Errorf("waypoints[%d]: %w", index, err)
		}
	}
	if request.Alternatives < 0 || request.Alternatives > maxAlternatives {
		return fmt.Errorf("alternatives must be between 0 and %d", maxAlternatives)
	}
	if request.RequestBudget < 1 || request.RequestBudget > 20 {
		return fmt.Errorf("requestBudget must be between 1 and 20")
	}
	if request.DeadlineMS < 100 || request.DeadlineMS > 30_000 {
		return fmt.Errorf("deadlineMs must be between 100 and 30000")
	}
	if request.DepartureUnix > 0 {
		if !request.Traffic {
			return fmt.Errorf("departureUnix cannot be used when traffic is disabled")
		}
		departure := time.Unix(request.DepartureUnix, 0)
		if departure.Before(now.Add(-time.Minute)) {
			return fmt.Errorf("departureUnix cannot be in the past")
		}
	}
	if len(request.AvoidZones) > 8 {
		return fmt.Errorf("at most 8 avoid zones are accepted by the internal safety limit")
	}
	for zoneIndex, zone := range request.AvoidZones {
		if len(zone.Points) < 3 || len(zone.Points) > 32 {
			return fmt.Errorf("avoidZones[%d] must contain between 3 and 32 points", zoneIndex)
		}
		unique := make(map[domain.GeoPoint]struct{}, len(zone.Points))
		for pointIndex, point := range zone.Points {
			if err := point.Validate(); err != nil {
				return fmt.Errorf("avoidZones[%d].points[%d]: %w", zoneIndex, pointIndex, err)
			}
			unique[point] = struct{}{}
		}
		if len(unique) < 3 {
			return fmt.Errorf("avoidZones[%d] needs at least 3 distinct points", zoneIndex)
		}
	}
	return nil
}

func validateSuggestQuery(query, language string, limit int) error {
	query = strings.TrimSpace(query)
	if query == "" {
		return fmt.Errorf("q is required")
	}
	if len([]rune(query)) > 256 {
		return fmt.Errorf("q must not exceed 256 characters")
	}
	if limit < 1 || limit > 10 {
		return fmt.Errorf("limit must be between 1 and 10")
	}
	switch language {
	case "ru_RU", "uk_UA", "be_BY", "en_RU", "en_US", "tr_TR":
	default:
		return fmt.Errorf("lang is not supported")
	}
	return nil
}
