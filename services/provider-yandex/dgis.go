package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	dgisRoutingAPIVersion   = "7.0.0"
	dgisRoutingEndpoint     = "https://routing.api.2gis.com/routing/" + dgisRoutingAPIVersion + "/global"
	maxDGISRoutePoints      = 10
	maxDGISManeuvers        = 2_000
	maxDGISGeometryParts    = 4_096
	maxDGISWKTBytes         = 1 << 20
	maxDGISTotalRouteMeters = 10_000_000
	maxDGISTotalDuration    = 14 * 24 * 60 * 60
)

// dgisAdapter uses only documented, public 2GIS cloud endpoints. It deliberately
// ignores the response geometry color field: 2GIS documents that detailed
// congestion levels are not part of the Routing API contract.
type dgisAdapter struct {
	cfg                     Config
	client                  *http.Client
	breaker                 *circuitBreaker
	bulkhead                *bulkhead
	cooldown                *sharedCooldown
	rateGate                *slidingWindowGate
	credentialFault         *credentialFaultLatch
	metrics                 *serviceMetrics
	now                     func() time.Time
	sleep                   func(context.Context, time.Duration) error
	cooldownJitter          func(time.Duration) time.Duration
	addressResolver         addressResolver
	addressProvider         string
	addressEndpoint         string
	addressDocs             []string
	addressFallbackProvider string
	addressFallbackDocs     []string
}

type addressResolver interface {
	Suggest(context.Context, string, string, int) (contracts.GeosuggestResponse, error)
	Ready() error
}

type fallbackAddressResolver struct {
	primary  addressResolver
	fallback addressResolver
	metrics  *serviceMetrics
}

func (r *fallbackAddressResolver) Suggest(ctx context.Context, query, language string, limit int) (contracts.GeosuggestResponse, error) {
	primaryResult := contracts.GeosuggestResponse{}
	primaryErr := r.primary.Ready()
	if primaryErr == nil {
		primaryResult, primaryErr = r.primary.Suggest(ctx, query, language, limit)
	}
	if primaryErr == nil && len(primaryResult.Suggestions) > 0 {
		return primaryResult, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return primaryResult, ctxErr
	}
	if r.metrics != nil {
		if primaryErr != nil {
			r.metrics.geocoderFallbackError.Add(1)
		} else {
			r.metrics.geocoderFallbackEmpty.Add(1)
		}
	}
	fallbackResult, fallbackErr := r.fallback.Suggest(ctx, query, language, limit)
	if fallbackErr == nil {
		return fallbackResult, nil
	}
	// An empty successful primary response remains a valid answer. A failure in
	// the opportunistic fallback must not turn that answer into a 5xx response.
	if primaryErr == nil {
		return primaryResult, nil
	}
	return contracts.GeosuggestResponse{}, fallbackErr
}

func (r *fallbackAddressResolver) Ready() error {
	primaryErr := r.primary.Ready()
	if primaryErr == nil {
		return nil
	}
	if fallbackErr := r.fallback.Ready(); fallbackErr != nil {
		return fmt.Errorf("primary and fallback address resolvers are unavailable: primary: %v; fallback: %w", primaryErr, fallbackErr)
	}
	return nil
}

func newDGISAdapter(cfg Config, client *http.Client, metrics *serviceMetrics) *dgisAdapter {
	if client == nil {
		client = cfg.HTTPClient()
	}
	adapter := &dgisAdapter{
		cfg: cfg, client: client, metrics: metrics,
		breaker:         newCircuitBreaker(cfg.CircuitBreakerThreshold, cfg.CircuitBreakerOpenDuration),
		bulkhead:        newBulkhead(cfg.MaxConcurrency, cfg.BulkheadWaitTimeout),
		cooldown:        &sharedCooldown{},
		rateGate:        newSlidingWindowGate(cfg.DGISRateLimitPerMinute, time.Minute),
		credentialFault: &credentialFaultLatch{},
		now:             time.Now,
		sleep:           sleepContext,
		cooldownJitter:  defaultCooldownJitter,
	}
	switch resolveAddressProvider(cfg) {
	case addressProviderYandex:
		resolver := newYandexAdapter(cfg, client, metrics)
		yandexResolver := &yandexAddressResolver{adapter: resolver}
		adapter.addressResolver = yandexResolver
		adapter.addressProvider = addressProviderYandex
		adapter.addressEndpoint = yandexGeocoderEndpoint
		adapter.addressDocs = []string{yandexGeocoderRequestDocumentation, yandexGeocoderResponseDocumentation}
		if cfg.AddressProviderMode == addressProviderAuto {
			dgisResolver := newDGISGeocoder(cfg, client, metrics, adapter.bulkhead, adapter.cooldown, adapter.credentialFault)
			adapter.addressResolver = &fallbackAddressResolver{primary: yandexResolver, fallback: dgisResolver, metrics: metrics}
			adapter.addressFallbackProvider = addressProviderDGIS
			adapter.addressFallbackDocs = []string{dgisGeocoderDocumentation}
		}
	default:
		adapter.addressResolver = newDGISGeocoder(cfg, client, metrics, adapter.bulkhead, adapter.cooldown, adapter.credentialFault)
		adapter.addressProvider = addressProviderDGIS
		adapter.addressEndpoint = dgisGeocoderEndpoint
		adapter.addressDocs = []string{dgisGeocoderDocumentation}
	}
	return adapter
}

func resolveAddressProvider(cfg Config) string {
	if cfg.AddressProviderMode != addressProviderAuto {
		return cfg.AddressProviderMode
	}
	if cfg.ProviderMode == providerModeDGIS && cfg.YandexGeocoderAPIKey != "" {
		return addressProviderYandex
	}
	return addressProviderDGIS
}

func (a *dgisAdapter) waitForProviderCooldown(ctx context.Context) error {
	delay := a.cooldown.WaitDuration(a.now(), a.cooldownJitter)
	if delay <= 0 {
		return nil
	}
	return a.sleep(ctx, delay)
}

func (a *dgisAdapter) Routes(ctx context.Context, request contracts.ProviderRouteRequest) (contracts.ProviderRouteResponse, error) {
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
		if allowed, retryAfter := a.rateGate.Try(a.now()); !allowed {
			a.bulkhead.Release()
			a.metrics.localRateGateRejected.Add(1)
			err := serviceError("PROVIDER_RATE_LIMITED", "configured provider quota gate is temporarily full", http.StatusTooManyRequests, true, nil)
			err.RetryAfter = retryAfter
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
			// 2GIS documents that alternatives for one set of points are not
			// billed additionally. The Platform Manager remains authoritative.
			a.metrics.addBillableUnits(1, a.cfg.ProviderCostPerBillableUnit)
			response.RequestsUsed = budget.Used()
			response.BudgetRemaining = budget.Remaining()
			response.EstimatedBillableUnits = 1
			for index := range response.Candidates {
				response.Candidates[index].ProviderRequestCount = budget.Used()
			}
			return response, nil
		}

		lastErr = err
		providerErr := normalizeProviderError(err)
		switch {
		case providerErr.Code == "PROVIDER_RATE_LIMITED":
			a.metrics.provider429.Add(1)
			a.cooldown.Extend(a.now(), providerCooldownDelay(attempt, a.cfg.RetryBaseDelay, a.cfg.RetryMaxDelay, providerErr.RetryAfter))
		case isDGISCredentialAccessError(providerErr.Code):
			a.credentialFault.Fail(credentialRouter)
		case providerErr.Retryable:
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
			continue
		}
		if err := a.sleep(ctx, retryDelay(attempt, a.cfg.RetryBaseDelay, a.cfg.RetryMaxDelay, 0)); err != nil {
			return contracts.ProviderRouteResponse{}, err
		}
	}
	return contracts.ProviderRouteResponse{}, lastErr
}

func (a *dgisAdapter) doRoute(ctx context.Context, request contracts.ProviderRouteRequest) (contracts.ProviderRouteResponse, error) {
	endpoint, body, err := buildDGISRouteRequest(a.cfg, request)
	if err != nil {
		return contracts.ProviderRouteResponse{}, serviceError("PROVIDER_REQUEST_INVALID", "provider request could not be constructed", http.StatusBadRequest, false, err)
	}
	attemptCtx, cancel := context.WithTimeout(ctx, a.cfg.RequestTimeout)
	defer cancel()

	httpRequest, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return contracts.ProviderRouteResponse{}, serviceError("PROVIDER_REQUEST_INVALID", "provider request could not be constructed", http.StatusInternalServerError, false, err)
	}
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("User-Agent", "GreenRoute-provider/1.0")

	httpResponse, err := a.client.Do(httpRequest)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return contracts.ProviderRouteResponse{}, callerContextError(ctxErr)
		}
		return contracts.ProviderRouteResponse{}, serviceError("PROVIDER_NETWORK_ERROR", "provider connection failed", http.StatusBadGateway, true, err)
	}
	defer func() { _ = httpResponse.Body.Close() }()

	responseBody, err := readBounded(httpResponse.Body, a.cfg.MaxProviderResponseBytes)
	if err != nil {
		return contracts.ProviderRouteResponse{}, serviceError("PROVIDER_RESPONSE_INVALID", "provider response exceeded the safe size limit", http.StatusBadGateway, false, err)
	}
	if httpResponse.StatusCode != http.StatusOK {
		return contracts.ProviderRouteResponse{}, mapDGISStatus(httpResponse.StatusCode, httpResponse.Header.Get("Retry-After"), a.now())
	}

	var wire dgisRouteResponse
	if err := json.Unmarshal(responseBody, &wire); err != nil {
		return contracts.ProviderRouteResponse{}, serviceError("PROVIDER_RESPONSE_INVALID", "provider returned an invalid response", http.StatusBadGateway, false, err)
	}
	return normalizeDGISRoutes(request, wire)
}

type dgisRouteRequest struct {
	Points           []dgisRequestPoint `json:"points"`
	Transport        string             `json:"transport"`
	Output           string             `json:"output"`
	Locale           string             `json:"locale"`
	RouteMode        string             `json:"route_mode"`
	TrafficMode      string             `json:"traffic_mode,omitempty"`
	UTC              int64              `json:"utc,omitempty"`
	AllowLockedRoads bool               `json:"allow_locked_roads"`
	Filters          []string           `json:"filters,omitempty"`
	Exclude          []dgisExcludeArea  `json:"exclude,omitempty"`
	NeedPaymentInfo  bool               `json:"need_payment_info"`
	Alternative      int                `json:"alternative"`
}

type dgisRequestPoint struct {
	Type string  `json:"type,omitempty"`
	Lon  float64 `json:"lon"`
	Lat  float64 `json:"lat"`
}

type dgisExcludeArea struct {
	Type     string             `json:"type"`
	Severity string             `json:"severity"`
	Points   []dgisRequestPoint `json:"points"`
}

func buildDGISRouteRequest(cfg Config, request contracts.ProviderRouteRequest) (string, []byte, error) {
	endpoint, err := url.Parse(dgisRoutingEndpoint)
	if err != nil {
		return "", nil, err
	}
	if endpoint.Scheme != "https" || endpoint.Hostname() != "routing.api.2gis.com" || endpoint.Path != "/routing/"+dgisRoutingAPIVersion+"/global" {
		return "", nil, errors.New("unsafe provider endpoint")
	}
	query := endpoint.Query()
	query.Set("key", cfg.DGISAPIKey)
	endpoint.RawQuery = query.Encode()

	points := make([]domain.GeoPoint, 0, len(request.Waypoints)+2)
	points = append(points, request.Origin)
	points = append(points, request.Waypoints...)
	points = append(points, request.Destination)
	if len(points) < 2 || len(points) > maxDGISRoutePoints {
		return "", nil, fmt.Errorf("2GIS accepts between 2 and %d route points", maxDGISRoutePoints)
	}

	wire := dgisRouteRequest{
		Points:           make([]dgisRequestPoint, 0, len(points)),
		Transport:        "driving",
		Output:           "detailed",
		Locale:           "ru",
		RouteMode:        "fastest",
		AllowLockedRoads: false,
		NeedPaymentInfo:  true,
		Alternative:      min(request.Alternatives, cfg.DGISMaxResults-1),
	}
	for _, point := range points {
		wire.Points = append(wire.Points, dgisRequestPoint{Type: "stop", Lon: point.Longitude, Lat: point.Latitude})
	}

	if request.Traffic {
		if dgisForecastDeparture(request.DepartureUnix, time.Now()) {
			wire.TrafficMode = "statistics"
			wire.UTC = request.DepartureUnix
		} else {
			wire.TrafficMode = "jam"
		}
	} else {
		// The documented shortest mode ignores traffic. This is used as the
		// traffic-disabled comparison and may have different geometry.
		wire.RouteMode = "shortest"
	}
	if request.AvoidTolls {
		wire.Filters = append(wire.Filters, "toll_road")
	}
	if request.AvoidUnpaved {
		wire.Filters = append(wire.Filters, "dirt_road")
	}
	for _, zone := range request.AvoidZones {
		area := dgisExcludeArea{Type: "polygon", Severity: "hard", Points: make([]dgisRequestPoint, 0, len(zone.Points)+1)}
		for _, point := range zone.Points {
			area.Points = append(area.Points, dgisRequestPoint{Lon: point.Longitude, Lat: point.Latitude})
		}
		if len(area.Points) > 0 && (area.Points[0].Lon != area.Points[len(area.Points)-1].Lon || area.Points[0].Lat != area.Points[len(area.Points)-1].Lat) {
			area.Points = append(area.Points, area.Points[0])
		}
		wire.Exclude = append(wire.Exclude, area)
	}

	body, err := json.Marshal(wire)
	if err != nil {
		return "", nil, err
	}
	return endpoint.String(), body, nil
}

func mapDGISStatus(status int, retryAfter string, now time.Time) error {
	switch status {
	case http.StatusNoContent:
		return serviceError("PROVIDER_NO_ROUTE", "provider could not build a route", http.StatusUnprocessableEntity, false, nil)
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge:
		return serviceError("PROVIDER_REJECTED_REQUEST", "provider rejected the normalized request", http.StatusBadGateway, false, nil)
	case http.StatusUnauthorized:
		return serviceError("PROVIDER_AUTHENTICATION_FAILED", "2GIS rejected the API key", http.StatusServiceUnavailable, false, nil)
	case http.StatusPaymentRequired:
		return serviceError("PROVIDER_SUBSCRIPTION_REQUIRED", "2GIS Routing API subscription or paid quota access is required", http.StatusServiceUnavailable, false, nil)
	case http.StatusForbidden:
		return serviceError("PROVIDER_ACCESS_FORBIDDEN", "2GIS Routing API access is forbidden by key service access or restrictions", http.StatusServiceUnavailable, false, nil)
	case http.StatusUnprocessableEntity:
		return serviceError("PROVIDER_NO_ROUTE", "provider could not build a route", http.StatusUnprocessableEntity, false, nil)
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

func isDGISCredentialAccessError(code string) bool {
	switch code {
	case "PROVIDER_AUTHENTICATION_FAILED", "PROVIDER_SUBSCRIPTION_REQUIRED", "PROVIDER_ACCESS_FORBIDDEN":
		return true
	default:
		return false
	}
}

type dgisRouteResponse struct {
	Status  string          `json:"status"`
	Type    string          `json:"type"`
	Message json.RawMessage `json:"message"`
	Result  []dgisRoute     `json:"result"`
}

type dgisRoute struct {
	ID                  json.RawMessage `json:"id"`
	RouteID             string          `json:"route_id"`
	Algorithm           string          `json:"algorithm"`
	TotalDistance       float64         `json:"total_distance"`
	TotalDuration       float64         `json:"total_duration"`
	Reliability         *float64        `json:"reliability"`
	BeginPedestrianPath dgisSidePath    `json:"begin_pedestrian_path"`
	EndPedestrianPath   dgisSidePath    `json:"end_pedestrian_path"`
	Maneuvers           []dgisManeuver  `json:"maneuvers"`
	TotalPayments       json.RawMessage `json:"total_payments"`
}

type dgisSidePath struct {
	Geometry dgisSidePathGeometry `json:"geometry"`
}

type dgisSidePathGeometry struct {
	Selection string `json:"selection"`
}

type dgisManeuver struct {
	ID             json.RawMessage `json:"id"`
	OutcomingPath  *dgisPath       `json:"outcoming_path"`
	ManeuverType   string          `json:"type"`
	ManeuverIcon   string          `json:"icon"`
	ManeuverRemark string          `json:"comment"`
}

type dgisPath struct {
	Distance float64            `json:"distance"`
	Duration float64            `json:"duration"`
	Geometry []dgisGeometryPart `json:"geometry"`
}

type dgisGeometryPart struct {
	Selection           string  `json:"selection"`
	Color               string  `json:"color"`
	Length              float64 `json:"length"`
	Style               string  `json:"style"`
	PublicTransportLane bool    `json:"public_transport_lane"`
}

func normalizeDGISRoutes(request contracts.ProviderRouteRequest, response dgisRouteResponse) (contracts.ProviderRouteResponse, error) {
	if response.Status != "OK" || response.Type != "result" {
		switch response.Status {
		case "ROUTE_NOT_FOUND", "ROUTE_DOES_NOT_EXISTS", "POINT_EXCLUDED", "ATTRACT_FAIL":
			return contracts.ProviderRouteResponse{}, serviceError("PROVIDER_NO_ROUTE", "provider could not build a route", http.StatusUnprocessableEntity, false, nil)
		case "FAIL":
			return contracts.ProviderRouteResponse{}, serviceError("PROVIDER_UNAVAILABLE", "provider failed to build a route", http.StatusBadGateway, true, nil)
		default:
			return contracts.ProviderRouteResponse{}, serviceError("PROVIDER_RESPONSE_INVALID", "provider returned an invalid result status", http.StatusBadGateway, false, nil)
		}
	}
	if len(response.Result) == 0 {
		return contracts.ProviderRouteResponse{}, serviceError("PROVIDER_NO_ROUTE", "provider returned no route", http.StatusUnprocessableEntity, false, nil)
	}
	maximumExpected := min(1+request.Alternatives, maxProviderRoutes)
	if len(response.Result) > maximumExpected {
		return contracts.ProviderRouteResponse{}, serviceError("PROVIDER_RESPONSE_INVALID", "provider returned too many routes", http.StatusBadGateway, false, nil)
	}

	candidates := make([]domain.RouteCandidate, 0, len(response.Result))
	for routeIndex, route := range response.Result {
		candidate, err := normalizeDGISRoute(request, routeIndex, route)
		if err != nil {
			return contracts.ProviderRouteResponse{}, err
		}
		candidates = append(candidates, candidate)
	}
	warnings := []string{
		"Estimated billable units: 1 for one point set; the active 2GIS subscription and Platform Manager statistics are authoritative.",
	}
	if !request.Traffic {
		warnings = append(warnings, "Traffic-disabled comparison uses the documented shortest-distance mode and may have geometry different from the fastest route.")
	}
	return contracts.ProviderRouteResponse{Candidates: candidates, Warnings: warnings}, nil
}

type normalizedDGISPart struct {
	geometry   []domain.GeoPoint
	weight     float64
	class      domain.CongestionClass
	confidence domain.Confidence
	source     string
}

func normalizeDGISRoute(request contracts.ProviderRouteRequest, routeIndex int, route dgisRoute) (domain.RouteCandidate, error) {
	if !finitePositive(route.TotalDistance) || route.TotalDistance > maxDGISTotalRouteMeters ||
		!finitePositive(route.TotalDuration) || route.TotalDuration > maxDGISTotalDuration {
		return domain.RouteCandidate{}, serviceError("PROVIDER_RESPONSE_INVALID", "provider returned invalid route measurements", http.StatusBadGateway, false, nil)
	}
	if route.Reliability != nil && (math.IsNaN(*route.Reliability) || math.IsInf(*route.Reliability, 0) || *route.Reliability < 0 || *route.Reliability > 1) {
		return domain.RouteCandidate{}, serviceError("PROVIDER_RESPONSE_INVALID", "provider returned invalid route reliability", http.StatusBadGateway, false, nil)
	}
	if len(route.Maneuvers) == 0 || len(route.Maneuvers) > maxDGISManeuvers {
		return domain.RouteCandidate{}, serviceError("PROVIDER_RESPONSE_INVALID", "provider returned an invalid maneuver count", http.StatusBadGateway, false, nil)
	}
	if len(route.RouteID) > 128 {
		return domain.RouteCandidate{}, serviceError("PROVIDER_RESPONSE_INVALID", "provider route reference exceeds the safety limit", http.StatusBadGateway, false, nil)
	}

	distanceMeters := int64(math.Round(route.TotalDistance))
	durationSeconds := int64(math.Round(route.TotalDuration))
	if distanceMeters <= 0 || durationSeconds <= 0 {
		return domain.RouteCandidate{}, serviceError("PROVIDER_RESPONSE_INVALID", "provider returned sub-unit route measurements", http.StatusBadGateway, false, nil)
	}

	dataType := dgisTrafficDataType(request)
	geometry := make([]domain.GeoPoint, 0, 256)
	parts := make([]normalizedDGISPart, 0, min(len(route.Maneuvers)*2, 256))
	totalParts, totalPoints := 0, 0
	for _, maneuver := range route.Maneuvers {
		path := maneuver.OutcomingPath
		if path == nil {
			continue
		}
		// A negative or non-finite measurement is corrupt and still fails closed.
		// Zero is not: 2GIS marks the start and end of a route with a maneuver
		// whose path has no distance, no duration and a single repeated point.
		// Rejecting the whole route over that marker made every 2GIS route
		// unusable, which is a normalization bug rather than provider evidence.
		if !nonNegativeFinite(path.Distance) || !nonNegativeFinite(path.Duration) {
			return domain.RouteCandidate{}, serviceError("PROVIDER_RESPONSE_INVALID", "provider returned invalid maneuver measurements", http.StatusBadGateway, false, nil)
		}
		totalParts += len(path.Geometry)
		if totalParts > maxDGISGeometryParts {
			return domain.RouteCandidate{}, serviceError("PROVIDER_RESPONSE_INVALID", "provider returned too many geometry parts", http.StatusBadGateway, false, nil)
		}
		for _, part := range path.Geometry {
			partGeometry, err := parseDGISGeometryPart(part.Selection, maxProviderPointsPerRoute-totalPoints)
			if err != nil {
				return domain.RouteCandidate{}, err
			}
			totalPoints += len(partGeometry)
			if totalPoints > maxProviderPointsPerRoute {
				return domain.RouteCandidate{}, serviceError("PROVIDER_RESPONSE_INVALID", "provider route geometry exceeds the safety limit", http.StatusBadGateway, false, nil)
			}
			geometry = appendDistinct(geometry, partGeometry)
			weight := part.Length
			if !finitePositive(weight) {
				weight = routegeometry.PolylineLength(partGeometry)
			}
			if weight > maxDGISTotalRouteMeters {
				return domain.RouteCandidate{}, serviceError("PROVIDER_RESPONSE_INVALID", "provider returned an invalid detailed geometry length", http.StatusBadGateway, false, nil)
			}
			if len(partGeometry) < 2 || !finitePositive(weight) {
				// A marker part covers no ground: it carries no traffic evidence and
				// must not receive a share of the route distance or duration.
				continue
			}
			class, confidence, source := normalizeDGISTrafficColor(request.Traffic, part.Color)
			parts = append(parts, normalizedDGISPart{
				geometry: partGeometry, weight: weight, class: class, confidence: confidence, source: source,
			})
		}
	}
	if len(geometry) < 2 || len(parts) == 0 {
		return domain.RouteCandidate{}, serviceError("PROVIDER_RESPONSE_INVALID", "provider returned incomplete route geometry", http.StatusBadGateway, false, nil)
	}
	if routegeometry.PolylineLength(geometry) > route.TotalDistance*2+5_000 {
		return domain.RouteCandidate{}, serviceError("PROVIDER_RESPONSE_INVALID", "provider geometry is inconsistent with route length", http.StatusBadGateway, false, nil)
	}

	weights := make([]float64, len(parts))
	for index := range parts {
		weights[index] = parts[index].weight
	}
	segmentDistances, ok := allocateDGISMeasurements(distanceMeters, weights)
	if !ok {
		return domain.RouteCandidate{}, serviceError("PROVIDER_RESPONSE_INVALID", "provider route distance cannot cover detailed geometry parts", http.StatusBadGateway, false, nil)
	}
	segmentDurations, ok := allocateDGISMeasurements(durationSeconds, weights)
	if !ok {
		return domain.RouteCandidate{}, serviceError("PROVIDER_RESPONSE_INVALID", "provider route duration cannot cover detailed geometry parts", http.StatusBadGateway, false, nil)
	}
	segments := make([]domain.RouteSegment, 0, len(parts))
	for index, part := range parts {
		segment := domain.RouteSegment{
			SegmentID: fmt.Sprintf("2gis-%d-%d", routeIndex+1, index+1), Geometry: part.geometry,
			DistanceMeters: segmentDistances[index], CongestionClass: part.class,
			Confidence: part.confidence, GeometrySimilarity: 1, Source: part.source,
		}
		if dataType == domain.TrafficDataBaseline {
			segment.BaselineDurationSeconds = segmentDurations[index]
		} else {
			segment.LiveDurationSeconds = segmentDurations[index]
		}
		segments = append(segments, segment)
	}

	reasonCodes := []string{"PROVIDER_ROUTE_DETAILS"}
	if request.Traffic {
		reasonCodes = append(reasonCodes, "DGIS_TRAFFIC_COLOR_SEGMENTS")
	}
	confidence := domain.Confidence{
		Level: domain.ConfidenceMedium, Score: 0.65,
		Reasons: []string{"OFFICIAL_PROVIDER_ROUTE", "PROVIDER_TRAFFIC_COLOR_INFERRED"},
	}
	switch dataType {
	case domain.TrafficDataRealtime:
		reasonCodes = append(reasonCodes, "DGIS_TRAFFIC_REALTIME")
	case domain.TrafficDataForecast:
		reasonCodes = append(reasonCodes, "DGIS_TRAFFIC_FORECAST")
	case domain.TrafficDataBaseline:
		reasonCodes = append(reasonCodes, "TRAFFIC_DISABLED_SHORTEST_BASELINE")
		confidence = domain.Confidence{Level: domain.ConfidenceLow, Score: 0.5, Reasons: []string{"TRAFFIC_DISABLED_BASELINE"}}
	default:
		reasonCodes = append(reasonCodes, "DGIS_TRAFFIC_TYPE_UNKNOWN")
		confidence = domain.Confidence{Level: domain.ConfidenceLow, Score: 0.5, Reasons: []string{"TRAFFIC_SEMANTICS_UNAVAILABLE"}}
	}

	candidate := domain.RouteCandidate{
		CandidateID: candidateID("2gis", routeIndex, geometry, distanceMeters, durationSeconds), Provider: "2gis",
		ProviderRouteReference: strings.TrimSpace(route.RouteID), TrafficDataType: dataType,
		Geometry: geometry, DistanceMeters: distanceMeters, Segments: segments,
		Tolls: rawCollectionNonEmpty(route.TotalPayments), Confidence: confidence, ReasonCodes: reasonCodes,
		Explanation: "GreenRoute разбил официальный маршрут 2ГИС на цветовые участки ответа Routing API; цвет отражает оценку GreenRoute на момент запроса.",
		GeneratedBy: "INITIAL_PROVIDER",
	}
	if dataType == domain.TrafficDataBaseline {
		candidate.BaselineDurationSeconds = durationSeconds
	} else {
		candidate.LiveDurationSeconds = durationSeconds
	}
	return candidate, nil
}

func normalizeDGISRouteLegacy(request contracts.ProviderRouteRequest, routeIndex int, route dgisRoute) (domain.RouteCandidate, error) {
	if !finitePositive(route.TotalDistance) || route.TotalDistance > maxDGISTotalRouteMeters ||
		!finitePositive(route.TotalDuration) || route.TotalDuration > maxDGISTotalDuration {
		return domain.RouteCandidate{}, serviceError("PROVIDER_RESPONSE_INVALID", "provider returned invalid route measurements", http.StatusBadGateway, false, nil)
	}
	if route.Reliability != nil && (math.IsNaN(*route.Reliability) || math.IsInf(*route.Reliability, 0) || *route.Reliability < 0 || *route.Reliability > 1) {
		return domain.RouteCandidate{}, serviceError("PROVIDER_RESPONSE_INVALID", "provider returned invalid route reliability", http.StatusBadGateway, false, nil)
	}
	if len(route.Maneuvers) == 0 || len(route.Maneuvers) > maxDGISManeuvers {
		return domain.RouteCandidate{}, serviceError("PROVIDER_RESPONSE_INVALID", "provider returned an invalid maneuver count", http.StatusBadGateway, false, nil)
	}
	if len(route.RouteID) > 128 {
		return domain.RouteCandidate{}, serviceError("PROVIDER_RESPONSE_INVALID", "provider route reference exceeds the safety limit", http.StatusBadGateway, false, nil)
	}

	geometry := make([]domain.GeoPoint, 0, 256)
	segments := make([]domain.RouteSegment, 0, min(len(route.Maneuvers), 128))
	totalParts, totalPoints := 0, 0
	appendWKT := func(selection string) ([]domain.GeoPoint, error) {
		if strings.TrimSpace(selection) == "" {
			return nil, nil
		}
		part, err := parseDGISLineString(selection, maxProviderPointsPerRoute-totalPoints)
		if err != nil {
			return nil, err
		}
		totalPoints += len(part)
		if totalPoints > maxProviderPointsPerRoute {
			return nil, serviceError("PROVIDER_RESPONSE_INVALID", "provider route geometry exceeds the safety limit", http.StatusBadGateway, false, nil)
		}
		geometry = appendDistinct(geometry, part)
		return part, nil
	}

	if _, err := appendWKT(route.BeginPedestrianPath.Geometry.Selection); err != nil {
		return domain.RouteCandidate{}, err
	}
	dataType := dgisTrafficDataType(request)
	for maneuverIndex, maneuver := range route.Maneuvers {
		path := maneuver.OutcomingPath
		if path == nil {
			continue
		}
		totalParts += len(path.Geometry)
		if totalParts > maxDGISGeometryParts {
			return domain.RouteCandidate{}, serviceError("PROVIDER_RESPONSE_INVALID", "provider returned too many geometry parts", http.StatusBadGateway, false, nil)
		}
		segmentGeometry := make([]domain.GeoPoint, 0, 16)
		for _, part := range path.Geometry {
			partGeometry, err := appendWKT(part.Selection)
			if err != nil {
				return domain.RouteCandidate{}, err
			}
			segmentGeometry = appendDistinct(segmentGeometry, partGeometry)
		}
		if len(segmentGeometry) < 2 {
			continue
		}
		if !finitePositive(path.Distance) || !finitePositive(path.Duration) {
			return domain.RouteCandidate{}, serviceError("PROVIDER_RESPONSE_INVALID", "provider returned invalid maneuver measurements", http.StatusBadGateway, false, nil)
		}
		if routegeometry.PolylineLength(segmentGeometry) > path.Distance*2+5_000 {
			return domain.RouteCandidate{}, serviceError("PROVIDER_RESPONSE_INVALID", "provider maneuver geometry is inconsistent with its length", http.StatusBadGateway, false, nil)
		}
		segmentDistance := int64(math.Round(path.Distance))
		segmentDuration := int64(math.Round(path.Duration))
		if segmentDistance <= 0 || segmentDuration <= 0 {
			continue
		}
		segment := domain.RouteSegment{
			SegmentID:          fmt.Sprintf("2gis-%d-%d", routeIndex+1, maneuverIndex+1),
			Geometry:           segmentGeometry,
			DistanceMeters:     segmentDistance,
			CongestionClass:    domain.CongestionUnknown,
			GeometrySimilarity: 1,
			Source:             "DGIS_ROUTING_API",
			Confidence: domain.Confidence{
				Level: domain.ConfidenceLow, Score: 0.5,
				Reasons: []string{"PROVIDER_HAS_NO_OFFICIAL_SEGMENT_CONGESTION_CLASS"},
			},
		}
		if dataType == domain.TrafficDataBaseline {
			segment.BaselineDurationSeconds = segmentDuration
		} else {
			segment.LiveDurationSeconds = segmentDuration
		}
		segments = append(segments, segment)
	}
	if _, err := appendWKT(route.EndPedestrianPath.Geometry.Selection); err != nil {
		return domain.RouteCandidate{}, err
	}
	if len(geometry) < 2 || len(segments) == 0 {
		return domain.RouteCandidate{}, serviceError("PROVIDER_RESPONSE_INVALID", "provider returned incomplete route geometry", http.StatusBadGateway, false, nil)
	}
	if routegeometry.PolylineLength(geometry) > route.TotalDistance*2+5_000 {
		return domain.RouteCandidate{}, serviceError("PROVIDER_RESPONSE_INVALID", "provider geometry is inconsistent with route length", http.StatusBadGateway, false, nil)
	}

	distanceMeters := int64(math.Round(route.TotalDistance))
	durationSeconds := int64(math.Round(route.TotalDuration))
	if distanceMeters <= 0 || durationSeconds <= 0 {
		return domain.RouteCandidate{}, serviceError("PROVIDER_RESPONSE_INVALID", "provider returned sub-unit route measurements", http.StatusBadGateway, false, nil)
	}
	reasonCodes := []string{"PROVIDER_ROUTE_DETAILS", "SEGMENT_CONGESTION_UNKNOWN"}
	confidence := domain.Confidence{
		Level: domain.ConfidenceMedium, Score: 0.65,
		Reasons: []string{"OFFICIAL_PROVIDER_ROUTE", "NO_OFFICIAL_SEGMENT_CONGESTION_CLASS", "NO_BASELINE_LIVE_PAIR"},
	}
	switch dataType {
	case domain.TrafficDataRealtime:
		reasonCodes = append(reasonCodes, "DGIS_TRAFFIC_REALTIME")
	case domain.TrafficDataForecast:
		reasonCodes = append(reasonCodes, "DGIS_TRAFFIC_FORECAST")
	case domain.TrafficDataBaseline:
		reasonCodes = append(reasonCodes, "TRAFFIC_DISABLED_SHORTEST_BASELINE")
	default:
		reasonCodes = append(reasonCodes, "DGIS_TRAFFIC_TYPE_UNKNOWN")
		confidence = domain.Confidence{Level: domain.ConfidenceLow, Score: 0.5, Reasons: []string{"TRAFFIC_SEMANTICS_UNAVAILABLE"}}
	}

	candidate := domain.RouteCandidate{
		CandidateID:            candidateID("2gis", routeIndex, geometry, distanceMeters, durationSeconds),
		Provider:               "2gis",
		ProviderRouteReference: strings.TrimSpace(route.RouteID),
		TrafficDataType:        dataType,
		Geometry:               geometry,
		DistanceMeters:         distanceMeters,
		Segments:               segments,
		Tolls:                  rawCollectionNonEmpty(route.TotalPayments),
		Confidence:             confidence,
		ReasonCodes:            reasonCodes,
		Explanation:            "Маршрут нормализован из официального 2GIS Routing API; покомпонентный класс загруженности остаётся UNKNOWN до независимого baseline-сопоставления.",
		GeneratedBy:            "INITIAL_PROVIDER",
	}
	if dataType == domain.TrafficDataBaseline {
		candidate.BaselineDurationSeconds = durationSeconds
	} else {
		candidate.LiveDurationSeconds = durationSeconds
	}
	return candidate, nil
}

func dgisTrafficDataType(request contracts.ProviderRouteRequest) domain.TrafficDataType {
	if !request.Traffic {
		return domain.TrafficDataBaseline
	}
	if dgisForecastDeparture(request.DepartureUnix, time.Now()) {
		return domain.TrafficDataForecast
	}
	return domain.TrafficDataRealtime
}

func dgisForecastDeparture(departureUnix int64, now time.Time) bool {
	if departureUnix <= 0 {
		return false
	}
	// departureTime is mandatory in the public search contract and immediate
	// searches send the current time. Only a materially future departure uses
	// statistics; a trip starting now must use the live 2GIS `jam` mode.
	return departureUnix > now.Add(5*time.Minute).Unix()
}

func normalizeDGISTrafficColor(traffic bool, color string) (domain.CongestionClass, domain.Confidence, string) {
	if !traffic {
		return domain.CongestionUnknown, domain.Confidence{
			Level: domain.ConfidenceLow, Score: 0.5, Reasons: []string{"TRAFFIC_DISABLED_BASELINE"},
		}, "DGIS_ROUTING_API"
	}

	class := domain.CongestionUnknown
	switch strings.ToLower(strings.TrimSpace(color)) {
	case "fast", "free":
		class = domain.CongestionGreen
	case "normal":
		class = domain.CongestionYellow
	case "fluid":
		class = domain.CongestionOrange
	case "slow", "slow-jams":
		class = domain.CongestionRed
	case "", "ignore", "no-traffic":
		class = domain.CongestionUnknown
	default:
		class = domain.CongestionUnknown
	}
	if class == domain.CongestionUnknown {
		return class, domain.Confidence{
			Level: domain.ConfidenceLow, Score: 0.25, Reasons: []string{"PROVIDER_TRAFFIC_CLASS_UNKNOWN"},
		}, domain.SegmentSourceDGISTrafficColor
	}
	return class, domain.Confidence{
		Level: domain.ConfidenceMedium, Score: 0.7, Reasons: []string{"PROVIDER_TRAFFIC_COLOR_INFERRED"},
	}, domain.SegmentSourceDGISTrafficColor
}

func allocateDGISMeasurements(total int64, weights []float64) ([]int64, bool) {
	if len(weights) == 0 || total < int64(len(weights)) {
		return nil, false
	}
	remainingTotal := total
	remainingWeight := 0.0
	for _, weight := range weights {
		if !finitePositive(weight) {
			return nil, false
		}
		remainingWeight += weight
	}
	result := make([]int64, len(weights))
	for index, weight := range weights {
		remainingItems := int64(len(weights) - index - 1)
		if index == len(weights)-1 {
			result[index] = remainingTotal
			break
		}
		share := int64(math.Round(float64(remainingTotal) * weight / remainingWeight))
		share = max(int64(1), min(share, remainingTotal-remainingItems))
		result[index] = share
		remainingTotal -= share
		remainingWeight -= weight
	}
	return result, true
}

func parseDGISLineString(value string, remainingPoints int) ([]domain.GeoPoint, error) {
	geometry, err := parseDGISGeometryPart(value, remainingPoints)
	if err != nil {
		return nil, err
	}
	if len(geometry) < 2 {
		return nil, serviceError("PROVIDER_RESPONSE_INVALID", "provider returned insufficient WKT geometry", http.StatusBadGateway, false, nil)
	}
	return geometry, nil
}

// parseDGISGeometryPart validates WKT syntax without demanding that the part
// spans any ground. A syntactically valid part collapsing to a single distinct
// point is a marker, not a malformed response, and the caller decides how to
// treat it.
func parseDGISGeometryPart(value string, remainingPoints int) ([]domain.GeoPoint, error) {
	value = strings.TrimSpace(value)
	if len(value) == 0 || len(value) > maxDGISWKTBytes || remainingPoints < 2 {
		return nil, serviceError("PROVIDER_RESPONSE_INVALID", "provider returned invalid WKT geometry", http.StatusBadGateway, false, nil)
	}
	const prefix = "LINESTRING"
	if len(value) < len(prefix) || !strings.EqualFold(value[:len(prefix)], prefix) {
		return nil, serviceError("PROVIDER_RESPONSE_INVALID", "provider returned unsupported WKT geometry", http.StatusBadGateway, false, nil)
	}
	coordinates := strings.TrimSpace(value[len(prefix):])
	if len(coordinates) < 5 || coordinates[0] != '(' || coordinates[len(coordinates)-1] != ')' {
		return nil, serviceError("PROVIDER_RESPONSE_INVALID", "provider returned malformed WKT geometry", http.StatusBadGateway, false, nil)
	}
	coordinates = coordinates[1 : len(coordinates)-1]
	parts := strings.Split(coordinates, ",")
	if len(parts) < 2 || len(parts) > remainingPoints {
		return nil, serviceError("PROVIDER_RESPONSE_INVALID", "provider WKT geometry exceeds the point limit", http.StatusBadGateway, false, nil)
	}
	geometry := make([]domain.GeoPoint, 0, len(parts))
	for _, rawPoint := range parts {
		fields := strings.Fields(rawPoint)
		if len(fields) < 2 || len(fields) > 3 {
			return nil, serviceError("PROVIDER_RESPONSE_INVALID", "provider returned malformed WKT coordinate", http.StatusBadGateway, false, nil)
		}
		longitude, err := strconv.ParseFloat(fields[0], 64)
		if err != nil || math.IsNaN(longitude) || math.IsInf(longitude, 0) {
			return nil, serviceError("PROVIDER_RESPONSE_INVALID", "provider returned malformed WKT longitude", http.StatusBadGateway, false, err)
		}
		latitude, err := strconv.ParseFloat(fields[1], 64)
		if err != nil || math.IsNaN(latitude) || math.IsInf(latitude, 0) {
			return nil, serviceError("PROVIDER_RESPONSE_INVALID", "provider returned malformed WKT latitude", http.StatusBadGateway, false, err)
		}
		if len(fields) == 3 {
			altitude, err := strconv.ParseFloat(fields[2], 64)
			if err != nil || math.IsNaN(altitude) || math.IsInf(altitude, 0) {
				return nil, serviceError("PROVIDER_RESPONSE_INVALID", "provider returned malformed WKT altitude", http.StatusBadGateway, false, err)
			}
		}
		point := domain.GeoPoint{Latitude: latitude, Longitude: longitude}
		if err := point.Validate(); err != nil {
			return nil, serviceError("PROVIDER_RESPONSE_INVALID", "provider returned out-of-range WKT geometry", http.StatusBadGateway, false, err)
		}
		geometry = appendDistinct(geometry, []domain.GeoPoint{point})
	}
	return geometry, nil
}

func nonNegativeFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}

func rawCollectionNonEmpty(raw json.RawMessage) bool {
	value := strings.TrimSpace(string(raw))
	return value != "" && value != "null" && value != "[]" && value != "{}"
}

func (a *dgisAdapter) Suggest(ctx context.Context, query, language string, limit int) (contracts.GeosuggestResponse, error) {
	if a.addressResolver == nil {
		return contracts.GeosuggestResponse{}, errUnsupported
	}
	return a.addressResolver.Suggest(ctx, query, language, limit)
}

func (a *dgisAdapter) Capabilities() capabilityDocument {
	document := capabilityDocument{
		ProviderCapabilities: contracts.ProviderCapabilities{
			ContractVersion: contracts.InternalContractVersion,
			Provider:        "2gis",
			Mode:            providerModeDGIS,
			APIIntegrations: []contracts.APIIntegration{
				{
					ID: "2gis-routing", Provider: "2gis", Product: "Routing API", APIVersion: dgisRoutingAPIVersion,
					Capability: apiCapabilityRouting, Role: apiRolePrimary, State: apiStateActive,
				},
			},
			MaxAlternatives: a.cfg.DGISMaxResults - 1,
			MaxWaypoints:    maxDGISRoutePoints,
			RealtimeTraffic: true,
			// The documented shortest-distance mode ignores traffic. Geometry may
			// differ, and the orchestrator validates similarity before scoring.
			TrafficDisabledBaseline:   true,
			DepartureTime:             true,
			AvoidZones:                true,
			Geosuggest:                a.addressResolver != nil,
			Coverage:                  []string{"2GIS Routing API subscription coverage"},
			ExperimentalSources:       false,
			DataStorageAllowed:        a.cfg.ProviderDataStorageAllowed,
			RawResponseCacheTTLSecond: 0,
		},
		VerifiedAt: "2026-08-05",
		OfficialDocumentation: []string{
			"https://docs.2gis.com/en/api/navigation/routing/overview",
			"https://docs.2gis.com/en/api/navigation/routing/start",
			"https://docs.2gis.com/en/api/navigation/routing/reference/routing",
			"https://docs.2gis.com/en/api/navigation/routing/examples/routing",
		},
		OfficialEndpoint:            dgisRoutingEndpoint,
		MaxRoutesPerRequest:         a.cfg.DGISMaxResults,
		RequestsPerSecond:           0,
		RequestsPerMinute:           a.cfg.DGISRateLimitPerMinute,
		DailyRequestLimit:           nil,
		MonthlyRequestLimit:         &a.cfg.DGISMonthlyLimit,
		DailyLimitContractDependent: true,
		AvoidTolls:                  true,
		AvoidUnpaved:                "supported by documented dirt_road filter",
		Billing: billingCapability{
			Unit:                "route for one point set",
			MultipleRoutesCount: "alternatives in the same request are not billed additionally",
		},
		Storage: storageCapability{
			Standard: "disabled by this adapter unless the active 2GIS contract is reviewed and configuration is enabled",
			Extended: "active 2GIS subscription terms are authoritative",
		},
		Licenses: licenseCapability{
			BasicName: "2GIS API subscription",
			FreeTier:  "demo access is time-limited; Platform Manager limits are authoritative",
		},
		DataModificationAllowed: a.cfg.ProviderDataModificationOK,
		Limitations: []string{
			"The documented Routing API does not expose an official per-segment congestion level or jam score.",
			"Traffic-disabled comparison uses shortest-distance mode; geometry similarity must be checked before scoring.",
			"Road filters may be relaxed by the provider when a fully filtered route is too long or impossible; detected toll payment data is preserved.",
			"Per-minute and monthly limits are subscription-specific and must be configured in 2GIS Platform Manager; provider 429 responses trigger an adapter-wide cooldown.",
		},
	}
	if a.addressResolver != nil {
		document.AddressSearchProvider = a.addressProvider
		document.AddressSearchEndpoint = a.addressEndpoint
		document.OfficialDocumentation = append(document.OfficialDocumentation, a.addressDocs...)
		document.APIIntegrations = append(document.APIIntegrations, addressAPIIntegration(a.addressProvider, apiRolePrimary, apiStateActive))
		document.Limitations = append(document.Limitations, "Point-bearing address lookup is served by the configured "+a.addressProvider+" geocoder; routing remains 2GIS.")
		if a.addressFallbackProvider != "" {
			document.OfficialDocumentation = append(document.OfficialDocumentation, a.addressFallbackDocs...)
			document.APIIntegrations = append(document.APIIntegrations, addressAPIIntegration(a.addressFallbackProvider, apiRoleFallback, apiStateStandby))
			document.Limitations = append(document.Limitations, "When the primary Yandex geocoder returns an error or no matches, the adapter fails over to the documented 2GIS Geocoder API.")
		}
	}
	return document
}

func addressAPIIntegration(provider, role, state string) contracts.APIIntegration {
	if provider == addressProviderYandex {
		return contracts.APIIntegration{
			ID: "yandex-http-geocoder", Provider: "yandex", Product: "HTTP Geocoder API", APIVersion: yandexGeocoderAPIVersion,
			Capability: apiCapabilityAddressSearch, Role: role, State: state,
		}
	}
	return contracts.APIIntegration{
		ID: "2gis-geocoder", Provider: "2gis", Product: "Geocoder API", APIVersion: dgisGeocoderAPIVersion,
		Capability: apiCapabilityAddressSearch, Role: role, State: state,
	}
}

func (a *dgisAdapter) Ready() error {
	if a.cfg.DGISAPIKey == "" {
		return errors.New("provider credentials are not configured")
	}
	if a.credentialFault.Failed() {
		return errCredentialFault
	}
	if a.breaker.State() == breakerOpen {
		return errCircuitOpen
	}
	if a.addressResolver != nil {
		if err := a.addressResolver.Ready(); err != nil {
			return fmt.Errorf("address resolver is not ready: %w", err)
		}
	}
	return nil
}
