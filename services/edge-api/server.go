package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/greenroute/greenroute/internal/contracts"
	"github.com/greenroute/greenroute/internal/domain"
	"github.com/greenroute/greenroute/internal/httpx"
	"github.com/greenroute/greenroute/internal/ids"
	"github.com/greenroute/greenroute/internal/telemetry"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type apiServer struct {
	config      config
	auth        *authenticator
	state       edgeState
	auditor     *auditor
	services    *serviceClient
	metrics     *telemetry.Metrics
	bulkhead    chan struct{}
	sseGlobal   chan struct{}
	sseMu       sync.Mutex
	sseByOwner  map[string]int
	proxyRanges []netip.Prefix
}

func newAPIServer(cfg config, auth *authenticator, state edgeState, auditor *auditor, services *serviceClient, metrics *telemetry.Metrics) (http.Handler, error) {
	server := &apiServer{
		config: cfg, auth: auth, state: state, auditor: auditor, services: services, metrics: metrics,
		bulkhead: make(chan struct{}, cfg.MaximumConcurrent), sseGlobal: make(chan struct{}, cfg.MaxConcurrentSSE),
		sseByOwner: make(map[string]int),
	}
	for _, value := range cfg.TrustedProxyCIDRs {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return nil, fmt.Errorf("invalid TRUSTED_PROXY_CIDRS entry %q", value)
		}
		server.proxyRanges = append(server.proxyRanges, prefix)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", server.live)
	mux.HandleFunc("GET /health/ready", server.ready)
	mux.Handle("GET /metrics", metrics.Handler())
	mux.HandleFunc("GET /api/v1/me", server.me)
	mux.HandleFunc("POST /api/v1/route-searches", server.startSearch)
	mux.HandleFunc("GET /api/v1/route-searches/{searchID}", server.getSearch)
	mux.HandleFunc("DELETE /api/v1/route-searches/{searchID}", server.deleteSearch)
	mux.HandleFunc("GET /api/v1/route-searches/{searchID}/events", server.events)
	mux.HandleFunc("GET /api/v1/geosuggest", server.geosuggest)
	mux.HandleFunc("GET /api/v1/admin/overview", server.adminOverview)
	return server.middleware(otelhttp.NewHandler(mux, "edge-api-http", otelhttp.WithFilter(telemetry.QuerylessRequestFilter))), nil
}

func (s *apiServer) live(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "edge-api"})
}

func (s *apiServer) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 900*time.Millisecond)
	defer cancel()
	checks := map[string]string{"state": "up", "database": "up", "orchestrator": "up", "provider": "up", "auth": "up"}
	status := http.StatusOK
	check := func(name string, err error) {
		if err != nil {
			checks[name] = "down"
			status = http.StatusServiceUnavailable
		}
	}
	check("state", s.state.Ping(ctx))
	check("database", s.auditor.ping(ctx))
	check("orchestrator", s.services.health(ctx, s.services.orchestrator))
	check("provider", s.services.health(ctx, s.services.provider))
	if !s.config.AnonymousUsage && s.auth.verifier == nil {
		check("auth", errors.New("verifier unavailable"))
	}
	httpx.WriteJSON(w, status, map[string]interface{}{"status": map[bool]string{true: "ready", false: "not_ready"}[status == http.StatusOK], "checks": checks})
}

func (s *apiServer) me(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, principalFrom(r.Context()))
}

func (s *apiServer) startSearch(w http.ResponseWriter, r *http.Request) {
	var input contracts.RouteSearchInput
	if err := httpx.DecodeJSON(w, r, &input, 256<<10); err != nil {
		httpx.WriteProblem(w, problem(r, http.StatusBadRequest, "Invalid route search", err.Error()))
		return
	}
	request, err := input.Request()
	if err != nil {
		httpx.WriteProblem(w, problem(r, http.StatusBadRequest, "Invalid route search", err.Error()))
		return
	}
	request.RequestID = requestID(r)
	if err := request.Validate(domain.DefaultValidationLimits()); err != nil {
		httpx.WriteProblem(w, problem(r, http.StatusUnprocessableEntity, "Route search rejected", err.Error()))
		return
	}
	canonicalRequest := request
	canonicalRequest.RequestID = "" // transport correlation must not change idempotency semantics
	canonical, _ := jsonMarshal(canonicalRequest)
	hash := sha256.Sum256(canonical)
	bodyHash := hex.EncodeToString(hash[:])
	principal := principalFrom(r.Context())
	ownerID := currentOwnerID(principal)
	if ownerID == "" {
		httpx.WriteProblem(w, problem(r, http.StatusInternalServerError, "Identity unavailable", "A privacy-preserving ownership identity could not be established."))
		return
	}
	idempotencyHeader := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	idempotencyKey := ""
	idempotencyClaim := ""
	operationKey := ""
	replayed := false
	var status int
	var body []byte
	var result domain.RouteSearchResult
	resultReady := false
	if idempotencyHeader != "" {
		if !validIdempotencyKey(idempotencyHeader) {
			httpx.WriteProblem(w, problem(r, http.StatusBadRequest, "Invalid Idempotency-Key", "Use 8-128 printable ASCII characters."))
			return
		}
		scopeHash := sha256.Sum256([]byte(ownerID + "\x00" + idempotencyHeader))
		idempotencyKey = hex.EncodeToString(scopeHash[:])
		operationHash := sha256.Sum256([]byte(idempotencyKey + "\x00" + bodyHash))
		operationKey = hex.EncodeToString(operationHash[:])
		record, claimToken, err := s.state.BeginIdempotency(
			r.Context(), idempotencyKey, bodyHash, s.config.IdempotencyTTL, pendingIdempotencyTTL(s.config.IdempotencyTTL),
		)
		idempotencyClaim = claimToken
		switch {
		case errors.Is(err, errStateConflict):
			httpx.WriteProblem(w, problem(r, http.StatusConflict, "Idempotency conflict", "The same key was already used with a different request."))
			return
		case errors.Is(err, errStateInProgress):
			status, body, result, resultReady = s.recoverOperation(requestID(r), operationKey)
			if !resultReady {
				w.Header().Set("Retry-After", "1")
				httpx.WriteProblem(w, problem(r, http.StatusConflict, "Request already in progress", "Retry with the same key after the original request completes."))
				return
			}
			replayed = true
		case err != nil:
			httpx.WriteProblem(w, problem(r, http.StatusServiceUnavailable, "Idempotency unavailable", "Request safety state is temporarily unavailable."))
			return
		case record != nil:
			w.Header().Set("Idempotency-Replayed", "true")
			status, replayBody, replayResult, replayErr := s.services.getSearch(r.Context(), requestID(r), record.SearchID)
			if replayErr != nil || status != http.StatusOK || validateResult(replayResult) != nil {
				httpx.WriteProblem(w, problem(r, http.StatusServiceUnavailable, "Idempotent result unavailable", "The original search exists but its current state is temporarily unavailable."))
				return
			}
			w.Header().Set("Location", "/api/v1/route-searches/"+record.SearchID)
			if replayResult.Status == domain.SearchAccepted || replayResult.Status == domain.SearchSearching {
				status = http.StatusAccepted
			}
			writeRawJSON(w, status, replayBody)
			return
		}
	}
	if !resultReady {
		status, body, result, err = s.services.startSearch(r.Context(), requestID(r), operationKey, request)
		if err != nil && operationKey != "" {
			status, body, result, resultReady = s.recoverOperation(requestID(r), operationKey)
			replayed = resultReady
		}
		if err != nil && !resultReady {
			// A transport failure is an ambiguous commit: retain the short-lived
			// claim and let a retry recover the deterministic search identifier.
			httpx.WriteProblem(w, problem(r, http.StatusServiceUnavailable, "Routing service unavailable", "The routing service is temporarily unavailable; retry later."))
			return
		}
	}
	if status == http.StatusUnprocessableEntity {
		if idempotencyKey != "" {
			_ = s.state.ForgetIdempotency(r.Context(), idempotencyKey, idempotencyClaim)
		}
		writeRawJSON(w, status, body)
		return
	}
	if err := validateResult(result); err != nil {
		slog.Error("routing create response failed contract validation", "search_id", result.SearchID, "status", result.Status, "reason", err.Error())
		if operationKey != "" {
			if recoveredStatus, recoveredBody, recoveredResult, ok := s.recoverOperation(requestID(r), operationKey); ok {
				status, body, result, replayed = recoveredStatus, recoveredBody, recoveredResult, true
			} else {
				// Preserve the claim: a malformed create response can still follow a
				// successfully committed operation.
				httpx.WriteProblem(w, problem(r, http.StatusBadGateway, "Invalid routing response", "The routing service returned an invalid contract."))
				return
			}
		} else {
			httpx.WriteProblem(w, problem(r, http.StatusBadGateway, "Invalid routing response", "The routing service returned an invalid contract."))
			return
		}
	}
	if err := s.state.SetOwner(r.Context(), result.SearchID, ownerID, s.config.OwnershipTTL); err != nil {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
		deleteStatus, deleteErr := s.services.deleteSearch(cleanupContext, requestID(r), result.SearchID)
		cleanupCancel()
		if idempotencyKey != "" && deleteErr == nil && (deleteStatus == http.StatusNoContent || deleteStatus == http.StatusNotFound) {
			_ = s.state.ForgetIdempotency(r.Context(), idempotencyKey, idempotencyClaim)
		}
		httpx.WriteProblem(w, problem(r, http.StatusServiceUnavailable, "Search ownership unavailable", "The request was cancelled because authorization state could not be stored."))
		return
	}
	if idempotencyKey != "" {
		if err := s.state.CompleteIdempotency(r.Context(), idempotencyKey, bodyHash, idempotencyClaim, result.SearchID, s.config.IdempotencyTTL); err != nil {
			slog.Warn("idempotency completion failed", "request_id", requestID(r), "error", err)
		}
	}
	if replayed {
		w.Header().Set("Idempotency-Replayed", "true")
	}
	auditContext, auditCancel := context.WithTimeout(context.Background(), time.Second)
	defer auditCancel()
	if err := s.auditor.recordSearch(auditContext, principal.UserID, request, result); err != nil {
		s.metrics.AuditFailures.WithLabelValues("write").Inc()
		slog.Error("privacy audit write failed", "request_id", requestID(r), "search_id", result.SearchID, "error_type", fmt.Sprintf("%T", err))
	}
	w.Header().Set("Location", "/api/v1/route-searches/"+result.SearchID)
	writeRawJSON(w, status, body)
}

func (s *apiServer) recoverOperation(requestID, operationKey string) (int, []byte, domain.RouteSearchResult, bool) {
	if operationKey == "" {
		return 0, nil, domain.RouteSearchResult{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	searchID := ids.FromStableKey("route-search", operationKey)
	status, body, result, err := s.services.getSearch(ctx, requestID, searchID)
	if err != nil || status != http.StatusOK || validateResult(result) != nil || result.SearchID != searchID {
		return 0, nil, domain.RouteSearchResult{}, false
	}
	if result.Status == domain.SearchAccepted || result.Status == domain.SearchSearching {
		status = http.StatusAccepted
	}
	return status, body, result, true
}

func pendingIdempotencyTTL(configured time.Duration) time.Duration {
	const maximumPendingTTL = 30 * time.Second
	if configured < maximumPendingTTL {
		return configured
	}
	return maximumPendingTTL
}

func principalOwnerIDs(identity principal) []string {
	if len(identity.OwnerIDs) > 0 {
		return identity.OwnerIDs
	}
	// Tests and trusted in-process callers created before pseudonymous owner
	// identifiers may still populate only UserID.
	if identity.UserID != "" {
		return []string{identity.UserID}
	}
	return nil
}

func currentOwnerID(identity principal) string {
	owners := principalOwnerIDs(identity)
	if len(owners) == 0 {
		return ""
	}
	return owners[0]
}

func (s *apiServer) getSearch(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeSearch(w, r) {
		return
	}
	status, body, result, err := s.services.getSearch(r.Context(), requestID(r), r.PathValue("searchID"))
	if err != nil {
		httpx.WriteProblem(w, problem(r, http.StatusServiceUnavailable, "Routing service unavailable", "The routing service is temporarily unavailable."))
		return
	}
	if status != http.StatusOK {
		writeNormalizedUpstreamProblem(w, r, status)
		return
	}
	if err := validateResult(result); err != nil {
		// The client is told only that the contract was violated. Which invariant
		// broke is operational detail, and without it a 502 here is undiagnosable.
		slog.Error("routing result failed contract validation", "search_id", result.SearchID, "status", result.Status, "reason", err.Error())
		httpx.WriteProblem(w, problem(r, http.StatusBadGateway, "Invalid routing response", "The routing service returned an invalid contract."))
		return
	}
	writeRawJSON(w, status, body)
}

func (s *apiServer) deleteSearch(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeSearch(w, r) {
		return
	}
	status, err := s.services.deleteSearch(r.Context(), requestID(r), r.PathValue("searchID"))
	if err != nil {
		httpx.WriteProblem(w, problem(r, http.StatusServiceUnavailable, "Routing service unavailable", "The routing service is temporarily unavailable."))
		return
	}
	if status != http.StatusNoContent {
		writeNormalizedUpstreamProblem(w, r, status)
		return
	}
	_ = s.state.DeleteOwner(r.Context(), r.PathValue("searchID"))
	w.WriteHeader(http.StatusNoContent)
}

func (s *apiServer) events(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeSearch(w, r) {
		return
	}
	release, rejection := s.acquireSSE(currentOwnerID(principalFrom(r.Context())))
	if rejection != "" {
		s.metrics.SSERejected.WithLabelValues(rejection).Inc()
		if rejection == "principal_limit" {
			w.Header().Set("Retry-After", "30")
			httpx.WriteProblem(w, problem(r, http.StatusTooManyRequests, "SSE connection limit exceeded", "Too many event streams are already open for this identity."))
		} else {
			w.Header().Set("Retry-After", "1")
			httpx.WriteProblem(w, problem(r, http.StatusServiceUnavailable, "SSE capacity exhausted", "Event-stream capacity is temporarily exhausted."))
		}
		return
	}
	started := time.Now()
	s.metrics.ActiveSSEConnections.Inc()
	defer func() {
		release()
		s.metrics.ActiveSSEConnections.Dec()
		s.metrics.SSEConnectionDuration.Observe(time.Since(started).Seconds())
	}()
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpx.WriteProblem(w, problem(r, http.StatusInternalServerError, "Streaming unsupported", "Response streaming is unavailable."))
		return
	}
	lastEventID := strings.TrimSpace(r.Header.Get("Last-Event-ID"))
	if lastEventID != "" {
		if value, err := strconv.ParseInt(lastEventID, 10, 64); err != nil || value < 0 {
			httpx.WriteProblem(w, problem(r, http.StatusBadRequest, "Invalid event cursor", "Last-Event-ID must be a non-negative integer."))
			return
		}
	}
	lifetime := s.config.SSEMaxLifetime
	identity := principalFrom(r.Context())
	if !identity.ExpiresAt.IsZero() {
		remaining := time.Until(identity.ExpiresAt)
		if remaining <= 0 {
			httpx.WriteProblem(w, problem(r, http.StatusUnauthorized, "Session expired", "Reauthenticate before opening an event stream."))
			return
		}
		if remaining < lifetime {
			lifetime = remaining
		}
	}
	streamContext, cancelStream := context.WithTimeout(r.Context(), lifetime)
	defer cancelStream()
	response, err := s.services.searchEvents(streamContext, requestID(r), r.PathValue("searchID"), lastEventID)
	if err != nil {
		httpx.WriteProblem(w, problem(r, http.StatusServiceUnavailable, "Event stream unavailable", "The event stream is temporarily unavailable."))
		return
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		writeNormalizedUpstreamProblem(w, r, response.StatusCode)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	type streamRead struct {
		body []byte
		err  error
	}
	reads := make(chan streamRead, 1)
	go func() {
		buffer := make([]byte, 16<<10)
		for {
			read, readErr := response.Body.Read(buffer)
			item := streamRead{err: readErr}
			if read > 0 {
				item.body = append([]byte(nil), buffer[:read]...)
			}
			select {
			case reads <- item:
			case <-streamContext.Done():
				return
			}
			if readErr != nil {
				return
			}
		}
	}()
	idle := time.NewTimer(s.config.SSEIdleTimeout)
	defer idle.Stop()
	for {
		select {
		case <-streamContext.Done():
			return
		case <-idle.C:
			return
		case item := <-reads:
			if len(item.body) > 0 {
				if _, writeErr := w.Write(item.body); writeErr != nil {
					return
				}
				flusher.Flush()
				if !idle.Stop() {
					select {
					case <-idle.C:
					default:
					}
				}
				idle.Reset(s.config.SSEIdleTimeout)
			}
			if item.err != nil {
				return
			}
		}
	}
}

func (s *apiServer) acquireSSE(owner string) (func(), string) {
	select {
	case s.sseGlobal <- struct{}{}:
	default:
		return nil, "global_limit"
	}
	s.sseMu.Lock()
	if s.sseByOwner[owner] >= s.config.MaxSSEPerPrincipal {
		s.sseMu.Unlock()
		<-s.sseGlobal
		return nil, "principal_limit"
	}
	s.sseByOwner[owner]++
	s.sseMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.sseMu.Lock()
			s.sseByOwner[owner]--
			if s.sseByOwner[owner] == 0 {
				delete(s.sseByOwner, owner)
			}
			s.sseMu.Unlock()
			<-s.sseGlobal
		})
	}, ""
}

func (s *apiServer) geosuggest(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if utf8.RuneCountInString(query) < 2 || utf8.RuneCountInString(query) > 200 {
		httpx.WriteProblem(w, problem(r, http.StatusBadRequest, "Invalid query", "q must contain 2-200 characters."))
		return
	}
	language := r.URL.Query().Get("lang")
	if language == "" {
		language = "ru_RU"
	}
	if language != "ru_RU" && language != "en_US" {
		httpx.WriteProblem(w, problem(r, http.StatusBadRequest, "Invalid language", "lang must be ru_RU or en_US."))
		return
	}
	limit := 5
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 10 {
			httpx.WriteProblem(w, problem(r, http.StatusBadRequest, "Invalid limit", "limit must be between 1 and 10."))
			return
		}
		limit = parsed
	}
	status, body, err := s.services.geosuggest(r.Context(), requestID(r), query, language, limit)
	if err != nil || status >= 500 {
		httpx.WriteProblem(w, problem(r, http.StatusServiceUnavailable, "Address search unavailable", "Address search is temporarily unavailable."))
		return
	}
	if status != http.StatusOK {
		writeNormalizedUpstreamProblem(w, r, status)
		return
	}
	if err := decodeGeosuggestResponse(body); err != nil {
		httpx.WriteProblem(w, problem(r, http.StatusBadGateway, "Invalid geosuggest response", "Address provider returned an invalid contract."))
		return
	}
	writeRawJSON(w, http.StatusOK, body)
}

func (s *apiServer) adminOverview(w http.ResponseWriter, r *http.Request) {
	if !hasRole(principalFrom(r.Context()), "admin") {
		httpx.WriteProblem(w, problem(r, http.StatusForbidden, "Forbidden", "Administrator role is required."))
		return
	}
	status, body, err := s.services.adminOverview(r.Context(), requestID(r))
	if err != nil || status != http.StatusOK {
		httpx.WriteProblem(w, problem(r, http.StatusServiceUnavailable, "Admin overview unavailable", "Administrative telemetry is temporarily unavailable."))
		return
	}
	writeRawJSON(w, http.StatusOK, body)
}

func (s *apiServer) authorizeSearch(w http.ResponseWriter, r *http.Request) bool {
	if !ids.Valid(r.PathValue("searchID")) {
		httpx.WriteProblem(w, problem(r, http.StatusBadRequest, "Invalid search identifier", "searchId must be a canonical UUID."))
		return false
	}
	identity := principalFrom(r.Context())
	owners := principalOwnerIDs(identity)
	owned := false
	var err error
	for _, owner := range owners {
		owned, err = s.state.Owns(r.Context(), r.PathValue("searchID"), owner)
		if err != nil || owned {
			break
		}
	}
	if err != nil {
		httpx.WriteProblem(w, problem(r, http.StatusServiceUnavailable, "Authorization state unavailable", "Search authorization is temporarily unavailable."))
		return false
	}
	if !owned {
		// Deliberately hide whether another user owns this identifier.
		httpx.WriteProblem(w, problem(r, http.StatusNotFound, "Search not found", "The search expired, was deleted, or never existed."))
		return false
	}
	return true
}

func (s *apiServer) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		observed := &observedResponseWriter{ResponseWriter: w, status: http.StatusOK}
		w = observed
		defer func() {
			route := routeTemplate(r.URL.Path)
			s.metrics.HTTPServerRequests.WithLabelValues(route, strconv.Itoa(observed.status)).Inc()
			s.metrics.HTTPServerDuration.WithLabelValues(route).Observe(time.Since(start).Seconds())
		}()
		id := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if !ids.Valid(id) {
			id = ids.New()
		}
		r.Header.Set("X-Request-ID", id)
		w.Header().Set("X-Request-ID", id)
		securityHeaders(w)
		if s.handleCORS(w, r) {
			return
		}
		if !isSSERequest(r) {
			select {
			case s.bulkhead <- struct{}{}:
				defer func() { <-s.bulkhead }()
			default:
				w.Header().Set("Retry-After", "1")
				httpx.WriteProblem(w, problem(r, http.StatusServiceUnavailable, "Service busy", "Request concurrency limit reached; retry shortly."))
				return
			}
		}
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.Error("HTTP panic recovered", "request_id", id, "path", routeTemplate(r.URL.Path))
				httpx.WriteProblem(w, problem(r, http.StatusInternalServerError, "Internal error", "An unexpected error occurred."))
			}
			slog.Info("request completed", "request_id", id, "method", r.Method, "path", routeTemplate(r.URL.Path), "duration_ms", time.Since(start).Milliseconds())
		}()
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		ip := s.clientIP(r)
		if !s.enforceRateLimit(w, r, prefixedKeys("ip:", s.auth.abusePseudonyms("rate-ip", ip)), s.config.IPRequestsPerMinute) {
			return
		}
		identity, err := s.auth.authenticate(r, ip)
		if err != nil {
			w.Header().Set("WWW-Authenticate", `Bearer realm="greenroute"`)
			httpx.WriteProblem(w, problem(r, http.StatusUnauthorized, "Unauthorized", "A valid short-lived access token is required."))
			return
		}
		if !s.enforceRateLimit(w, r, prefixedKeys("user:", identity.AbuseSubjectIDs), s.config.UserRequestsPerMinute) {
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/route-searches" {
			if !s.enforceRateLimit(w, r, prefixedKeys("search:", identity.AbuseSubjectIDs), s.config.SearchesPerMinute) {
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(withPrincipal(r.Context(), identity)))
	})
}

func (s *apiServer) enforceRateLimit(w http.ResponseWriter, r *http.Request, keys []string, limit int) bool {
	for _, key := range keys {
		allowed, err := s.state.Allow(r.Context(), key, limit, time.Minute)
		if err != nil {
			httpx.WriteProblem(w, problem(r, http.StatusServiceUnavailable, "Rate-limit state unavailable", "Request safety state is temporarily unavailable."))
			return false
		}
		if !allowed {
			s.writeRateLimit(w, r)
			return false
		}
	}
	return true
}

func prefixedKeys(prefix string, values []string) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = prefix + value
	}
	return result
}

func isSSERequest(r *http.Request) bool {
	return r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/route-searches/") && strings.HasSuffix(r.URL.Path, "/events")
}

type observedResponseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *observedResponseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.status = status
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *observedResponseWriter) Write(body []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

func (w *observedResponseWriter) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *observedResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (s *apiServer) handleCORS(w http.ResponseWriter, r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	allowed := false
	for _, configured := range s.config.CORSOrigins {
		if subtle.ConstantTimeCompare([]byte(origin), []byte(configured)) == 1 {
			allowed = true
			break
		}
	}
	if !allowed {
		httpx.WriteProblem(w, problem(r, http.StatusForbidden, "CORS origin denied", "Origin is not allowlisted."))
		return true
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.Header().Set("Access-Control-Expose-Headers", "Location, Idempotency-Replayed, X-Request-ID")
	w.Header().Add("Vary", "Origin")
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, Last-Event-ID, X-Request-ID")
		w.Header().Set("Access-Control-Max-Age", "600")
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	return false
}

func (s *apiServer) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	address, err := netip.ParseAddr(host)
	if err != nil {
		return "invalid"
	}
	if !s.isTrustedProxy(address) {
		return address.String()
	}
	// Walk the proxy chain from the socket peer towards the client. The first
	// untrusted hop is the effective client. Reading XFF from the left would let
	// a caller prepend an arbitrary address and bypass per-IP controls.
	current := address
	forwarded := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for index := len(forwarded) - 1; index >= 0; index-- {
		if !s.isTrustedProxy(current) {
			return current.String()
		}
		next, parseErr := netip.ParseAddr(strings.TrimSpace(forwarded[index]))
		if parseErr != nil {
			// Fail closed to the last authenticated hop on malformed chains.
			return current.String()
		}
		current = next.Unmap()
	}
	return current.String()
}

func (s *apiServer) isTrustedProxy(address netip.Addr) bool {
	for _, prefix := range s.proxyRanges {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func (s *apiServer) writeRateLimit(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Retry-After", "60")
	httpx.WriteProblem(w, problem(r, http.StatusTooManyRequests, "Rate limit exceeded", "Too many requests; retry after the indicated interval."))
}

func securityHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
	w.Header().Set("Permissions-Policy", "geolocation=(), camera=(), microphone=()")
	w.Header().Set("Cache-Control", "no-store")
}

func validIdempotencyKey(value string) bool {
	if len(value) < 8 || len(value) > 128 {
		return false
	}
	for _, character := range []byte(value) {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}

func requestID(r *http.Request) string { return r.Header.Get("X-Request-ID") }

func problem(r *http.Request, status int, title, detail string) httpx.Problem {
	return httpx.Problem{Type: "https://greenroute.local/problems/" + strconv.Itoa(status), Title: title, Status: status, Detail: detail, Instance: routeTemplate(r.URL.Path), CorrelationID: requestID(r)}
}

func writeRawJSON(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func writeNormalizedUpstreamProblem(w http.ResponseWriter, r *http.Request, status int) {
	switch status {
	case http.StatusNotFound:
		httpx.WriteProblem(w, problem(r, http.StatusNotFound, "Search not found", "The search expired, was deleted, or never existed."))
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		httpx.WriteProblem(w, problem(r, http.StatusUnprocessableEntity, "Request rejected", "The routing request could not be processed."))
	default:
		httpx.WriteProblem(w, problem(r, http.StatusServiceUnavailable, "Routing service unavailable", "The routing service is temporarily unavailable."))
	}
}

func routeTemplate(path string) string {
	switch path {
	case "/health/live", "/health/ready", "/metrics", "/api/v1/me", "/api/v1/route-searches", "/api/v1/geosuggest", "/api/v1/admin/overview":
		return path
	}
	const prefix = "/api/v1/route-searches/"
	if strings.HasPrefix(path, prefix) {
		remainder := strings.TrimPrefix(path, prefix)
		if !strings.Contains(remainder, "/") {
			return prefix + "{searchId}"
		}
		if strings.Count(remainder, "/") == 1 && strings.HasSuffix(remainder, "/events") {
			return prefix + "{searchId}/events"
		}
	}
	return "/unmatched"
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value]; !exists {
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	return result
}

func jsonMarshal(value any) ([]byte, error) { return json.Marshal(value) }
