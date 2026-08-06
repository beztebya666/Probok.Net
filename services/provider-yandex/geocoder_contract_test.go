package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNormalizeGeocoderResponsePreservesOfficialCoordinateOrder(t *testing.T) {
	var wire yandexGeocoderResponse
	if err := json.Unmarshal(readGeocoderContractFixture(t), &wire); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}

	result, err := normalizeGeocoderResponse(wire)
	if err != nil {
		t.Fatalf("normalize response: %v", err)
	}
	if len(result.Suggestions) != 2 {
		t.Fatalf("got %d suggestions, want 2", len(result.Suggestions))
	}
	first := result.Suggestions[0]
	if first.ID != "ymapsbm1://geo?data=synthetic-one" || first.Label != "Тверская улица, 1" || first.Subtitle != "Москва, Россия" {
		t.Fatalf("unexpected first suggestion: %+v", first)
	}
	if first.Point.Longitude != 37.617635 || first.Point.Latitude != 55.755814 {
		t.Fatalf("Point.pos longitude/latitude order changed: %+v", first.Point)
	}
}

func TestBuildYandexGeocoderURLIsFixedAndComplete(t *testing.T) {
	cfg := testConfig()
	cfg.YandexGeocoderAPIKey = "geocoder-server-secret"
	raw, err := buildYandexGeocoderURL(cfg, "Москва, Тверская 1", "ru_RU", 3)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "https" || parsed.Host != "geocode-maps.yandex.ru" || parsed.Path != "/v1/" {
		t.Fatalf("unsafe endpoint: %s", parsed.Redacted())
	}
	query := parsed.Query()
	if query.Get("apikey") != "geocoder-server-secret" || query.Get("geocode") != "Москва, Тверская 1" || query.Get("lang") != "ru_RU" || query.Get("results") != "3" || query.Get("format") != "json" {
		t.Fatalf("unexpected query: %#v", query)
	}
}

func TestYandexGeocoderRetries500ThenNormalizesFixture(t *testing.T) {
	fixture := string(readGeocoderContractFixture(t))
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.URL.Host != "geocode-maps.yandex.ru" {
			t.Fatalf("unexpected provider host: %s", request.URL.Redacted())
		}
		if calls == 1 {
			return response(http.StatusInternalServerError, `{}`), nil
		}
		return response(http.StatusOK, fixture), nil
	})
	metrics := &serviceMetrics{}
	adapter := newYandexAdapter(testConfig(), &http.Client{Transport: transport}, metrics)
	adapter.sleep = func(context.Context, time.Duration) error { return nil }

	result, err := adapter.Suggest(context.Background(), "Тверская 1", "ru_RU", 2)
	if err != nil {
		t.Fatalf("geocoder failed: %v", err)
	}
	if calls != 2 || len(result.Suggestions) != 2 {
		t.Fatalf("calls=%d suggestions=%d, want 2 and 2", calls, len(result.Suggestions))
	}
	if metrics.geocoderRequests.Load() != 2 || metrics.providerSuccesses.Load() != 1 {
		t.Fatalf("unexpected geocoder metrics: attempts=%d successes=%d", metrics.geocoderRequests.Load(), metrics.providerSuccesses.Load())
	}
}

func TestYandexGeocoderSanitizes403AndDoesNotRetry(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return response(http.StatusForbidden, `{"message":"secret upstream entitlement details"}`), nil
	})
	adapter := newYandexAdapter(testConfig(), &http.Client{Transport: transport}, &serviceMetrics{})
	adapter.sleep = func(context.Context, time.Duration) error {
		t.Fatal("non-retryable 403 attempted to sleep")
		return nil
	}

	_, err := adapter.Suggest(context.Background(), "Тверская 1", "ru_RU", 1)
	providerErr := normalizeProviderError(err)
	if err == nil || providerErr.Code != "PROVIDER_AUTHENTICATION_FAILED" {
		t.Fatalf("got %v, want authentication failure", err)
	}
	if strings.Contains(providerErr.Message, "entitlement") || strings.Contains(providerErr.Message, "secret") {
		t.Fatalf("raw provider error leaked: %q", providerErr.Message)
	}
	if calls != 1 {
		t.Fatalf("got %d calls, want 1", calls)
	}
}

func TestYandexGeocoderHonorsRetryAfter(t *testing.T) {
	fixture := string(readGeocoderContractFixture(t))
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			result := response(http.StatusTooManyRequests, `{}`)
			result.Header.Set("Retry-After", "2")
			return result, nil
		}
		return response(http.StatusOK, fixture), nil
	})
	metrics := &serviceMetrics{}
	adapter := newYandexAdapter(testConfig(), &http.Client{Transport: transport}, metrics)
	adapter.cooldownJitter = func(time.Duration) time.Duration { return 0 }
	var slept time.Duration
	adapter.sleep = func(_ context.Context, duration time.Duration) error {
		slept = duration
		return nil
	}

	if _, err := adapter.Suggest(context.Background(), "Тверская 1", "ru_RU", 1); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || slept != 2*time.Second || metrics.provider429.Load() != 1 {
		t.Fatalf("calls=%d slept=%s rate_limited=%d", calls, slept, metrics.provider429.Load())
	}
}

func TestGeosuggestHTTPBoundaryUsesOfficialGeocoderAndCapsResults(t *testing.T) {
	fixture := string(readGeocoderContractFixture(t))
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		query := request.URL.Query()
		if query.Get("geocode") != "Тверская 1" || query.Get("lang") != "en_US" || query.Get("results") != "1" {
			t.Fatalf("unexpected upstream query: %#v", query)
		}
		return response(http.StatusOK, fixture), nil
	})
	cfg := testConfig()
	metrics := &serviceMetrics{}
	adapter := newYandexAdapter(cfg, &http.Client{Transport: transport}, metrics)
	handler := newAPIServer(cfg, adapter, metrics, slog.New(slog.NewJSONHandler(io.Discard, nil)))

	request := httptest.NewRequest(http.MethodGet, "/internal/v1/geosuggest?q="+url.QueryEscape("Тверская 1")+"&lang=en_US&limit=1", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("geocoder response is cacheable")
	}
	var result struct {
		Suggestions []json.RawMessage `json:"suggestions"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Suggestions) != 1 {
		t.Fatalf("got %d suggestions, want provider response capped to 1", len(result.Suggestions))
	}
}

func TestYandexGeocoderRejectsOnlyInvalidPointBearingObjects(t *testing.T) {
	var wire yandexGeocoderResponse
	if err := json.Unmarshal(readGeocoderContractFixture(t), &wire); err != nil {
		t.Fatal(err)
	}
	for index := range wire.Response.GeoObjectCollection.FeatureMember {
		wire.Response.GeoObjectCollection.FeatureMember[index].GeoObject.Point.Position = "not-a-point"
	}
	_, err := normalizeGeocoderResponse(wire)
	if err == nil || normalizeProviderError(err).Code != "PROVIDER_RESPONSE_INVALID" {
		t.Fatalf("got %v, want invalid provider response", err)
	}
}

func TestYandexGeocoderAcceptsNoMatches(t *testing.T) {
	result, err := normalizeGeocoderResponse(yandexGeocoderResponse{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Suggestions) != 0 {
		t.Fatalf("got %d suggestions, want none", len(result.Suggestions))
	}
}

func readGeocoderContractFixture(t *testing.T) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "tests", "contract", "provider-yandex", "yandex-geocoder-v1.synthetic.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}
