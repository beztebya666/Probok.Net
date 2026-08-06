package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/greenroute/greenroute/internal/contracts"
)

type apiServer struct {
	cfg      Config
	provider Provider
	metrics  *serviceMetrics
	logger   *slog.Logger
	now      func() time.Time
}

func newAPIServer(cfg Config, provider Provider, metrics *serviceMetrics, logger *slog.Logger) http.Handler {
	server := &apiServer{cfg: cfg, provider: provider, metrics: metrics, logger: logger, now: time.Now}
	mux := http.NewServeMux()
	mux.HandleFunc("/health/live", server.handleLive)
	mux.HandleFunc("/health/ready", server.handleReady)
	mux.HandleFunc("/healthz", server.handleLive)
	mux.HandleFunc("/readyz", server.handleReady)
	mux.HandleFunc("/metrics", server.handleMetrics)
	mux.HandleFunc("/internal/v1/capabilities", server.handleCapabilities)
	mux.HandleFunc("/internal/v1/routes", server.handleRoutes)
	mux.HandleFunc("/internal/v1/geosuggest", server.handleSuggest)
	return server.middleware(mux)
}

func (s *apiServer) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		s.metrics.httpRequests.Add(1)
		requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if !requestIDPattern.MatchString(requestID) {
			requestID = newRequestID()
		}
		w.Header().Set("X-Request-ID", requestID)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		defer func() {
			if recovered := recover(); recovered != nil {
				s.metrics.httpErrors.Add(1)
				s.logger.Error("request panic recovered", "request_id", requestID, "method", r.Method, "path", r.URL.Path, "panic_type", fmt.Sprintf("%T", recovered), "stack", string(debug.Stack()))
				writeError(recorder, requestID, serviceError("INTERNAL_ERROR", "internal server error", http.StatusInternalServerError, false, nil))
			}
			if recorder.status >= 400 {
				s.metrics.httpErrors.Add(1)
			}
			s.logger.Info("request completed",
				"request_id", requestID,
				"method", r.Method,
				"path", r.URL.Path,
				"status", recorder.status,
				"duration_ms", time.Since(started).Milliseconds(),
			)
		}()
		if strings.HasPrefix(r.URL.Path, "/internal/") && !s.authorized(r) {
			recorder.Header().Set("WWW-Authenticate", "Bearer")
			writeError(recorder, requestID, serviceError("UNAUTHORIZED", "valid internal service credentials are required", http.StatusUnauthorized, false, nil))
			return
		}

		next.ServeHTTP(recorder, r)
	})
}

func (s *apiServer) authorized(r *http.Request) bool {
	if s.cfg.InternalAPIToken == "" {
		return true
	}
	expected := "Bearer " + s.cfg.InternalAPIToken
	provided := r.Header.Get("Authorization")
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

func (s *apiServer) handleLive(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *apiServer) handleReady(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	if err := s.provider.Ready(); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *apiServer) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	s.metrics.writePrometheus(w)
}

func (s *apiServer) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	writeJSON(w, http.StatusOK, s.provider.Capabilities())
}

func (s *apiServer) handleRoutes(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	requestID := w.Header().Get("X-Request-ID")
	var input contracts.ProviderRouteInput
	if err := decodeJSONBody(w, r, s.cfg.MaxRequestBodyBytes, &input); err != nil {
		writeError(w, requestID, serviceError("INVALID_JSON", "request body must be one valid JSON object", http.StatusBadRequest, false, err))
		return
	}
	request, missing := input.Request()
	if missing != "" {
		writeError(w, requestID, serviceError("VALIDATION_ERROR", "required property "+missing+" is missing", http.StatusBadRequest, false, nil))
		return
	}
	if err := validateRouteRequest(request, s.provider.Capabilities().MaxAlternatives, s.now()); err != nil {
		writeError(w, requestID, serviceError("VALIDATION_ERROR", err.Error(), http.StatusBadRequest, false, err))
		return
	}

	deadline := time.Duration(request.DeadlineMS) * time.Millisecond
	ctx, cancel := context.WithTimeout(r.Context(), deadline)
	defer cancel()
	response, err := s.provider.Routes(ctx, request)
	if err != nil {
		writeError(w, requestID, normalizeProviderError(err))
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *apiServer) handleSuggest(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	requestID := w.Header().Get("X-Request-ID")
	limit := 7
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, requestID, serviceError("VALIDATION_ERROR", "limit must be an integer between 1 and 10", http.StatusBadRequest, false, err))
			return
		}
		limit = parsed
	}
	query := r.URL.Query().Get("q")
	language := r.URL.Query().Get("lang")
	if language == "" {
		language = "ru_RU"
	}
	if err := validateSuggestQuery(query, language, limit); err != nil {
		writeError(w, requestID, serviceError("VALIDATION_ERROR", err.Error(), http.StatusBadRequest, false, err))
		return
	}
	deadline := s.cfg.RequestTimeout*time.Duration(s.cfg.MaxRetries+1) + s.cfg.RetryMaxDelay*time.Duration(s.cfg.MaxRetries)
	ctx, cancel := context.WithTimeout(r.Context(), deadline)
	defer cancel()
	response, err := s.provider.Suggest(ctx, query, language, limit)
	if err != nil {
		writeError(w, requestID, normalizeProviderError(err))
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func requireMethod(w http.ResponseWriter, r *http.Request, expected string) bool {
	if r.Method == expected {
		return true
	}
	w.Header().Set("Allow", expected)
	writeJSON(w, http.StatusMethodNotAllowed, map[string]any{
		"error": map[string]any{"code": "METHOD_NOT_ALLOWED", "message": "method not allowed", "retryable": false},
	})
	return false
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, limit int64, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func writeError(w http.ResponseWriter, requestID string, err *providerError) {
	if err.HTTPStatus == 0 {
		err.HTTPStatus = http.StatusInternalServerError
	}
	if err.RetryAfter > 0 {
		seconds := int64(math.Ceil(err.RetryAfter.Seconds()))
		if seconds > 0 {
			w.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
		}
	}
	writeJSON(w, err.HTTPStatus, map[string]any{
		"requestId": requestID,
		"error": map[string]any{
			"code": err.Code, "message": err.Message, "retryable": err.Retryable,
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func newRequestID() string {
	var bytes [12]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "req-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return "req-" + hex.EncodeToString(bytes[:])
}

type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.wroteHeader {
		return
	}
	r.status = status
	r.wroteHeader = true
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(body []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(body)
}
