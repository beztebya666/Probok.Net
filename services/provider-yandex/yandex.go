package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/greenroute/greenroute/internal/contracts"
	"github.com/greenroute/greenroute/internal/domain"
	routegeometry "github.com/greenroute/greenroute/internal/geometry"
)

const (
	yandexRouterAPIVersion = "v2"
	yandexRouterEndpoint   = "https://api.routing.yandex.net/" + yandexRouterAPIVersion + "/route"
)

type yandexAdapter struct {
	cfg             Config
	client          *http.Client
	breaker         *circuitBreaker
	geocoderBreaker *circuitBreaker
	bulkhead        *bulkhead
	cooldown        *sharedCooldown
	credentialFault *credentialFaultLatch
	metrics         *serviceMetrics
	now             func() time.Time
	sleep           func(context.Context, time.Duration) error
	cooldownJitter  func(time.Duration) time.Duration
}

func newYandexAdapter(cfg Config, client *http.Client, metrics *serviceMetrics) *yandexAdapter {
	if client == nil {
		client = cfg.HTTPClient()
	}
	return &yandexAdapter{
		cfg: cfg, client: client, metrics: metrics,
		breaker:         newCircuitBreaker(cfg.CircuitBreakerThreshold, cfg.CircuitBreakerOpenDuration),
		geocoderBreaker: newCircuitBreaker(cfg.CircuitBreakerThreshold, cfg.CircuitBreakerOpenDuration),
		bulkhead:        newBulkhead(cfg.MaxConcurrency, cfg.BulkheadWaitTimeout),
		cooldown:        &sharedCooldown{},
		credentialFault: &credentialFaultLatch{},
		now:             time.Now, sleep: sleepContext, cooldownJitter: defaultCooldownJitter,
	}
}

func (a *yandexAdapter) waitForProviderCooldown(ctx context.Context) error {
	delay := a.cooldown.WaitDuration(a.now(), a.cooldownJitter)
	if delay <= 0 {
		return nil
	}
	return a.sleep(ctx, delay)
}

func (a *yandexAdapter) Routes(ctx context.Context, request contracts.ProviderRouteRequest) (contracts.ProviderRouteResponse, error) {
	budget := newRequestBudget(request.RequestBudget)
	var lastErr error

	for attempt := 0; attempt <= a.cfg.MaxRetries; attempt++ {
		if budget.Remaining() == 0 {
			a.metrics.budgetExhausted.Add(1)
			return contracts.ProviderRouteResponse{}, serviceError(
				"PROVIDER_BUDGET_EXHAUSTED",
				"provider request budget exhausted before a retry could complete",
				http.StatusTooManyRequests,
				false,
				errBudgetExhausted,
			)
		}
		if err := a.waitForProviderCooldown(ctx); err != nil {
			return contracts.ProviderRouteResponse{}, err
		}
		if !a.breaker.Allow() {
			a.metrics.circuitRejected.Add(1)
			a.metrics.breakerState.Store(int64(a.breaker.State()))
			return contracts.ProviderRouteResponse{}, errCircuitOpen
		}
		if err := a.bulkhead.Acquire(ctx); err != nil {
			if errors.Is(err, errBulkheadFull) {
				a.metrics.bulkheadRejected.Add(1)
			}
			return contracts.ProviderRouteResponse{}, err
		}
		if !budget.Consume() {
			a.bulkhead.Release()
			a.metrics.budgetExhausted.Add(1)
			return contracts.ProviderRouteResponse{}, errBudgetExhausted
		}

		a.metrics.inFlight.Add(1)
		a.metrics.providerRequests.Add(1)
		started := time.Now()
		response, err := a.doRoute(ctx, request)
		a.metrics.inFlight.Add(-1)
		a.bulkhead.Release()
		a.metrics.observeProvider(time.Since(started), err, len(response.Candidates))

		if err == nil {
			a.credentialFault.Success(credentialRouter)
			a.breaker.Success()
			a.metrics.breakerState.Store(int64(breakerClosed))
			a.metrics.addBillableUnits(len(response.Candidates), a.cfg.ProviderCostPerBillableUnit)
			response.RequestsUsed = budget.Used()
			response.BudgetRemaining = budget.Remaining()
			response.EstimatedBillableUnits = len(response.Candidates)
			for i := range response.Candidates {
				response.Candidates[i].ProviderRequestCount = budget.Used()
			}
			return response, nil
		}

		lastErr = err
		providerErr := normalizeProviderError(err)
		if providerErr.Code == "PROVIDER_RATE_LIMITED" {
			a.metrics.provider429.Add(1)
			a.cooldown.Extend(a.now(), providerCooldownDelay(attempt, a.cfg.RetryBaseDelay, a.cfg.RetryMaxDelay, providerErr.RetryAfter))
		} else if providerErr.Code == "PROVIDER_AUTHENTICATION_FAILED" {
			a.credentialFault.Fail(credentialRouter)
		} else if providerErr.Retryable {
			a.breaker.Failure()
		}
		a.metrics.breakerState.Store(int64(a.breaker.State()))

		if !providerErr.Retryable || attempt == a.cfg.MaxRetries {
			return contracts.ProviderRouteResponse{}, err
		}
		if budget.Remaining() == 0 {
			a.metrics.budgetExhausted.Add(1)
			return contracts.ProviderRouteResponse{}, serviceError(
				"PROVIDER_BUDGET_EXHAUSTED",
				"provider request budget exhausted before a retry could complete",
				http.StatusTooManyRequests,
				false,
				errBudgetExhausted,
			)
		}
		if providerErr.Code == "PROVIDER_RATE_LIMITED" {
			// The next loop iteration enters the adapter-wide gate. Keeping the
			// wait there also throttles callers that did not receive this 429.
			continue
		}
		delay := retryDelay(attempt, a.cfg.RetryBaseDelay, a.cfg.RetryMaxDelay, 0)
		if err := a.sleep(ctx, delay); err != nil {
			return contracts.ProviderRouteResponse{}, err
		}
	}
	return contracts.ProviderRouteResponse{}, lastErr
}

func (a *yandexAdapter) doRoute(ctx context.Context, request contracts.ProviderRouteRequest) (contracts.ProviderRouteResponse, error) {
	endpoint, err := buildYandexRouteURL(a.cfg, request)
	if err != nil {
		return contracts.ProviderRouteResponse{}, serviceError("PROVIDER_REQUEST_INVALID", "provider request could not be constructed", http.StatusBadRequest, false, err)
	}
	attemptCtx, cancel := context.WithTimeout(ctx, a.cfg.RequestTimeout)
	defer cancel()

	httpRequest, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return contracts.ProviderRouteResponse{}, serviceError("PROVIDER_REQUEST_INVALID", "provider request could not be constructed", http.StatusInternalServerError, false, err)
	}
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("User-Agent", "ProbokNet-provider-yandex/1.0")

	httpResponse, err := a.client.Do(httpRequest)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return contracts.ProviderRouteResponse{}, callerContextError(ctxErr)
		}
		return contracts.ProviderRouteResponse{}, serviceError("PROVIDER_NETWORK_ERROR", "provider connection failed", http.StatusBadGateway, true, err)
	}
	defer func() { _ = httpResponse.Body.Close() }()

	body, err := readBounded(httpResponse.Body, a.cfg.MaxProviderResponseBytes)
	if err != nil {
		return contracts.ProviderRouteResponse{}, serviceError("PROVIDER_RESPONSE_INVALID", "provider response exceeded the safe size limit", http.StatusBadGateway, false, err)
	}
	if httpResponse.StatusCode != http.StatusOK {
		return contracts.ProviderRouteResponse{}, mapYandexStatus(httpResponse.StatusCode, httpResponse.Header.Get("Retry-After"), a.now())
	}

	var wire yandexRouteResponse
	if err := json.Unmarshal(body, &wire); err != nil {
		return contracts.ProviderRouteResponse{}, serviceError("PROVIDER_RESPONSE_INVALID", "provider returned an invalid response", http.StatusBadGateway, false, err)
	}
	return normalizeYandexRoutes(request, wire)
}

func buildYandexRouteURL(cfg Config, request contracts.ProviderRouteRequest) (string, error) {
	endpoint, err := url.Parse(yandexRouterEndpoint)
	if err != nil {
		return "", err
	}
	if endpoint.Scheme != "https" || endpoint.Hostname() != "api.routing.yandex.net" || endpoint.Path != "/"+yandexRouterAPIVersion+"/route" {
		return "", errors.New("unsafe provider endpoint")
	}

	points := make([]domain.GeoPoint, 0, len(request.Waypoints)+2)
	points = append(points, request.Origin)
	points = append(points, request.Waypoints...)
	points = append(points, request.Destination)
	waypoints := make([]string, 0, len(points))
	for _, point := range points {
		waypoints = append(waypoints, formatCoordinate(point.Latitude)+","+formatCoordinate(point.Longitude))
	}

	query := endpoint.Query()
	query.Set("apikey", cfg.YandexAPIKey)
	query.Set("waypoints", strings.Join(waypoints, "|"))
	query.Set("mode", "driving")
	results := request.Alternatives + 1
	if results < 1 {
		results = 1
	}
	if results > cfg.YandexMaxResults {
		results = cfg.YandexMaxResults
	}
	query.Set("results", strconv.Itoa(results))
	if !request.Traffic {
		query.Set("traffic", "disabled")
	}
	if request.DepartureUnix > 0 && request.Traffic {
		query.Set("departure_time", strconv.FormatInt(request.DepartureUnix, 10))
	}
	if request.AvoidTolls {
		query.Set("avoid_tolls", "true")
	}
	if request.AvoidUnpaved {
		query.Set("avoid_unpaved", "true")
	}
	for _, zone := range request.AvoidZones {
		zonePoints := make([]string, 0, len(zone.Points))
		for _, point := range zone.Points {
			zonePoints = append(zonePoints, formatCoordinate(point.Latitude)+","+formatCoordinate(point.Longitude))
		}
		query.Add("avoid_zones", strings.Join(zonePoints, "|"))
	}
	endpoint.RawQuery = query.Encode()
	return endpoint.String(), nil
}

func formatCoordinate(value float64) string {
	return strconv.FormatFloat(value, 'f', 7, 64)
}

func readBounded(reader io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("response too large")
	}
	return data, nil
}

func mapYandexStatus(status int, retryAfter string, now time.Time) error {
	switch status {
	case http.StatusBadRequest:
		return serviceError("PROVIDER_REJECTED_REQUEST", "provider rejected the normalized request", http.StatusBadGateway, false, nil)
	case http.StatusUnauthorized, http.StatusForbidden:
		return serviceError("PROVIDER_AUTHENTICATION_FAILED", "provider credentials are invalid or lack access", http.StatusServiceUnavailable, false, nil)
	case http.StatusTooManyRequests:
		err := serviceError("PROVIDER_RATE_LIMITED", "provider rate limit reached", http.StatusTooManyRequests, true, nil)
		err.RetryAfter = parseRetryAfter(retryAfter, now)
		return err
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return serviceError("PROVIDER_UNAVAILABLE", "provider is temporarily unavailable", http.StatusBadGateway, true, nil)
	default:
		if status >= 500 && status <= 599 {
			return serviceError("PROVIDER_UNAVAILABLE", "provider is temporarily unavailable", http.StatusBadGateway, true, nil)
		}
		return serviceError("PROVIDER_UNEXPECTED_STATUS", "provider returned an unexpected status", http.StatusBadGateway, false, nil)
	}
}

func callerContextError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return serviceError("REQUEST_DEADLINE_EXCEEDED", "request deadline was exceeded", http.StatusGatewayTimeout, false, err)
	}
	return serviceError("REQUEST_CANCELLED", "request was cancelled", 499, false, err)
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds <= 0 {
			return 0
		}
		if seconds >= int64(maxProviderCooldown/time.Second) {
			return maxProviderCooldown
		}
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil && when.After(now) {
		delay := when.Sub(now)
		if delay > maxProviderCooldown {
			return maxProviderCooldown
		}
		return delay
	}
	return 0
}

type yandexRouteResponse struct {
	TrafficType string        `json:"traffic_type"`
	Route       *yandexRoute  `json:"route"`
	Routes      []yandexRoute `json:"routes"`
}

type yandexRoute struct {
	Legs  []yandexLeg `json:"legs"`
	Flags struct {
		HasTolls                 bool `json:"hasTolls"`
		HasNonTransactionalTolls bool `json:"hasNonTransactionalTolls"`
	} `json:"flags"`
}

type yandexLeg struct {
	Status string       `json:"status"`
	Steps  []yandexStep `json:"steps"`
}

type yandexStep struct {
	Duration float64 `json:"duration"`
	Length   float64 `json:"length"`
	Mode     string  `json:"mode"`
	Polyline struct {
		Points [][]float64 `json:"points"`
	} `json:"polyline"`
}

const (
	maxProviderRoutes         = 3
	maxProviderLegsPerRoute   = 64
	maxProviderStepsPerRoute  = 2_000
	maxProviderPointsPerRoute = 4_096
)

func normalizeYandexRoutes(request contracts.ProviderRouteRequest, response yandexRouteResponse) (contracts.ProviderRouteResponse, error) {
	routes := response.Routes
	if len(routes) == 0 && response.Route != nil {
		routes = []yandexRoute{*response.Route}
	}
	if len(routes) == 0 {
		return contracts.ProviderRouteResponse{}, serviceError("PROVIDER_NO_ROUTE", "provider returned no route", http.StatusUnprocessableEntity, false, nil)
	}
	maximumExpected := min(1+request.Alternatives, maxProviderRoutes)
	if len(routes) > maximumExpected {
		return contracts.ProviderRouteResponse{}, serviceError("PROVIDER_RESPONSE_INVALID", "provider returned too many routes", http.StatusBadGateway, false, nil)
	}

	candidates := make([]domain.RouteCandidate, 0, len(routes))
	for routeIndex, route := range routes {
		candidate, err := normalizeYandexRoute(request, response.TrafficType, routeIndex, route)
		if err != nil {
			return contracts.ProviderRouteResponse{}, err
		}
		candidates = append(candidates, candidate)
	}

	warnings := []string{
		"Yandex traffic_type describes the traffic model used; the API does not expose an official per-segment congestion class.",
		fmt.Sprintf("Estimated billable response units: %d; the active commercial contract is authoritative.", len(candidates)),
	}
	return contracts.ProviderRouteResponse{Candidates: candidates, Warnings: warnings}, nil
}

func normalizeYandexRoute(request contracts.ProviderRouteRequest, trafficType string, routeIndex int, route yandexRoute) (domain.RouteCandidate, error) {
	if len(route.Legs) == 0 || len(route.Legs) > maxProviderLegsPerRoute {
		return domain.RouteCandidate{}, serviceError("PROVIDER_RESPONSE_INVALID", "provider returned an invalid leg count", http.StatusBadGateway, false, nil)
	}
	var distance float64
	var duration float64
	var blocked bool
	geometry := make([]domain.GeoPoint, 0, 128)
	segments := make([]domain.RouteSegment, 0, 32)
	totalSteps, totalPoints := 0, 0

	for legIndex, leg := range route.Legs {
		if leg.Status != "OK" {
			blocked = true
		}
		for stepIndex, step := range leg.Steps {
			totalSteps++
			totalPoints += len(step.Polyline.Points)
			if totalSteps > maxProviderStepsPerRoute || totalPoints > maxProviderPointsPerRoute {
				return domain.RouteCandidate{}, serviceError("PROVIDER_RESPONSE_INVALID", "provider route geometry exceeds safety limits", http.StatusBadGateway, false, nil)
			}
			if !finitePositive(step.Length) || !finitePositive(step.Duration) {
				return domain.RouteCandidate{}, serviceError("PROVIDER_RESPONSE_INVALID", "provider returned invalid route measurements", http.StatusBadGateway, false, nil)
			}
			stepGeometry, err := normalizePolyline(step.Polyline.Points)
			if err != nil {
				return domain.RouteCandidate{}, err
			}
			if routegeometry.PolylineLength(stepGeometry) > step.Length*2+5_000 {
				return domain.RouteCandidate{}, serviceError("PROVIDER_RESPONSE_INVALID", "provider geometry is inconsistent with route length", http.StatusBadGateway, false, nil)
			}
			geometry = appendDistinct(geometry, stepGeometry)
			distance += step.Length
			duration += step.Duration

			segment := domain.RouteSegment{
				SegmentID:          fmt.Sprintf("yandex-%d-%d-%d", routeIndex+1, legIndex+1, stepIndex+1),
				Geometry:           stepGeometry,
				DistanceMeters:     int64(math.Round(step.Length)),
				CongestionClass:    domain.CongestionUnknown,
				GeometrySimilarity: 1,
				Source:             "YANDEX_ROUTER_API",
				Confidence: domain.Confidence{
					Level: domain.ConfidenceLow, Score: 0.5,
					Reasons: []string{"PROVIDER_HAS_NO_OFFICIAL_SEGMENT_CONGESTION_CLASS"},
				},
			}
			if trafficType == "disabled" || !request.Traffic {
				segment.BaselineDurationSeconds = int64(math.Round(step.Duration))
			} else {
				segment.LiveDurationSeconds = int64(math.Round(step.Duration))
			}
			segments = append(segments, segment)
		}
	}
	if len(geometry) < 2 || distance <= 0 || duration <= 0 {
		return domain.RouteCandidate{}, serviceError("PROVIDER_RESPONSE_INVALID", "provider returned an incomplete route", http.StatusBadGateway, false, nil)
	}

	distanceMeters := int64(math.Round(distance))
	durationSeconds := int64(math.Round(duration))
	reasonCodes := []string{"PROVIDER_ROUTE_DETAILS", "SEGMENT_CONGESTION_UNKNOWN"}
	confidence := domain.Confidence{Level: domain.ConfidenceMedium, Score: 0.65, Reasons: []string{"OFFICIAL_PROVIDER_ROUTE", "NO_BASELINE_LIVE_PAIR"}}
	if trafficType == "disabled" || !request.Traffic {
		reasonCodes = append(reasonCodes, "TRAFFIC_DISABLED_BASELINE")
	} else if trafficType == "realtime" {
		reasonCodes = append(reasonCodes, "YANDEX_TRAFFIC_REALTIME")
	} else if trafficType == "forecast" {
		reasonCodes = append(reasonCodes, "YANDEX_TRAFFIC_FORECAST")
	} else {
		reasonCodes = append(reasonCodes, "YANDEX_TRAFFIC_TYPE_UNKNOWN")
		confidence.Level = domain.ConfidenceLow
		confidence.Score = 0.5
	}

	candidate := domain.RouteCandidate{
		CandidateID:    candidateID("yandex", routeIndex, geometry, distanceMeters, durationSeconds),
		Provider:       "yandex",
		Geometry:       geometry,
		DistanceMeters: distanceMeters,
		Segments:       segments,
		Blocked:        blocked,
		Tolls:          route.Flags.HasTolls || route.Flags.HasNonTransactionalTolls,
		Confidence:     confidence,
		ReasonCodes:    reasonCodes,
		Explanation:    "Маршрут нормализован из официального Yandex Router API; уровень пробок по сегментам остаётся UNKNOWN до baseline-сопоставления.",
		GeneratedBy:    "INITIAL_PROVIDER",
	}
	if trafficType == "disabled" || !request.Traffic {
		candidate.TrafficDataType = domain.TrafficDataBaseline
		candidate.BaselineDurationSeconds = durationSeconds
	} else {
		switch trafficType {
		case "realtime":
			candidate.TrafficDataType = domain.TrafficDataRealtime
		case "forecast":
			candidate.TrafficDataType = domain.TrafficDataForecast
		default:
			candidate.TrafficDataType = domain.TrafficDataUnknown
		}
		candidate.LiveDurationSeconds = durationSeconds
	}
	return candidate, nil
}

func finitePositive(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func normalizePolyline(points [][]float64) ([]domain.GeoPoint, error) {
	geometry := make([]domain.GeoPoint, 0, len(points))
	for _, pair := range points {
		if len(pair) != 2 {
			return nil, serviceError("PROVIDER_RESPONSE_INVALID", "provider returned malformed geometry", http.StatusBadGateway, false, nil)
		}
		point := domain.GeoPoint{Latitude: pair[0], Longitude: pair[1]}
		if err := point.Validate(); err != nil {
			return nil, serviceError("PROVIDER_RESPONSE_INVALID", "provider returned out-of-range geometry", http.StatusBadGateway, false, err)
		}
		geometry = appendDistinct(geometry, []domain.GeoPoint{point})
	}
	if len(geometry) < 2 {
		return nil, serviceError("PROVIDER_RESPONSE_INVALID", "provider returned insufficient geometry", http.StatusBadGateway, false, nil)
	}
	return geometry, nil
}

func appendDistinct(target, points []domain.GeoPoint) []domain.GeoPoint {
	for _, point := range points {
		if len(target) == 0 || target[len(target)-1] != point {
			target = append(target, point)
		}
	}
	return target
}

func candidateID(provider string, index int, geometry []domain.GeoPoint, distance, duration int64) string {
	hash := sha256.New()
	_, _ = fmt.Fprintf(hash, "%s|%d|%d|%d|", provider, index, distance, duration)
	for _, point := range geometry {
		_, _ = fmt.Fprintf(hash, "%.7f,%.7f|", point.Latitude, point.Longitude)
	}
	return provider + "-" + hex.EncodeToString(hash.Sum(nil)[:12])
}

func (a *yandexAdapter) Capabilities() capabilityDocument {
	return officialCapabilities(a.cfg, providerModeYandex)
}

func (a *yandexAdapter) Ready() error {
	if a.cfg.YandexAPIKey == "" {
		return errors.New("provider credentials are not configured")
	}
	if a.credentialFault.Failed() {
		return errCredentialFault
	}
	if a.breaker.State() == breakerOpen {
		return errCircuitOpen
	}
	return nil
}

func officialCapabilities(cfg Config, mode string) capabilityDocument {
	return capabilityDocument{
		ProviderCapabilities: contracts.ProviderCapabilities{
			ContractVersion: contracts.InternalContractVersion,
			Provider:        "yandex", Mode: mode, MaxAlternatives: cfg.YandexMaxResults - 1,
			APIIntegrations: []contracts.APIIntegration{
				{
					ID: "yandex-route-details", Provider: "yandex", Product: "Route Details API", APIVersion: yandexRouterAPIVersion,
					Capability: apiCapabilityRouting, Role: apiRolePrimary, State: apiStateActive,
				},
				{
					ID: "yandex-http-geocoder", Provider: "yandex", Product: "HTTP Geocoder API", APIVersion: yandexGeocoderAPIVersion,
					Capability: apiCapabilityAddressSearch, Role: apiRolePrimary, State: apiStateActive,
				},
			},
			MaxWaypoints: 50, RealtimeTraffic: true, TrafficDisabledBaseline: true,
			DepartureTime: true, AvoidZones: true, Geosuggest: true,
			Coverage:            []string{"Россия", "Абхазия", "Турция", "Азербайджан", "Армения", "Казахстан", "Кыргызстан", "Таджикистан", "Беларусь", "Грузия", "Узбекистан", "Молдова"},
			ExperimentalSources: false, DataStorageAllowed: cfg.ProviderDataStorageAllowed,
			RawResponseCacheTTLSecond: 0,
		},
		VerifiedAt: "2026-08-05",
		OfficialDocumentation: []string{
			"https://yandex.ru/maps-api/docs/router-api/request.html",
			"https://yandex.ru/maps-api/docs/router-api/response.html",
			"https://yandex.ru/dev/tariffs/doc/ru/router/terms/",
			"https://yandex.ru/dev/tariffs/doc/ru/router/prices/",
			yandexGeocoderRequestDocumentation,
			yandexGeocoderResponseDocumentation,
		},
		OfficialEndpoint:            yandexRouterEndpoint,
		AddressSearchProvider:       addressProviderYandex,
		AddressSearchEndpoint:       yandexGeocoderEndpoint,
		MaxRoutesPerRequest:         cfg.YandexMaxResults,
		RequestsPerSecond:           50,
		DailyRequestLimit:           nil,
		DailyLimitContractDependent: true,
		AvoidTolls:                  true,
		AvoidUnpaved:                "paid-plan capability",
		Billing: billingCapability{
			Unit: "provider response", MultipleRoutesCount: "each response is billed when one request returns multiple responses",
		},
		Storage: storageCapability{
			Standard: "cache up to 30 days solely to improve the licensed resource performance",
			Extended: "store during the offer/license term; exact contract controls",
		},
		Licenses: licenseCapability{
			BasicName:    "Стандартная (official current name; Basic is not used in current Russian terms)",
			AdvancedName: "Расширенная", FreeTier: "not available for Route Details API; 7-day trial may be available under published terms",
		},
		Limitations: []string{
			"No official per-segment congestion class or jam score in the documented response.",
			"Live and traffic-disabled baseline durations require separate requests and geometry matching.",
			"The documented API does not expose a stable provider route reference.",
			"Address search uses the separately licensed HTTP Geocoder rather than Geosuggest because the neutral contract requires coordinates.",
			"Geocoder key entitlement is contract-dependent; a dedicated YANDEX_GEOCODER_API_KEY is preferred.",
			"The documented Router limit of 50 requests per second does not establish a Geocoder rate limit; Geocoder quota and rate are contract-dependent.",
			"Avoid-zone polygon count and point-count maxima are not documented (UNKNOWN).",
		},
		ExperimentalRequested:   cfg.ExperimentalSourcesRequested,
		DataModificationAllowed: cfg.ProviderDataModificationOK,
	}
}
