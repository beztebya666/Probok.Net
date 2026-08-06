package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/greenroute/greenroute/internal/contracts"
	"github.com/greenroute/greenroute/internal/domain"
	"github.com/greenroute/greenroute/internal/httpx"
	"github.com/greenroute/greenroute/internal/ids"
	"github.com/greenroute/greenroute/internal/searchstore"
	"github.com/greenroute/greenroute/internal/telemetry"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type apiServer struct {
	config config
	engine *engine
	store  searchstore.Store
}

var operationKeyPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func newAPIServer(cfg config, engine *engine, store searchstore.Store) http.Handler {
	server := &apiServer{config: cfg, engine: engine, store: store}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", server.live)
	mux.HandleFunc("GET /health/ready", server.ready)
	mux.Handle("GET /metrics", engine.metrics.Handler())
	mux.HandleFunc("POST /internal/v1/searches", server.start)
	mux.HandleFunc("GET /internal/v1/searches/{searchID}", server.get)
	mux.HandleFunc("DELETE /internal/v1/searches/{searchID}", server.delete)
	mux.HandleFunc("GET /internal/v1/searches/{searchID}/events", server.events)
	mux.HandleFunc("GET /internal/v1/admin/overview", server.adminOverview)
	return requestMiddleware(cfg, otelhttp.NewHandler(mux, "routing-orchestrator-http", otelhttp.WithFilter(telemetry.QuerylessRequestFilter)))
}

func (s *apiServer) live(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "routing-orchestrator"})
}

func (s *apiServer) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 800*time.Millisecond)
	defer cancel()
	checks := map[string]string{"store": "up", "provider": "up", "capabilities": "up"}
	status := http.StatusOK
	if err := s.store.Ping(ctx); err != nil {
		checks["store"] = "down"
		status = http.StatusServiceUnavailable
	}
	if err := s.engine.provider.health(ctx); err != nil {
		checks["provider"] = "down"
		status = http.StatusServiceUnavailable
	}
	if _, ready := s.engine.capabilitySnapshot(); !ready {
		checks["capabilities"] = "down"
		status = http.StatusServiceUnavailable
	}
	httpx.WriteJSON(w, status, map[string]interface{}{"status": map[bool]string{true: "ready", false: "not_ready"}[status == http.StatusOK], "checks": checks})
}

func (s *apiServer) start(w http.ResponseWriter, r *http.Request) {
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
	request.ApplySafeDefaults()
	if err := request.Validate(domain.DefaultValidationLimits()); err != nil {
		httpx.WriteProblem(w, problem(r, http.StatusUnprocessableEntity, "Route search rejected", err.Error()))
		return
	}
	operationKey := strings.TrimSpace(r.Header.Get("X-Operation-Key"))
	if operationKey != "" && !operationKeyPattern.MatchString(operationKey) {
		httpx.WriteProblem(w, problem(r, http.StatusBadRequest, "Invalid operation key", "X-Operation-Key must be a 64-character lowercase hexadecimal value."))
		return
	}
	result, err := s.engine.start(request, operationKey)
	if err != nil {
		if errors.Is(err, errSearchCapacity) || errors.Is(err, errCapabilitiesUnavailable) {
			w.Header().Set("Retry-After", "1")
			httpx.WriteProblem(w, problem(r, http.StatusServiceUnavailable, "Route search temporarily unavailable", err.Error()))
			return
		}
		httpx.WriteProblem(w, problem(r, http.StatusServiceUnavailable, "Route search state unavailable", "The search could not be safely persisted."))
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, result)
}

func (s *apiServer) get(w http.ResponseWriter, r *http.Request) {
	if !validSearchID(w, r) {
		return
	}
	result, err := s.store.Get(r.Context(), r.PathValue("searchID"))
	if errors.Is(err, searchstore.ErrNotFound) {
		httpx.WriteProblem(w, problem(r, http.StatusNotFound, "Search not found", "The search expired, was deleted, or never existed."))
		return
	}
	if err != nil {
		httpx.WriteProblem(w, problem(r, http.StatusServiceUnavailable, "Search state unavailable", "Search state is temporarily unavailable."))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (s *apiServer) delete(w http.ResponseWriter, r *http.Request) {
	if !validSearchID(w, r) {
		return
	}
	err := s.engine.cancel(r.PathValue("searchID"))
	if errors.Is(err, searchstore.ErrNotFound) {
		httpx.WriteProblem(w, problem(r, http.StatusNotFound, "Search not found", "The search expired, was deleted, or never existed."))
		return
	}
	if err != nil {
		httpx.WriteProblem(w, problem(r, http.StatusServiceUnavailable, "Search deletion failed", "Search state could not be deleted."))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *apiServer) events(w http.ResponseWriter, r *http.Request) {
	if !validSearchID(w, r) {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpx.WriteProblem(w, problem(r, http.StatusInternalServerError, "Streaming unsupported", "Response streaming is unavailable."))
		return
	}
	after := int64(0)
	lastID := r.Header.Get("Last-Event-ID")
	if query := r.URL.Query().Get("after"); query != "" {
		lastID = query
	}
	if lastID != "" {
		parsed, err := strconv.ParseInt(lastID, 10, 64)
		if err != nil || parsed < 0 {
			httpx.WriteProblem(w, problem(r, http.StatusBadRequest, "Invalid event cursor", "Last-Event-ID must be a non-negative integer."))
			return
		}
		after = parsed
	}
	if _, err := s.store.Get(r.Context(), r.PathValue("searchID")); err != nil {
		if errors.Is(err, searchstore.ErrNotFound) {
			httpx.WriteProblem(w, problem(r, http.StatusNotFound, "Search not found", "The event stream expired or was deleted."))
		} else {
			httpx.WriteProblem(w, problem(r, http.StatusServiceUnavailable, "Event stream unavailable", "Event state is temporarily unavailable."))
		}
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	poll := time.NewTicker(250 * time.Millisecond)
	heartbeat := time.NewTicker(s.config.SSEHeartbeat)
	defer poll.Stop()
	defer heartbeat.Stop()
	for {
		events, err := s.store.EventsAfter(r.Context(), r.PathValue("searchID"), after)
		if err != nil {
			return
		}
		for _, event := range events {
			payload, err := json.Marshal(event)
			if err != nil {
				continue
			}
			_, _ = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.EventID, event.Type, payload)
			after = event.EventID
			flusher.Flush()
		}
		result, err := s.store.Get(r.Context(), r.PathValue("searchID"))
		if err != nil {
			return
		}
		if terminal(result.Status) && len(events) == 0 {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-poll.C:
		case <-heartbeat.C:
			_, _ = fmt.Fprintf(w, ": heartbeat %d\n\n", time.Now().Unix())
			flusher.Flush()
		}
	}
}

func (s *apiServer) adminOverview(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 800*time.Millisecond)
	defer cancel()
	providerStatus := "UP"
	if err := s.engine.provider.health(ctx); err != nil {
		providerStatus = "DOWN"
	}
	total := s.engine.stats.searches.Load()
	degraded := s.engine.stats.degraded.Load()
	degradedPercent := 0.0
	if total > 0 {
		degradedPercent = float64(degraded) / float64(total) * 100
	}
	capabilities, _ := s.engine.capabilitySnapshot()
	apiIntegrations := capabilities.APIIntegrations
	if apiIntegrations == nil {
		apiIntegrations = []contracts.APIIntegration{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"provider": map[string]interface{}{
			"name": capabilities.Provider, "status": providerStatus, "mode": capabilities.Mode,
			"requestCount": s.engine.stats.providerCalls.Load(), "estimatedBillableUnits": s.engine.stats.billingUnits.Load(), "circuitBreaker": "MANAGED_BY_PROVIDER_ADAPTER",
		},
		"apiIntegrations": apiIntegrations,
		"scoringPolicy":   s.engine.scoring,
		"searches": map[string]interface{}{
			"total": total, "degradedPercent": degradedPercent, "lowConfidenceCount": s.engine.stats.lowConfidence.Load(), "searchBudgetExhaustion": s.engine.stats.budgetExceeded.Load(),
		},
		"featureFlags": map[string]bool{
			"ENABLE_ENHANCED_SEARCH": s.config.EnableEnhancedSearch, "ENABLE_AVOID_ZONE_GENERATION": s.config.EnableAvoidZones,
			"ENABLE_CORRIDOR_ANCHORS": s.config.EnableCorridorAnchors, "ENABLE_ROUTE_RERANKING": s.config.EnableReranking,
		},
	})
}

func requestMiddleware(cfg config, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if !ids.Valid(requestID) {
			requestID = ids.New()
		}
		r.Header.Set("X-Request-ID", requestID)
		w.Header().Set("X-Request-ID", requestID)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(r.URL.Path, "/internal/") && cfg.InternalToken != "" {
			expected := []byte("Bearer " + cfg.InternalToken)
			provided := []byte(r.Header.Get("Authorization"))
			if len(expected) != len(provided) || subtle.ConstantTimeCompare(expected, provided) != 1 {
				httpx.WriteProblem(w, problem(r, http.StatusUnauthorized, "Unauthorized", "A valid internal service credential is required."))
				return
			}
		}
		started := time.Now()
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.Error("HTTP handler panic recovered", "request_id", requestID, "path", routeTemplate(r.URL.Path))
				httpx.WriteProblem(w, problem(r, http.StatusInternalServerError, "Internal error", "An unexpected error occurred."))
			}
			slog.Info("request completed", "request_id", requestID, "method", r.Method, "path", routeTemplate(r.URL.Path), "duration_ms", time.Since(started).Milliseconds())
		}()
		next.ServeHTTP(w, r)
	})
}

func problem(r *http.Request, status int, title, detail string) httpx.Problem {
	return httpx.Problem{Type: "https://greenroute.local/problems/" + strconv.Itoa(status), Title: title, Status: status, Detail: detail, Instance: routeTemplate(r.URL.Path), CorrelationID: r.Header.Get("X-Request-ID")}
}

func terminal(status domain.SearchStatus) bool {
	return status == domain.SearchCompleted || status == domain.SearchDegraded || status == domain.SearchFailed || status == domain.SearchCancelled
}

func routeTemplate(path string) string {
	switch path {
	case "/health/live", "/health/ready", "/metrics", "/internal/v1/searches", "/internal/v1/admin/overview":
		return path
	}
	const prefix = "/internal/v1/searches/"
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

func validSearchID(w http.ResponseWriter, r *http.Request) bool {
	if ids.Valid(r.PathValue("searchID")) {
		return true
	}
	httpx.WriteProblem(w, problem(r, http.StatusBadRequest, "Invalid search identifier", "searchId must be a canonical UUID."))
	return false
}
