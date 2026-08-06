package main

import (
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	providerModeYandex = "yandex"
	providerModeDGIS   = "2gis"
	providerModeStub   = "stub"

	addressProviderAuto   = "auto"
	addressProviderYandex = "yandex"
	addressProviderDGIS   = "2gis"
)

type Config struct {
	Environment                  string
	HTTPAddr                     string
	InternalAPIToken             string
	ProviderMode                 string
	AddressProviderMode          string
	YandexAPIKey                 string
	YandexRouterBaseURL          string
	YandexGeocoderAPIKey         string
	YandexGeocoderBaseURL        string
	YandexMaxResults             int
	DGISAPIKey                   string
	DGISRoutingBaseURL           string
	DGISMaxResults               int
	DGISRateLimitPerMinute       int
	DGISMonthlyLimit             int
	DGISGeocoderRatePerMinute    int
	DGISGeocoderLocationBias     string
	ConnectTimeout               time.Duration
	RequestTimeout               time.Duration
	ResponseHeaderTimeout        time.Duration
	BulkheadWaitTimeout          time.Duration
	MaxRetries                   int
	RetryBaseDelay               time.Duration
	RetryMaxDelay                time.Duration
	MaxConcurrency               int
	CircuitBreakerThreshold      int
	CircuitBreakerOpenDuration   time.Duration
	ShutdownTimeout              time.Duration
	MaxRequestBodyBytes          int64
	MaxProviderResponseBytes     int64
	ProviderCostPerBillableUnit  float64
	ProviderDataStorageAllowed   bool
	ProviderDataModificationOK   bool
	ExperimentalSourcesRequested bool
	StubScenario                 string
	StubDelay                    time.Duration
}

func LoadConfig() (Config, error) {
	environment, err := parseProviderEnvironment(os.Getenv("APP_ENV"))
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		Environment:                 environment,
		HTTPAddr:                    envString("PROVIDER_ADDR", envString("HTTP_ADDR", ":8082")),
		InternalAPIToken:            strings.TrimSpace(os.Getenv("INTERNAL_API_TOKEN")),
		ProviderMode:                strings.ToLower(envString("PROVIDER_MODE", providerModeYandex)),
		AddressProviderMode:         strings.ToLower(envString("ADDRESS_PROVIDER_MODE", addressProviderAuto)),
		YandexAPIKey:                firstNonEmptySecret("YANDEX_ROUTER_API_KEY", "YANDEX_API_KEY"),
		YandexRouterBaseURL:         envString("YANDEX_ROUTER_BASE_URL", "https://api.routing.yandex.net"),
		YandexGeocoderAPIKey:        firstNonEmptySecret("YANDEX_GEOCODER_API_KEY", "YANDEX_ROUTER_API_KEY", "YANDEX_API_KEY"),
		YandexGeocoderBaseURL:       envString("YANDEX_GEOCODER_BASE_URL", "https://geocode-maps.yandex.ru"),
		YandexMaxResults:            envInt("YANDEX_MAX_RESULTS", 3),
		DGISAPIKey:                  firstNonEmptySecret("DGIS_API_KEY"),
		DGISRoutingBaseURL:          envString("DGIS_ROUTING_BASE_URL", "https://routing.api.2gis.com"),
		DGISMaxResults:              envInt("DGIS_MAX_RESULTS", 3),
		// One green search issues the initial request plus up to five detour
		// probes. A gate below that rejects the tail of every search and trips
		// provider cooldowns for work the search legitimately intended to do.
		DGISRateLimitPerMinute: envInt("DGIS_RATE_LIMIT_PER_MINUTE", 12),
		DGISMonthlyLimit:            envInt("DGIS_MONTHLY_LIMIT", 1000),
		DGISGeocoderRatePerMinute:   envInt("DGIS_GEOCODER_RATE_LIMIT_PER_MINUTE", 600),
		DGISGeocoderLocationBias:    envString("DGIS_GEOCODER_LOCATION_BIAS", defaultDGISGeocoderLocationBias),
		ConnectTimeout:              envDuration("PROVIDER_CONNECT_TIMEOUT", 500*time.Millisecond),
		RequestTimeout:              envDuration("PROVIDER_REQUEST_TIMEOUT", 2500*time.Millisecond),
		ResponseHeaderTimeout:       envDuration("PROVIDER_RESPONSE_HEADER_TIMEOUT", 2*time.Second),
		BulkheadWaitTimeout:         envDuration("PROVIDER_BULKHEAD_WAIT_TIMEOUT", 100*time.Millisecond),
		MaxRetries:                  envInt("PROVIDER_MAX_RETRIES", 2),
		RetryBaseDelay:              envDuration("PROVIDER_RETRY_BASE_DELAY", 100*time.Millisecond),
		RetryMaxDelay:               envDuration("PROVIDER_RETRY_MAX_DELAY", time.Second),
		MaxConcurrency:              envInt("PROVIDER_MAX_CONCURRENCY", 32),
		CircuitBreakerThreshold:     envInt("PROVIDER_CB_FAILURE_THRESHOLD", 5),
		CircuitBreakerOpenDuration:  envDuration("PROVIDER_CB_OPEN_DURATION", 30*time.Second),
		ShutdownTimeout:             envDuration("PROVIDER_SHUTDOWN_TIMEOUT", 10*time.Second),
		MaxRequestBodyBytes:         envInt64("PROVIDER_MAX_REQUEST_BODY_BYTES", 128<<10),
		MaxProviderResponseBytes:    envInt64("PROVIDER_MAX_RESPONSE_BYTES", 4<<20),
		ProviderCostPerBillableUnit: envFloat("PROVIDER_COST_PER_BILLABLE_UNIT", 0),
		StubScenario:                strings.ToLower(envString("PROVIDER_STUB_SCENARIO", "normal")),
		StubDelay:                   envDuration("PROVIDER_STUB_DELAY", 4*time.Second),
	}

	if cfg.ProviderDataStorageAllowed, err = envBool("PROVIDER_DATA_STORAGE_ALLOWED", false); err != nil {
		return Config{}, err
	}
	if cfg.ProviderDataModificationOK, err = envBool("PROVIDER_DATA_MODIFICATION_ALLOWED", false); err != nil {
		return Config{}, err
	}
	if cfg.ExperimentalSourcesRequested, err = envBool("ALLOW_EXPERIMENTAL_PROVIDER_SOURCES", false); err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if c.ProviderMode != providerModeYandex && c.ProviderMode != providerModeDGIS && c.ProviderMode != providerModeStub {
		return fmt.Errorf("PROVIDER_MODE must be %q, %q, or %q", providerModeYandex, providerModeDGIS, providerModeStub)
	}
	if c.AddressProviderMode != addressProviderAuto && c.AddressProviderMode != addressProviderYandex && c.AddressProviderMode != addressProviderDGIS {
		return fmt.Errorf("ADDRESS_PROVIDER_MODE must be %q, %q, or %q", addressProviderAuto, addressProviderYandex, addressProviderDGIS)
	}
	if c.ProviderMode == providerModeDGIS && c.AddressProviderMode == addressProviderYandex && c.YandexGeocoderAPIKey == "" {
		return errors.New("YANDEX_GEOCODER_API_KEY is required when ADDRESS_PROVIDER_MODE=yandex")
	}
	if c.ProviderMode == providerModeYandex && c.AddressProviderMode == addressProviderDGIS {
		return errors.New("ADDRESS_PROVIDER_MODE=2gis is only supported with PROVIDER_MODE=2gis")
	}
	if c.Environment == "production" && len(c.InternalAPIToken) < 32 {
		return errors.New("INTERNAL_API_TOKEN of at least 32 characters is required in production")
	}
	if c.Environment == "production" && c.ProviderMode == providerModeStub {
		return errors.New("PROVIDER_MODE=stub is forbidden in production")
	}
	if c.ProviderMode == providerModeYandex && c.YandexAPIKey == "" {
		return errors.New("YANDEX_ROUTER_API_KEY (or YANDEX_API_KEY alias) is required when PROVIDER_MODE=yandex")
	}
	if c.ProviderMode == providerModeDGIS && c.DGISAPIKey == "" {
		return errors.New("DGIS_API_KEY is required when PROVIDER_MODE=2gis")
	}
	if c.YandexRouterBaseURL != "https://api.routing.yandex.net" {
		return errors.New("YANDEX_ROUTER_BASE_URL must be the fixed official https://api.routing.yandex.net endpoint")
	}
	if c.YandexGeocoderBaseURL != "https://geocode-maps.yandex.ru" {
		return errors.New("YANDEX_GEOCODER_BASE_URL must be the fixed official https://geocode-maps.yandex.ru endpoint")
	}
	if c.YandexMaxResults < 1 || c.YandexMaxResults > 3 {
		return errors.New("YANDEX_MAX_RESULTS must be between 1 and 3")
	}
	if c.DGISRoutingBaseURL != "" && c.DGISRoutingBaseURL != "https://routing.api.2gis.com" {
		return errors.New("DGIS_ROUTING_BASE_URL must be the fixed official https://routing.api.2gis.com endpoint")
	}
	if c.ProviderMode == providerModeDGIS && c.DGISRoutingBaseURL == "" {
		return errors.New("DGIS_ROUTING_BASE_URL must be configured with the fixed official endpoint")
	}
	if c.DGISMaxResults < 1 || c.DGISMaxResults > 3 {
		return errors.New("DGIS_MAX_RESULTS must be between 1 and 3")
	}
	if c.DGISRateLimitPerMinute < 1 || c.DGISRateLimitPerMinute > 100_000 {
		return errors.New("DGIS_RATE_LIMIT_PER_MINUTE must be between 1 and 100000")
	}
	if c.DGISMonthlyLimit < 1 || c.DGISMonthlyLimit > 100_000_000 {
		return errors.New("DGIS_MONTHLY_LIMIT must be between 1 and 100000000")
	}
	if c.DGISGeocoderRatePerMinute < 1 || c.DGISGeocoderRatePerMinute > 100_000 {
		return errors.New("DGIS_GEOCODER_RATE_LIMIT_PER_MINUTE must be between 1 and 100000")
	}
	if _, err := normalizeDGISLocationBias(c.DGISGeocoderLocationBias); err != nil {
		return fmt.Errorf("DGIS_GEOCODER_LOCATION_BIAS must be a valid lon,lat pair: %w", err)
	}
	if c.ConnectTimeout <= 0 || c.RequestTimeout <= 0 || c.ResponseHeaderTimeout <= 0 {
		return errors.New("provider timeouts must be positive")
	}
	if c.BulkheadWaitTimeout < 0 {
		return errors.New("PROVIDER_BULKHEAD_WAIT_TIMEOUT cannot be negative")
	}
	if c.MaxRetries < 0 || c.MaxRetries > 5 {
		return errors.New("PROVIDER_MAX_RETRIES must be between 0 and 5")
	}
	if c.RetryBaseDelay <= 0 || c.RetryMaxDelay < c.RetryBaseDelay {
		return errors.New("retry delays are invalid")
	}
	if c.MaxConcurrency < 1 || c.MaxConcurrency > 10_000 {
		return errors.New("PROVIDER_MAX_CONCURRENCY must be between 1 and 10000")
	}
	if c.CircuitBreakerThreshold < 1 {
		return errors.New("PROVIDER_CB_FAILURE_THRESHOLD must be positive")
	}
	if c.CircuitBreakerOpenDuration <= 0 || c.ShutdownTimeout <= 0 {
		return errors.New("circuit-breaker and shutdown durations must be positive")
	}
	if c.MaxRequestBodyBytes < 1024 || c.MaxProviderResponseBytes < 1024 {
		return errors.New("request and response byte limits must be at least 1024")
	}
	if math.IsNaN(c.ProviderCostPerBillableUnit) || math.IsInf(c.ProviderCostPerBillableUnit, 0) || c.ProviderCostPerBillableUnit < 0 {
		return errors.New("PROVIDER_COST_PER_BILLABLE_UNIT must be a non-negative number")
	}
	switch c.StubScenario {
	case "normal", "slow", "rate_limit", "outage":
	default:
		return errors.New("PROVIDER_STUB_SCENARIO must be normal, slow, rate_limit, or outage")
	}
	if c.StubDelay < 0 {
		return errors.New("PROVIDER_STUB_DELAY cannot be negative")
	}
	if c.ProviderDataModificationOK && !c.ProviderDataStorageAllowed {
		return errors.New("PROVIDER_DATA_MODIFICATION_ALLOWED requires PROVIDER_DATA_STORAGE_ALLOWED")
	}
	return nil
}

func parseProviderEnvironment(raw string) (string, error) {
	switch value := strings.ToLower(strings.TrimSpace(raw)); value {
	case "", "development", "dev":
		return "development", nil
	case "local":
		return "local", nil
	case "test", "testing":
		return "test", nil
	case "stage", "staging":
		return "staging", nil
	case "prod", "production":
		return "production", nil
	default:
		return "", errors.New("APP_ENV must be one of local, development, test, staging, or production")
	}
}

func (c Config) HTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: c.ConnectTimeout, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		// Provider destinations are fixed in code. Ignoring ambient proxy variables
		// prevents an injected proxy from receiving the server-side API key.
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          c.MaxConcurrency * 2,
		MaxIdleConnsPerHost:   c.MaxConcurrency,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   c.ConnectTimeout,
		ResponseHeaderTimeout: c.ResponseHeaderTimeout,
		ExpectContinueTimeout: time.Second,
	}
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			// Never follow provider redirects: the only permitted destinations are
			// the fixed HTTPS hosts compiled into the adapter.
			return http.ErrUseLastResponse
		},
	}
}

func envString(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func firstNonEmptySecret(names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

func envDuration(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return -1
	}
	return parsed
}

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return -1
	}
	return parsed
}

func envInt64(name string, fallback int64) int64 {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return -1
	}
	return parsed
}

func envFloat(name string, fallback float64) float64 {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return math.NaN()
	}
	return parsed
}

func envBool(name string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean", name)
	}
	return parsed, nil
}
