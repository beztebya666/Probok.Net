package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/greenroute/greenroute/internal/contracts"
	"github.com/greenroute/greenroute/internal/domain"
	"github.com/greenroute/greenroute/internal/telemetry"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const (
	maxProviderRouteResponseBytes        = int64(8 << 20)
	maxProviderCapabilitiesResponseBytes = int64(1 << 20)
	maxProviderRequestsPerCall           = 20
	maxProviderCandidates                = 3
	maxProviderGeometryPoints            = 4_096
	maxProviderSegments                  = 2_000
	maxProviderSegmentGeometryPoints     = 8_192
	maxProviderWarnings                  = 64
	maxProviderWarningBytes              = 2_048
	maxProviderIdentifierBytes           = 256
	maxProviderAPIIntegrations           = 8
	maxProviderAPIMetadataBytes          = 128
)

var (
	errProviderUnavailable = errors.New("provider unavailable")
	errProviderRateLimited = errors.New("provider rate limited")
	errProviderContract    = errors.New("provider contract violation")
)

type providerClient struct {
	baseURL       *url.URL
	internalToken string
	httpClient    *http.Client
	metrics       *telemetry.Metrics
}

func newProviderClient(rawURL, internalToken string, metrics *telemetry.Metrics) (*providerClient, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("invalid provider URL")
	}
	transport := newInternalProviderTransport()
	return &providerClient{
		baseURL: parsed, internalToken: internalToken, metrics: metrics,
		httpClient: &http.Client{
			Transport:     otelhttp.NewTransport(transport, otelhttp.WithFilter(telemetry.QuerylessRequestFilter)),
			Timeout:       8 * time.Second,
			CheckRedirect: rejectInternalRedirect,
		},
	}, nil
}

func newInternalProviderTransport() *http.Transport {
	return &http.Transport{
		// The bearer token is scoped to the configured service and must never be
		// exposed to an ambient HTTP(S)_PROXY.
		Proxy:               nil,
		DialContext:         (&net.Dialer{Timeout: 800 * time.Millisecond, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout: 1 * time.Second, ResponseHeaderTimeout: 3 * time.Second,
		MaxIdleConns: 64, MaxIdleConnsPerHost: 32, IdleConnTimeout: 60 * time.Second,
	}
}

func rejectInternalRedirect(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}

func (c *providerClient) routes(ctx context.Context, request contracts.ProviderRouteRequest) (contracts.ProviderRouteResponse, error) {
	started := time.Now()
	response, err := c.doJSON(ctx, http.MethodPost, "/internal/v1/routes", request)
	c.metrics.ProviderRequestDuration.WithLabelValues("routes").Observe(time.Since(started).Seconds())
	if err != nil {
		c.metrics.ProviderRequests.WithLabelValues("routes", "error").Inc()
		return contracts.ProviderRouteResponse{}, err
	}
	defer func() { _ = response.Body.Close() }()
	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusTooManyRequests:
		c.metrics.Provider429.Inc()
		return contracts.ProviderRouteResponse{}, errProviderRateLimited
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return contracts.ProviderRouteResponse{}, errProviderContract
	default:
		c.metrics.ProviderErrors.WithLabelValues(statusCategory(response.StatusCode)).Inc()
		return contracts.ProviderRouteResponse{}, errProviderUnavailable
	}
	var result contracts.ProviderRouteResponse
	if err := decodeBoundedStrictJSON(response.Body, maxProviderRouteResponseBytes, &result); err != nil {
		c.metrics.ProviderRequests.WithLabelValues("routes", "contract_error").Inc()
		c.metrics.ProviderErrors.WithLabelValues("contract").Inc()
		return result, errProviderContract
	}
	if err := validateProviderRouteResponse(result, request); err != nil {
		c.metrics.ProviderRequests.WithLabelValues("routes", "contract_error").Inc()
		c.metrics.ProviderErrors.WithLabelValues("contract").Inc()
		return contracts.ProviderRouteResponse{}, errProviderContract
	}
	c.metrics.ProviderRequests.WithLabelValues("routes", "success").Inc()
	return result, nil
}

func (c *providerClient) capabilities(ctx context.Context) (contracts.ProviderCapabilities, error) {
	response, err := c.doJSON(ctx, http.MethodGet, "/internal/v1/capabilities", nil)
	if err != nil {
		return contracts.ProviderCapabilities{}, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return contracts.ProviderCapabilities{}, errProviderUnavailable
	}
	var document providerCapabilitiesDocument
	if err := decodeBoundedStrictJSON(response.Body, maxProviderCapabilitiesResponseBytes, &document); err != nil {
		return contracts.ProviderCapabilities{}, errProviderContract
	}
	if err := validateProviderCapabilitiesDocument(document); err != nil {
		return contracts.ProviderCapabilities{}, errProviderContract
	}
	return document.ProviderCapabilities, nil
}

func (c *providerClient) health(ctx context.Context) error {
	response, err := c.doJSON(ctx, http.MethodGet, "/health/ready", nil)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return errProviderUnavailable
	}
	return nil
}

func (c *providerClient) doJSON(ctx context.Context, method, path string, payload any) (*http.Response, error) {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(encoded)
	}
	target := c.baseURL.ResolveReference(&url.URL{Path: strings.TrimRight(c.baseURL.Path, "/") + path})
	request, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if c.internalToken != "" {
		request.Header.Set("Authorization", "Bearer "+c.internalToken)
	}
	return c.httpClient.Do(request)
}

func statusCategory(status int) string {
	switch {
	case status == http.StatusTooManyRequests:
		return "rate_limited"
	case status >= 500:
		return "provider_5xx"
	case status >= 400:
		return "provider_4xx"
	default:
		return "transport"
	}
}

// providerCapabilitiesDocument mirrors the complete, versioned response of
// provider-yandex. The orchestrator intentionally returns only the embedded
// provider-neutral capabilities to the rest of the service.
type providerCapabilitiesDocument struct {
	contracts.ProviderCapabilities
	VerifiedAt                  string                    `json:"verifiedAt"`
	OfficialDocumentation       []string                  `json:"officialDocumentation"`
	OfficialEndpoint            string                    `json:"officialEndpoint,omitempty"`
	AddressSearchProvider       string                    `json:"addressSearchProvider,omitempty"`
	AddressSearchEndpoint       string                    `json:"addressSearchEndpoint,omitempty"`
	MaxRoutesPerRequest         int                       `json:"maxRoutesPerRequest"`
	RequestsPerSecond           int                       `json:"requestsPerSecond"`
	RequestsPerMinute           int                       `json:"requestsPerMinute,omitempty"`
	DailyRequestLimit           *int                      `json:"dailyRequestLimit"`
	MonthlyRequestLimit         *int                      `json:"monthlyRequestLimit,omitempty"`
	DailyLimitContractDependent bool                      `json:"dailyLimitContractDependent"`
	AvoidTolls                  bool                      `json:"avoidTolls"`
	AvoidUnpaved                string                    `json:"avoidUnpaved"`
	Billing                     providerBillingCapability `json:"billing"`
	Storage                     providerStorageCapability `json:"storage"`
	DataModificationAllowed     bool                      `json:"dataModificationAllowed"`
	Licenses                    providerLicenseCapability `json:"licenses"`
	Limitations                 []string                  `json:"limitations"`
	ExperimentalRequested       bool                      `json:"experimentalSourcesRequested"`
}

type providerBillingCapability struct {
	Unit                string `json:"unit"`
	MultipleRoutesCount string `json:"multipleRoutesCount"`
}

type providerStorageCapability struct {
	Standard string `json:"standard"`
	Extended string `json:"extended"`
}

type providerLicenseCapability struct {
	BasicName    string `json:"basicName"`
	AdvancedName string `json:"advancedName"`
	FreeTier     string `json:"freeTier"`
}

func validateProviderCapabilitiesDocument(document providerCapabilitiesDocument) error {
	if document.ContractVersion != contracts.InternalContractVersion {
		return fmt.Errorf("provider contract version %q is incompatible with %q", document.ContractVersion, contracts.InternalContractVersion)
	}
	if strings.TrimSpace(document.Provider) == "" || strings.TrimSpace(document.Mode) == "" {
		return fmt.Errorf("provider and mode are required")
	}
	if document.MaxAlternatives < 0 || document.MaxAlternatives > 2 {
		return fmt.Errorf("maxAlternatives must be between 0 and 2")
	}
	if document.MaxWaypoints < 0 || document.MaxWaypoints > 50 {
		return fmt.Errorf("maxWaypoints must be between 0 and 50")
	}
	if document.RequestsPerMinute < 0 || document.RequestsPerMinute > 100_000 {
		return fmt.Errorf("requestsPerMinute must be between 0 and 100000")
	}
	if document.MonthlyRequestLimit != nil && (*document.MonthlyRequestLimit < 1 || *document.MonthlyRequestLimit > 100_000_000) {
		return fmt.Errorf("monthlyRequestLimit must be between 1 and 100000000")
	}
	if document.AddressSearchProvider != "" && document.AddressSearchProvider != "yandex" && document.AddressSearchProvider != "2gis" {
		return fmt.Errorf("addressSearchProvider must be yandex or 2gis")
	}
	if document.AddressSearchProvider != "" && strings.TrimSpace(document.AddressSearchEndpoint) == "" {
		return fmt.Errorf("addressSearchEndpoint is required with addressSearchProvider")
	}
	if err := validateAPIIntegrations(document.APIIntegrations); err != nil {
		return err
	}
	return nil
}

func validateAPIIntegrations(integrations []contracts.APIIntegration) error {
	if len(integrations) > maxProviderAPIIntegrations {
		return fmt.Errorf("too many API integrations")
	}
	seen := make(map[string]struct{}, len(integrations))
	for index, integration := range integrations {
		values := []struct {
			name  string
			value string
		}{
			{name: "id", value: integration.ID},
			{name: "provider", value: integration.Provider},
			{name: "product", value: integration.Product},
			{name: "apiVersion", value: integration.APIVersion},
		}
		for _, item := range values {
			trimmed := strings.TrimSpace(item.value)
			if trimmed == "" || trimmed != item.value || len(trimmed) > maxProviderAPIMetadataBytes {
				return fmt.Errorf("apiIntegrations[%d].%s is invalid", index, item.name)
			}
		}
		if _, duplicate := seen[integration.ID]; duplicate {
			return fmt.Errorf("apiIntegrations[%d].id is duplicated", index)
		}
		seen[integration.ID] = struct{}{}
		switch integration.Capability {
		case "routing", "address-search":
		default:
			return fmt.Errorf("apiIntegrations[%d].capability is invalid", index)
		}
		switch integration.Role {
		case "primary", "fallback":
		default:
			return fmt.Errorf("apiIntegrations[%d].role is invalid", index)
		}
		switch integration.State {
		case "active", "standby":
		default:
			return fmt.Errorf("apiIntegrations[%d].state is invalid", index)
		}
		if integration.Role == "primary" && integration.State != "active" {
			return fmt.Errorf("apiIntegrations[%d] primary integration must be active", index)
		}
		if integration.Role == "fallback" && integration.State != "standby" {
			return fmt.Errorf("apiIntegrations[%d] fallback integration must be standby", index)
		}
	}
	return nil
}

func decodeBoundedStrictJSON(body io.Reader, maximumBytes int64, destination any) error {
	limited := &io.LimitedReader{R: body, N: maximumBytes + 1}
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}
	if limited.N <= 0 {
		return fmt.Errorf("JSON body exceeds %d bytes", maximumBytes)
	}
	var trailing any
	err := decoder.Decode(&trailing)
	if limited.N <= 0 {
		return fmt.Errorf("JSON body exceeds %d bytes", maximumBytes)
	}
	if !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("JSON body must contain exactly one value")
		}
		return fmt.Errorf("decode trailing JSON: %w", err)
	}
	return nil
}

func validateProviderRouteResponse(response contracts.ProviderRouteResponse, request contracts.ProviderRouteRequest) error {
	if request.RequestBudget < 1 || request.RequestBudget > maxProviderRequestsPerCall {
		return fmt.Errorf("request budget is outside the internal contract")
	}
	if response.RequestsUsed < 1 || response.RequestsUsed > maxProviderRequestsPerCall || response.RequestsUsed > request.RequestBudget {
		return fmt.Errorf("requestsUsed is outside the request budget")
	}
	if response.BudgetRemaining < 0 || response.BudgetRemaining > request.RequestBudget || response.RequestsUsed+response.BudgetRemaining > request.RequestBudget {
		return fmt.Errorf("budgetRemaining is inconsistent with requestsUsed")
	}
	if len(response.Candidates) < 1 || len(response.Candidates) > maxProviderCandidates {
		return fmt.Errorf("candidate count is outside the provider contract")
	}
	// Billing units are provider-contract specific. Some providers bill every
	// returned route, while 2GIS bills one point set even when that request
	// returns multiple alternatives. Keep the value bounded by the number of
	// candidates, but do not require those two counts to be identical.
	if response.EstimatedBillableUnits < 0 || response.EstimatedBillableUnits > len(response.Candidates) {
		return fmt.Errorf("estimatedBillableUnits is inconsistent with candidates")
	}
	if len(response.Warnings) > maxProviderWarnings {
		return fmt.Errorf("too many provider warnings")
	}
	for _, warning := range response.Warnings {
		if len(warning) > maxProviderWarningBytes {
			return fmt.Errorf("provider warning exceeds the size limit")
		}
	}

	seenCandidateIDs := make(map[string]struct{}, len(response.Candidates))
	for index, candidate := range response.Candidates {
		if err := validateProviderCandidate(candidate, response.RequestsUsed); err != nil {
			return fmt.Errorf("candidates[%d]: %w", index, err)
		}
		if _, exists := seenCandidateIDs[candidate.CandidateID]; exists {
			return fmt.Errorf("candidates[%d]: duplicate candidateId", index)
		}
		seenCandidateIDs[candidate.CandidateID] = struct{}{}
	}
	return nil
}

func validateProviderCandidate(candidate domain.RouteCandidate, requestsUsed int) error {
	if strings.TrimSpace(candidate.CandidateID) == "" || len(candidate.CandidateID) > maxProviderIdentifierBytes {
		return fmt.Errorf("candidateId is missing or too long")
	}
	if strings.TrimSpace(candidate.Provider) == "" || len(candidate.Provider) > maxProviderIdentifierBytes {
		return fmt.Errorf("provider is missing or too long")
	}
	if len(candidate.ProviderRouteReference) > maxProviderIdentifierBytes {
		return fmt.Errorf("providerRouteReference is too long")
	}
	if len(candidate.Geometry) < 2 || len(candidate.Geometry) > maxProviderGeometryPoints {
		return fmt.Errorf("geometry point count is outside the safety limit")
	}
	for index, point := range candidate.Geometry {
		if err := point.Validate(); err != nil {
			return fmt.Errorf("geometry[%d] is invalid", index)
		}
	}
	if candidate.DistanceMeters <= 0 || candidate.LiveDurationSeconds < 0 || candidate.BaselineDurationSeconds < 0 ||
		(candidate.LiveDurationSeconds == 0 && candidate.BaselineDurationSeconds == 0) || candidate.TrafficDelaySeconds < 0 {
		return fmt.Errorf("route measurements must be non-negative with positive distance and duration")
	}
	if !finiteInRange(candidate.Confidence.Score, 0, 1) || !finiteInRange(candidate.Score, 0, 1) {
		return fmt.Errorf("candidate confidence or score is invalid")
	}
	if candidate.ProviderRequestCount < 0 || candidate.ProviderRequestCount > requestsUsed {
		return fmt.Errorf("providerRequestCount is inconsistent with requestsUsed")
	}
	if len(candidate.Segments) > maxProviderSegments {
		return fmt.Errorf("segment count exceeds the safety limit")
	}
	if err := validateProviderMetrics(candidate.Metrics); err != nil {
		return err
	}

	totalSegmentPoints := 0
	seenSegmentIDs := make(map[string]struct{}, len(candidate.Segments))
	for index, segment := range candidate.Segments {
		totalSegmentPoints += len(segment.Geometry)
		if totalSegmentPoints > maxProviderSegmentGeometryPoints {
			return fmt.Errorf("segment geometry exceeds the aggregate safety limit")
		}
		if err := validateProviderSegment(segment); err != nil {
			return fmt.Errorf("segments[%d]: %w", index, err)
		}
		if _, exists := seenSegmentIDs[segment.SegmentID]; exists {
			return fmt.Errorf("segments[%d]: duplicate segmentId", index)
		}
		seenSegmentIDs[segment.SegmentID] = struct{}{}
	}
	return nil
}

func validateProviderSegment(segment domain.RouteSegment) error {
	if strings.TrimSpace(segment.SegmentID) == "" || len(segment.SegmentID) > maxProviderIdentifierBytes {
		return fmt.Errorf("segmentId is missing or too long")
	}
	if len(segment.Geometry) < 2 || len(segment.Geometry) > maxProviderGeometryPoints {
		return fmt.Errorf("geometry point count is outside the safety limit")
	}
	for index, point := range segment.Geometry {
		if err := point.Validate(); err != nil {
			return fmt.Errorf("geometry[%d] is invalid", index)
		}
	}
	if segment.DistanceMeters <= 0 || segment.LiveDurationSeconds < 0 || segment.BaselineDurationSeconds < 0 ||
		(segment.LiveDurationSeconds == 0 && segment.BaselineDurationSeconds == 0) {
		return fmt.Errorf("segment measurements must be non-negative with positive distance and duration")
	}
	if !finiteInRange(segment.TrafficRatio, 0, 100) || !finiteInRange(segment.GeometrySimilarity, 0, 1) ||
		!finiteInRange(segment.Confidence.Score, 0, 1) {
		return fmt.Errorf("segment ratios or confidence are invalid")
	}
	return nil
}

func validateProviderMetrics(metrics domain.RouteMetrics) error {
	if metrics.GreenDistanceMeters < 0 || metrics.YellowDistanceMeters < 0 || metrics.OrangeDistanceMeters < 0 ||
		metrics.RedDistanceMeters < 0 || metrics.UnknownDistanceMeters < 0 || metrics.GreenDurationSeconds < 0 ||
		metrics.YellowDurationSeconds < 0 || metrics.OrangeDurationSeconds < 0 || metrics.RedDurationSeconds < 0 ||
		metrics.UnknownDurationSeconds < 0 || metrics.TotalTrafficDelaySeconds < 0 || metrics.WorstContinuousCongestionMeters < 0 {
		return fmt.Errorf("route metrics cannot be negative")
	}
	if !finiteInRange(metrics.CongestedDistancePercent, 0, 100) || !finiteInRange(metrics.CongestedDurationPercent, 0, 100) ||
		!finiteInRange(metrics.GreenDistancePercent, 0, 100) {
		return fmt.Errorf("route metric percentages are invalid")
	}
	return nil
}

func finiteInRange(value, minimum, maximum float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= minimum && value <= maximum
}
