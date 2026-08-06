package main

import (
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
)

const (
	dgisGeocoderAPIVersion          = "3.0"
	dgisGeocoderEndpoint            = "https://catalog.api.2gis.com/" + dgisGeocoderAPIVersion + "/items/geocode"
	dgisGeocoderDocumentation       = "https://docs.2gis.com/api/search/geocoder/reference/3.0/items/geocode"
	defaultDGISGeocoderLocationBias = "37.617635,55.755814"
	maxDGISGeocoderTextBytes        = 2_048
	maxDGISGeocoderIDBytes          = 512
)

// dgisGeocoder provides coordinate-bearing address results. It is intentionally
// named geocoder rather than suggest: 2GIS recommends Suggest API for a true
// autocomplete product, while this contract requires coordinates in every item.
type dgisGeocoder struct {
	cfg             Config
	client          *http.Client
	metrics         *serviceMetrics
	breaker         *circuitBreaker
	bulkhead        *bulkhead
	cooldown        *sharedCooldown
	rateGate        *slidingWindowGate
	credentialFault *credentialFaultLatch
	now             func() time.Time
	sleep           func(context.Context, time.Duration) error
	cooldownJitter  func(time.Duration) time.Duration
}

func newDGISGeocoder(
	cfg Config,
	client *http.Client,
	metrics *serviceMetrics,
	bulkhead *bulkhead,
	cooldown *sharedCooldown,
	credentialFault *credentialFaultLatch,
) *dgisGeocoder {
	return &dgisGeocoder{
		cfg: cfg, client: client, metrics: metrics,
		breaker:         newCircuitBreaker(cfg.CircuitBreakerThreshold, cfg.CircuitBreakerOpenDuration),
		bulkhead:        bulkhead,
		cooldown:        cooldown,
		rateGate:        newSlidingWindowGate(cfg.DGISGeocoderRatePerMinute, time.Minute),
		credentialFault: credentialFault,
		now:             time.Now,
		sleep:           sleepContext,
		cooldownJitter:  defaultCooldownJitter,
	}
}

func (g *dgisGeocoder) Ready() error {
	if g.cfg.DGISAPIKey == "" {
		return errors.New("2GIS geocoder credentials are not configured")
	}
	if g.credentialFault.Failed() {
		return errCredentialFault
	}
	if g.breaker.State() == breakerOpen {
		return errCircuitOpen
	}
	return nil
}

func (g *dgisGeocoder) Suggest(ctx context.Context, query, language string, limit int) (contracts.GeosuggestResponse, error) {
	var lastErr error
	for attempt := 0; attempt <= g.cfg.MaxRetries; attempt++ {
		delay := g.cooldown.WaitDuration(g.now(), g.cooldownJitter)
		if delay > 0 {
			if err := g.sleep(ctx, delay); err != nil {
				return contracts.GeosuggestResponse{}, err
			}
		}
		if !g.breaker.Allow() {
			g.metrics.geocoderCircuitRejected.Add(1)
			return contracts.GeosuggestResponse{}, errCircuitOpen
		}
		if err := g.bulkhead.Acquire(ctx); err != nil {
			if errors.Is(err, errBulkheadFull) {
				g.metrics.bulkheadRejected.Add(1)
			}
			return contracts.GeosuggestResponse{}, err
		}
		if allowed, retryAfter := g.rateGate.Try(g.now()); !allowed {
			g.bulkhead.Release()
			g.metrics.localRateGateRejected.Add(1)
			err := serviceError("PROVIDER_RATE_LIMITED", "configured geocoder quota gate is temporarily full", http.StatusTooManyRequests, true, nil)
			err.RetryAfter = retryAfter
			return contracts.GeosuggestResponse{}, err
		}

		g.metrics.inFlight.Add(1)
		g.metrics.geocoderRequests.Add(1)
		started := time.Now()
		response, err := g.doGeocode(ctx, query, language, limit)
		g.metrics.inFlight.Add(-1)
		g.bulkhead.Release()
		g.metrics.observeGeocoder(time.Since(started), err)
		if err == nil {
			g.credentialFault.Success(credentialGeocoder)
			g.breaker.Success()
			return response, nil
		}

		lastErr = err
		providerErr := normalizeProviderError(err)
		switch {
		case providerErr.Code == "PROVIDER_RATE_LIMITED":
			g.metrics.provider429.Add(1)
			g.cooldown.Extend(g.now(), providerCooldownDelay(attempt, g.cfg.RetryBaseDelay, g.cfg.RetryMaxDelay, providerErr.RetryAfter))
		case isDGISCredentialAccessError(providerErr.Code):
			g.credentialFault.Fail(credentialGeocoder)
		case providerErr.Retryable:
			g.breaker.Failure()
		}
		if !providerErr.Retryable || attempt == g.cfg.MaxRetries {
			return contracts.GeosuggestResponse{}, err
		}
		if providerErr.Code == "PROVIDER_RATE_LIMITED" {
			continue
		}
		if err := g.sleep(ctx, retryDelay(attempt, g.cfg.RetryBaseDelay, g.cfg.RetryMaxDelay, 0)); err != nil {
			return contracts.GeosuggestResponse{}, err
		}
	}
	return contracts.GeosuggestResponse{}, lastErr
}

func (g *dgisGeocoder) doGeocode(ctx context.Context, query, language string, limit int) (contracts.GeosuggestResponse, error) {
	endpoint, err := buildDGISGeocoderURL(g.cfg, query, language, limit)
	if err != nil {
		return contracts.GeosuggestResponse{}, serviceError("PROVIDER_REQUEST_INVALID", "geocoder request could not be constructed", http.StatusBadRequest, false, err)
	}
	attemptCtx, cancel := context.WithTimeout(ctx, g.cfg.RequestTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return contracts.GeosuggestResponse{}, serviceError("PROVIDER_REQUEST_INVALID", "geocoder request could not be constructed", http.StatusInternalServerError, false, err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "ProbokNet-provider/1.0")
	response, err := g.client.Do(request)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return contracts.GeosuggestResponse{}, callerContextError(ctxErr)
		}
		return contracts.GeosuggestResponse{}, serviceError("PROVIDER_NETWORK_ERROR", "geocoder connection failed", http.StatusBadGateway, true, err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := readBounded(response.Body, g.cfg.MaxProviderResponseBytes)
	if err != nil {
		return contracts.GeosuggestResponse{}, serviceError("PROVIDER_RESPONSE_INVALID", "geocoder response exceeded the safe size limit", http.StatusBadGateway, false, err)
	}
	if response.StatusCode == http.StatusNotFound {
		return contracts.GeosuggestResponse{Suggestions: []domain.GeoSuggestion{}}, nil
	}
	if response.StatusCode != http.StatusOK {
		return contracts.GeosuggestResponse{}, mapDGISGeocoderStatus(response.StatusCode, response.Header.Get("Retry-After"), g.now())
	}
	var wire dgisGeocoderResponse
	if err := json.Unmarshal(body, &wire); err != nil {
		return contracts.GeosuggestResponse{}, serviceError("PROVIDER_RESPONSE_INVALID", "geocoder returned an invalid response", http.StatusBadGateway, false, err)
	}
	return normalizeDGISGeocoder(wire, limit)
}

func buildDGISGeocoderURL(cfg Config, query, language string, limit int) (string, error) {
	endpoint, err := url.Parse(dgisGeocoderEndpoint)
	if err != nil {
		return "", err
	}
	if endpoint.Scheme != "https" || endpoint.Hostname() != "catalog.api.2gis.com" || endpoint.Path != "/"+dgisGeocoderAPIVersion+"/items/geocode" {
		return "", errors.New("unsafe geocoder endpoint")
	}
	parameters := endpoint.Query()
	parameters.Set("key", cfg.DGISAPIKey)
	parameters.Set("q", strings.TrimSpace(query))
	parameters.Set("fields", "items.point,items.full_address_name")
	parameters.Set("locale", dgisLocale(language))
	parameters.Set("page", "1")
	parameters.Set("page_size", strconv.Itoa(limit))
	parameters.Set("sort", "relevance")
	location, err := normalizeDGISLocationBias(cfg.DGISGeocoderLocationBias)
	if err != nil {
		return "", err
	}
	parameters.Set("location", location)
	endpoint.RawQuery = parameters.Encode()
	return endpoint.String(), nil
}

func normalizeDGISLocationBias(value string) (string, error) {
	parts := strings.Split(strings.TrimSpace(value), ",")
	if len(parts) != 2 {
		return "", errors.New("expected exactly two comma-separated coordinates")
	}
	lon, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil || math.IsNaN(lon) || math.IsInf(lon, 0) || lon < -180 || lon > 180 {
		return "", errors.New("longitude is outside -180..180")
	}
	lat, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err != nil || math.IsNaN(lat) || math.IsInf(lat, 0) || lat < -90 || lat > 90 {
		return "", errors.New("latitude is outside -90..90")
	}
	return fmt.Sprintf("%s,%s", strconv.FormatFloat(lon, 'f', -1, 64), strconv.FormatFloat(lat, 'f', -1, 64)), nil
}

func dgisLocale(language string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(language)), "en") {
		return "en_RU"
	}
	return "ru_RU"
}

func mapDGISGeocoderStatus(status int, retryAfter string, now time.Time) error {
	switch status {
	case http.StatusBadRequest:
		return serviceError("PROVIDER_REJECTED_REQUEST", "geocoder rejected the normalized request", http.StatusBadGateway, false, nil)
	case http.StatusUnauthorized:
		return serviceError("PROVIDER_AUTHENTICATION_FAILED", "2GIS rejected the geocoder API key", http.StatusServiceUnavailable, false, nil)
	case http.StatusPaymentRequired:
		return serviceError("PROVIDER_SUBSCRIPTION_REQUIRED", "2GIS Geocoder API subscription or paid quota access is required", http.StatusServiceUnavailable, false, nil)
	case http.StatusForbidden:
		return serviceError("PROVIDER_ACCESS_FORBIDDEN", "2GIS Geocoder API access is forbidden by key service access or restrictions", http.StatusServiceUnavailable, false, nil)
	case http.StatusNotFound:
		return nil
	case http.StatusRequestTimeout:
		return serviceError("PROVIDER_TIMEOUT", "geocoder request timed out", http.StatusGatewayTimeout, true, nil)
	case http.StatusTooManyRequests:
		err := serviceError("PROVIDER_RATE_LIMITED", "geocoder rate limit reached", http.StatusTooManyRequests, true, nil)
		err.RetryAfter = parseRetryAfter(retryAfter, now)
		return err
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return serviceError("PROVIDER_UNAVAILABLE", "geocoder is temporarily unavailable", http.StatusBadGateway, true, nil)
	default:
		if status >= 500 && status <= 599 {
			return serviceError("PROVIDER_UNAVAILABLE", "geocoder is temporarily unavailable", http.StatusBadGateway, true, nil)
		}
		return serviceError("PROVIDER_UNEXPECTED_STATUS", "geocoder returned an unexpected status", http.StatusBadGateway, false, nil)
	}
}

type dgisGeocoderResponse struct {
	Meta struct {
		Code int `json:"code"`
	} `json:"meta"`
	Result struct {
		Items []dgisGeocoderItem `json:"items"`
	} `json:"result"`
}

type dgisGeocoderItem struct {
	ID              string        `json:"id"`
	Name            string        `json:"name"`
	FullName        string        `json:"full_name"`
	AddressName     string        `json:"address_name"`
	FullAddressName string        `json:"full_address_name"`
	PurposeName     string        `json:"purpose_name"`
	Point           *dgisGeoPoint `json:"point"`
}

type dgisGeoPoint struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

func normalizeDGISGeocoder(response dgisGeocoderResponse, limit int) (contracts.GeosuggestResponse, error) {
	if response.Meta.Code == http.StatusNotFound {
		return contracts.GeosuggestResponse{Suggestions: []domain.GeoSuggestion{}}, nil
	}
	if response.Meta.Code != http.StatusOK {
		return contracts.GeosuggestResponse{}, serviceError("PROVIDER_RESPONSE_INVALID", "geocoder returned an invalid result code", http.StatusBadGateway, false, nil)
	}
	if len(response.Result.Items) > limit {
		return contracts.GeosuggestResponse{}, serviceError("PROVIDER_RESPONSE_INVALID", "geocoder returned too many results", http.StatusBadGateway, false, nil)
	}
	suggestions := make([]domain.GeoSuggestion, 0, len(response.Result.Items))
	seen := make(map[string]struct{}, len(response.Result.Items))
	for _, item := range response.Result.Items {
		if !validDGISGeocoderStrings(item) || item.Point == nil {
			continue
		}
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		label := firstNonEmptyText(item.FullAddressName, item.AddressName, item.FullName, item.Name)
		if label == "" {
			continue
		}
		point := domain.GeoPoint{Latitude: item.Point.Lat, Longitude: item.Point.Lon}
		if err := point.Validate(); err != nil {
			continue
		}
		subtitle := firstDistinctText(label, item.Name, item.PurposeName, item.FullName, item.AddressName)
		suggestions = append(suggestions, domain.GeoSuggestion{ID: id, Label: label, Subtitle: subtitle, Point: point})
		seen[id] = struct{}{}
	}
	return contracts.GeosuggestResponse{Suggestions: suggestions}, nil
}

func validDGISGeocoderStrings(item dgisGeocoderItem) bool {
	if len(item.ID) > maxDGISGeocoderIDBytes {
		return false
	}
	for _, value := range []string{item.Name, item.FullName, item.AddressName, item.FullAddressName, item.PurposeName} {
		if len(value) > maxDGISGeocoderTextBytes {
			return false
		}
	}
	return true
}

func firstNonEmptyText(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func firstDistinctText(label string, values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !strings.EqualFold(value, label) {
			return value
		}
	}
	return ""
}
