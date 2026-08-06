package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/greenroute/greenroute/internal/contracts"
	"github.com/greenroute/greenroute/internal/domain"
	"github.com/greenroute/greenroute/internal/telemetry"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

var errUpstreamUnavailable = errors.New("upstream service unavailable")

type serviceClient struct {
	orchestrator *url.URL
	provider     *url.URL
	token        string
	http         *http.Client
	stream       *http.Client
}

func newServiceClient(orchestratorURL, providerURL, token string) (*serviceClient, error) {
	orchestrator, err := validatedServiceURL(orchestratorURL)
	if err != nil {
		return nil, err
	}
	provider, err := validatedServiceURL(providerURL)
	if err != nil {
		return nil, err
	}
	transport := otelhttp.NewTransport(newInternalServiceTransport(), otelhttp.WithFilter(telemetry.QuerylessRequestFilter))
	return &serviceClient{
		orchestrator: orchestrator, provider: provider, token: token,
		http: &http.Client{
			Transport: transport, Timeout: 12 * time.Second,
			CheckRedirect: rejectInternalRedirect,
		},
		stream: &http.Client{Transport: transport, CheckRedirect: rejectInternalRedirect},
	}, nil
}

func newInternalServiceTransport() *http.Transport {
	return &http.Transport{
		// Internal credentials must never be forwarded to an ambient proxy.
		Proxy:               nil,
		DialContext:         (&net.Dialer{Timeout: 800 * time.Millisecond, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout: time.Second, ResponseHeaderTimeout: 10 * time.Second,
		MaxIdleConns: 128, MaxIdleConnsPerHost: 64, IdleConnTimeout: 60 * time.Second,
	}
}

func rejectInternalRedirect(*http.Request, []*http.Request) error {
	// A redirect target is outside the configured trust decision even when it
	// happens to share a hostname. Returning the response lets callers map it
	// to a sanitized upstream error without ever forwarding Authorization.
	return http.ErrUseLastResponse
}

func (c *serviceClient) startSearch(ctx context.Context, requestID, operationKey string, request domain.RouteSearchRequest) (int, []byte, domain.RouteSearchResult, error) {
	internalRequest, err := c.request(ctx, c.orchestrator, http.MethodPost, "/internal/v1/searches", requestID, request)
	if err != nil {
		return 0, nil, domain.RouteSearchResult{}, err
	}
	if operationKey != "" {
		internalRequest.Header.Set("X-Operation-Key", operationKey)
	}
	response, err := c.http.Do(internalRequest)
	if err != nil {
		return 0, nil, domain.RouteSearchResult{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return 0, nil, domain.RouteSearchResult{}, errUpstreamUnavailable
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusAccepted && response.StatusCode != http.StatusUnprocessableEntity {
		return response.StatusCode, body, domain.RouteSearchResult{}, errUpstreamUnavailable
	}
	if response.StatusCode == http.StatusUnprocessableEntity {
		return response.StatusCode, body, domain.RouteSearchResult{}, nil
	}
	var result domain.RouteSearchResult
	if err := json.Unmarshal(body, &result); err != nil {
		return response.StatusCode, body, result, errUpstreamUnavailable
	}
	return response.StatusCode, body, result, nil
}

func (c *serviceClient) getSearch(ctx context.Context, requestID, searchID string) (int, []byte, domain.RouteSearchResult, error) {
	response, err := c.do(ctx, c.orchestrator, http.MethodGet, "/internal/v1/searches/"+url.PathEscape(searchID), requestID, nil, false)
	if err != nil {
		return 0, nil, domain.RouteSearchResult{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return 0, nil, domain.RouteSearchResult{}, errUpstreamUnavailable
	}
	if response.StatusCode != http.StatusOK {
		return response.StatusCode, body, domain.RouteSearchResult{}, nil
	}
	var result domain.RouteSearchResult
	if err := json.Unmarshal(body, &result); err != nil {
		return response.StatusCode, body, result, errUpstreamUnavailable
	}
	return response.StatusCode, body, result, nil
}

func (c *serviceClient) deleteSearch(ctx context.Context, requestID, searchID string) (int, error) {
	response, err := c.do(ctx, c.orchestrator, http.MethodDelete, "/internal/v1/searches/"+url.PathEscape(searchID), requestID, nil, false)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	return response.StatusCode, nil
}

func (c *serviceClient) searchEvents(ctx context.Context, requestID, searchID, lastEventID string) (*http.Response, error) {
	path := "/internal/v1/searches/" + url.PathEscape(searchID) + "/events"
	request, err := c.request(ctx, c.orchestrator, http.MethodGet, path, requestID, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "text/event-stream")
	if lastEventID != "" {
		request.Header.Set("Last-Event-ID", lastEventID)
	}
	return c.stream.Do(request)
}

func (c *serviceClient) geosuggest(ctx context.Context, requestID, query, language string, limit int) (int, []byte, error) {
	values := url.Values{"q": []string{query}, "lang": []string{language}, "limit": []string{strconv.Itoa(limit)}}
	path := "/internal/v1/geosuggest?" + values.Encode()
	response, err := c.do(ctx, c.provider, http.MethodGet, path, requestID, nil, false)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	return response.StatusCode, body, err
}

func (c *serviceClient) adminOverview(ctx context.Context, requestID string) (int, []byte, error) {
	response, err := c.do(ctx, c.orchestrator, http.MethodGet, "/internal/v1/admin/overview", requestID, nil, false)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	return response.StatusCode, body, err
}

func (c *serviceClient) health(ctx context.Context, base *url.URL) error {
	response, err := c.do(ctx, base, http.MethodGet, "/health/ready", "readiness-check", nil, false)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errUpstreamUnavailable
	}
	return nil
}

func (c *serviceClient) do(ctx context.Context, base *url.URL, method, path, requestID string, payload any, stream bool) (*http.Response, error) {
	request, err := c.request(ctx, base, method, path, requestID, payload)
	if err != nil {
		return nil, err
	}
	if stream {
		return c.stream.Do(request)
	}
	return c.http.Do(request)
}

func (c *serviceClient) request(ctx context.Context, base *url.URL, method, path, requestID string, payload any) (*http.Request, error) {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(encoded)
	}
	pathOnly, query, _ := strings.Cut(path, "?")
	target := base.ResolveReference(&url.URL{Path: strings.TrimRight(base.Path, "/") + pathOnly, RawQuery: query})
	request, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Request-ID", requestID)
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	return request, nil
}

func validateResult(result domain.RouteSearchResult) error {
	if result.SearchID == "" || result.Status == "" {
		return fmt.Errorf("missing required search response fields")
	}
	if result.Alternatives == nil || result.BestEffortRoutes == nil {
		return fmt.Errorf("route arrays must be present")
	}
	if len(result.BestEffortRoutes) > 3 {
		return fmt.Errorf("too many best-effort routes")
	}
	if (result.Status == domain.SearchCompleted || result.Status == domain.SearchDegraded) && result.SelectedRoute != nil {
		if err := validateResultCandidate(*result.SelectedRoute); err != nil {
			return err
		}
	}
	seen := make(map[string]struct{}, len(result.BestEffortRoutes))
	for _, candidate := range result.BestEffortRoutes {
		if err := validateResultCandidate(candidate); err != nil {
			return fmt.Errorf("invalid best-effort route: %w", err)
		}
		if _, duplicate := seen[candidate.CandidateID]; duplicate {
			return fmt.Errorf("duplicate best-effort route")
		}
		seen[candidate.CandidateID] = struct{}{}
		if result.SelectedRoute != nil && candidate.CandidateID == result.SelectedRoute.CandidateID {
			return fmt.Errorf("selected route cannot also be best effort")
		}
		if !containsReason(candidate.ReasonCodes, "BEST_EFFORT_NOT_STRICT_GREEN") || !containsReasonPrefix(candidate.ReasonCodes, "STRICT_GREEN_") {
			return fmt.Errorf("best-effort route lacks strict rejection reasons")
		}
	}
	if len(result.GreenTopRoutes) > 3 {
		return fmt.Errorf("too many green-ranked routes")
	}
	greenSeen := make(map[string]struct{}, len(result.GreenTopRoutes))
	for _, candidate := range result.GreenTopRoutes {
		if err := validateResultCandidate(candidate); err != nil {
			return fmt.Errorf("invalid green-ranked route: %w", err)
		}
		if _, duplicate := greenSeen[candidate.CandidateID]; duplicate {
			return fmt.Errorf("duplicate green-ranked route")
		}
		greenSeen[candidate.CandidateID] = struct{}{}
		if !containsReasonPrefix(candidate.ReasonCodes, "GREEN_RANK_") {
			return fmt.Errorf("green-ranked route lacks its rank reason code")
		}
	}
	return nil
}

func validateResultCandidate(candidate domain.RouteCandidate) error {
	if candidate.CandidateID == "" {
		return fmt.Errorf("missing candidate id")
	}
	if candidate.Confidence.Level != domain.ConfidenceHigh && candidate.Confidence.Level != domain.ConfidenceMedium && candidate.Confidence.Level != domain.ConfidenceLow {
		return fmt.Errorf("invalid confidence level")
	}
	if candidate.Confidence.Score < 0 || candidate.Confidence.Score > 1 || len(candidate.ReasonCodes) == 0 {
		return fmt.Errorf("missing confidence or reason codes")
	}
	return nil
}

func containsReason(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func containsReasonPrefix(values []string, prefix string) bool {
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func decodeGeosuggestResponse(body []byte) error {
	var response contracts.GeosuggestResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return err
	}
	for _, suggestion := range response.Suggestions {
		if suggestion.ID == "" || suggestion.Label == "" || suggestion.Point.Validate() != nil {
			return fmt.Errorf("invalid geosuggest response")
		}
	}
	return nil
}
