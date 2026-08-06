package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/greenroute/greenroute/internal/contracts"
	"github.com/greenroute/greenroute/internal/domain"
)

const (
	yandexGeocoderAPIVersion            = "v1"
	yandexGeocoderEndpoint              = "https://geocode-maps.yandex.ru/" + yandexGeocoderAPIVersion + "/"
	yandexGeocoderRequestDocumentation  = "https://yandex.com/maps-api/docs/geocoder-api/request.html"
	yandexGeocoderResponseDocumentation = "https://yandex.com/maps-api/docs/geocoder-api/response.html"
)

type yandexAddressResolver struct {
	adapter *yandexAdapter
}

func (r *yandexAddressResolver) Suggest(ctx context.Context, query, language string, limit int) (contracts.GeosuggestResponse, error) {
	return r.adapter.Suggest(ctx, query, language, limit)
}

func (r *yandexAddressResolver) Ready() error {
	if r.adapter.cfg.YandexGeocoderAPIKey == "" {
		return errors.New("credentials for the Yandex geocoder are not configured")
	}
	if r.adapter.credentialFault.Failed() {
		return errCredentialFault
	}
	if r.adapter.geocoderBreaker.State() == breakerOpen {
		return errCircuitOpen
	}
	return nil
}

func (a *yandexAdapter) Suggest(ctx context.Context, query, language string, limit int) (contracts.GeosuggestResponse, error) {
	if a.cfg.YandexGeocoderAPIKey == "" {
		return contracts.GeosuggestResponse{}, errUnsupported
	}
	var lastErr error
	for attempt := 0; attempt <= a.cfg.MaxRetries; attempt++ {
		if err := a.waitForProviderCooldown(ctx); err != nil {
			return contracts.GeosuggestResponse{}, err
		}
		if !a.geocoderBreaker.Allow() {
			a.metrics.geocoderCircuitRejected.Add(1)
			return contracts.GeosuggestResponse{}, errCircuitOpen
		}
		if err := a.bulkhead.Acquire(ctx); err != nil {
			if errors.Is(err, errBulkheadFull) {
				a.metrics.bulkheadRejected.Add(1)
			}
			return contracts.GeosuggestResponse{}, err
		}

		a.metrics.inFlight.Add(1)
		a.metrics.geocoderRequests.Add(1)
		started := time.Now()
		response, err := a.doGeocode(ctx, query, language, limit)
		a.metrics.inFlight.Add(-1)
		a.bulkhead.Release()
		a.metrics.observeGeocoder(time.Since(started), err)
		if err == nil {
			a.credentialFault.Success(credentialGeocoder)
			a.geocoderBreaker.Success()
			return response, nil
		}

		lastErr = err
		providerErr := normalizeProviderError(err)
		if providerErr.Code == "PROVIDER_RATE_LIMITED" {
			a.metrics.provider429.Add(1)
			a.cooldown.Extend(a.now(), providerCooldownDelay(attempt, a.cfg.RetryBaseDelay, a.cfg.RetryMaxDelay, providerErr.RetryAfter))
		} else if providerErr.Code == "PROVIDER_AUTHENTICATION_FAILED" {
			a.credentialFault.Fail(credentialGeocoder)
		} else if providerErr.Retryable {
			a.geocoderBreaker.Failure()
		}
		if !providerErr.Retryable || attempt == a.cfg.MaxRetries {
			return contracts.GeosuggestResponse{}, err
		}
		if providerErr.Code == "PROVIDER_RATE_LIMITED" {
			continue
		}
		delay := retryDelay(attempt, a.cfg.RetryBaseDelay, a.cfg.RetryMaxDelay, 0)
		if err := a.sleep(ctx, delay); err != nil {
			return contracts.GeosuggestResponse{}, err
		}
	}
	return contracts.GeosuggestResponse{}, lastErr
}

func (a *yandexAdapter) doGeocode(ctx context.Context, query, language string, limit int) (contracts.GeosuggestResponse, error) {
	endpoint, err := buildYandexGeocoderURL(a.cfg, query, language, limit)
	if err != nil {
		return contracts.GeosuggestResponse{}, serviceError("PROVIDER_REQUEST_INVALID", "address request could not be constructed", http.StatusBadRequest, false, err)
	}
	attemptCtx, cancel := context.WithTimeout(ctx, a.cfg.RequestTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return contracts.GeosuggestResponse{}, serviceError("PROVIDER_REQUEST_INVALID", "address request could not be constructed", http.StatusInternalServerError, false, err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "GreenRoute-provider-yandex/1.0")

	response, err := a.client.Do(request)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return contracts.GeosuggestResponse{}, callerContextError(ctxErr)
		}
		return contracts.GeosuggestResponse{}, serviceError("PROVIDER_NETWORK_ERROR", "address provider connection failed", http.StatusBadGateway, true, err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := readBounded(response.Body, a.cfg.MaxProviderResponseBytes)
	if err != nil {
		return contracts.GeosuggestResponse{}, serviceError("PROVIDER_RESPONSE_INVALID", "address provider response exceeded the safe size limit", http.StatusBadGateway, false, err)
	}
	if response.StatusCode != http.StatusOK {
		return contracts.GeosuggestResponse{}, mapYandexStatus(response.StatusCode, response.Header.Get("Retry-After"), a.now())
	}
	var wire yandexGeocoderResponse
	if err := json.Unmarshal(body, &wire); err != nil {
		return contracts.GeosuggestResponse{}, serviceError("PROVIDER_RESPONSE_INVALID", "address provider returned an invalid response", http.StatusBadGateway, false, err)
	}
	normalized, err := normalizeGeocoderResponse(wire)
	if err != nil {
		return contracts.GeosuggestResponse{}, err
	}
	// Do not trust an upstream implementation to honor results exactly. The
	// internal response must never exceed the caller's validated limit.
	if len(normalized.Suggestions) > limit {
		normalized.Suggestions = normalized.Suggestions[:limit]
	}
	return normalized, nil
}

func buildYandexGeocoderURL(cfg Config, query, language string, limit int) (string, error) {
	endpoint, err := url.Parse(yandexGeocoderEndpoint)
	if err != nil {
		return "", err
	}
	if endpoint.Scheme != "https" || endpoint.Hostname() != "geocode-maps.yandex.ru" || endpoint.Path != "/"+yandexGeocoderAPIVersion+"/" {
		return "", errors.New("unsafe geocoder endpoint")
	}
	values := endpoint.Query()
	values.Set("apikey", cfg.YandexGeocoderAPIKey)
	values.Set("geocode", query)
	values.Set("lang", language)
	values.Set("results", strconv.Itoa(limit))
	values.Set("format", "json")
	endpoint.RawQuery = values.Encode()
	return endpoint.String(), nil
}

type yandexGeocoderResponse struct {
	Response struct {
		GeoObjectCollection struct {
			FeatureMember []struct {
				GeoObject yandexGeoObject `json:"GeoObject"`
			} `json:"featureMember"`
		} `json:"GeoObjectCollection"`
	} `json:"response"`
}

type yandexGeoObject struct {
	MetaDataProperty struct {
		GeocoderMetaData struct {
			Text      string `json:"text"`
			Precision string `json:"precision"`
			Address   struct {
				Formatted string `json:"formatted"`
			} `json:"Address"`
		} `json:"GeocoderMetaData"`
	} `json:"metaDataProperty"`
	Name        string `json:"name"`
	Description string `json:"description"`
	URI         string `json:"uri"`
	Point       struct {
		Position string `json:"pos"`
	} `json:"Point"`
}

func normalizeGeocoderResponse(response yandexGeocoderResponse) (contracts.GeosuggestResponse, error) {
	members := response.Response.GeoObjectCollection.FeatureMember
	suggestions := make([]domain.GeoSuggestion, 0, len(members))
	seen := make(map[string]struct{}, len(members))
	invalidObjects := 0
	for _, member := range members {
		object := member.GeoObject
		point, err := parseGeocoderPoint(object.Point.Position)
		if err != nil {
			invalidObjects++
			continue
		}
		label := firstText(object.Name, object.MetaDataProperty.GeocoderMetaData.Address.Formatted, object.MetaDataProperty.GeocoderMetaData.Text)
		if label == "" {
			invalidObjects++
			continue
		}
		subtitle := strings.TrimSpace(object.Description)
		if subtitle == "" {
			formatted := strings.TrimSpace(object.MetaDataProperty.GeocoderMetaData.Address.Formatted)
			if formatted != label {
				subtitle = formatted
			}
		}
		id := strings.TrimSpace(object.URI)
		if id == "" {
			id = geocoderSuggestionID(label, point)
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		suggestions = append(suggestions, domain.GeoSuggestion{ID: id, Label: label, Subtitle: subtitle, Point: point})
	}
	if len(members) > 0 && len(suggestions) == 0 && invalidObjects > 0 {
		return contracts.GeosuggestResponse{}, serviceError("PROVIDER_RESPONSE_INVALID", "address provider returned no valid point-bearing objects", http.StatusBadGateway, false, nil)
	}
	return contracts.GeosuggestResponse{Suggestions: suggestions}, nil
}

func parseGeocoderPoint(position string) (domain.GeoPoint, error) {
	parts := strings.Fields(position)
	if len(parts) != 2 {
		return domain.GeoPoint{}, errors.New("invalid Point.pos")
	}
	longitude, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return domain.GeoPoint{}, err
	}
	latitude, err := strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return domain.GeoPoint{}, err
	}
	point := domain.GeoPoint{Latitude: latitude, Longitude: longitude}
	if err := point.Validate(); err != nil {
		return domain.GeoPoint{}, err
	}
	return point, nil
}

func firstText(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func geocoderSuggestionID(label string, point domain.GeoPoint) string {
	digest := sha256.Sum256([]byte(label + "|" + formatCoordinate(point.Latitude) + "|" + formatCoordinate(point.Longitude)))
	return "yandex-geo-" + hex.EncodeToString(digest[:12])
}
