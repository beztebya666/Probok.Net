package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestProductionDefaultFailsClosedWithoutYandexKey(t *testing.T) {
	environment := []string{
		"APP_ENV", "INTERNAL_API_TOKEN", "PROVIDER_MODE", "ADDRESS_PROVIDER_MODE", "YANDEX_API_KEY", "YANDEX_ROUTER_API_KEY", "YANDEX_ROUTER_BASE_URL", "YANDEX_GEOCODER_API_KEY", "YANDEX_GEOCODER_BASE_URL", "YANDEX_MAX_RESULTS", "PROVIDER_CONNECT_TIMEOUT",
		"DGIS_API_KEY", "DGIS_ROUTING_BASE_URL", "DGIS_MAX_RESULTS", "DGIS_RATE_LIMIT_PER_MINUTE", "DGIS_MONTHLY_LIMIT", "DGIS_GEOCODER_RATE_LIMIT_PER_MINUTE", "DGIS_GEOCODER_LOCATION_BIAS",
		"PROVIDER_REQUEST_TIMEOUT", "PROVIDER_RESPONSE_HEADER_TIMEOUT", "PROVIDER_BULKHEAD_WAIT_TIMEOUT",
		"PROVIDER_MAX_RETRIES", "PROVIDER_RETRY_BASE_DELAY", "PROVIDER_RETRY_MAX_DELAY",
		"PROVIDER_MAX_CONCURRENCY", "PROVIDER_CB_FAILURE_THRESHOLD", "PROVIDER_CB_OPEN_DURATION",
		"PROVIDER_SHUTDOWN_TIMEOUT", "PROVIDER_MAX_REQUEST_BODY_BYTES", "PROVIDER_MAX_RESPONSE_BYTES",
		"PROVIDER_COST_PER_BILLABLE_UNIT",
		"PROVIDER_DATA_STORAGE_ALLOWED", "PROVIDER_DATA_MODIFICATION_ALLOWED", "ALLOW_EXPERIMENTAL_PROVIDER_SOURCES",
		"PROVIDER_STUB_SCENARIO", "PROVIDER_STUB_DELAY",
	}
	for _, name := range environment {
		t.Setenv(name, "")
	}
	if _, err := LoadConfig(); err == nil {
		t.Fatal("default yandex mode started without a key")
	}
}

func TestProductionRequiresInternalToken(t *testing.T) {
	cfg := testConfig()
	cfg.Environment = "production"
	cfg.InternalAPIToken = "short"
	if err := cfg.Validate(); err == nil {
		t.Fatal("production accepted a weak internal API token")
	}
}

func TestExplicitStubModeNeedsNoKey(t *testing.T) {
	t.Setenv("PROVIDER_MODE", "stub")
	t.Setenv("YANDEX_API_KEY", "")
	t.Setenv("YANDEX_ROUTER_API_KEY", "")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProviderMode != providerModeStub {
		t.Fatalf("mode=%s", cfg.ProviderMode)
	}
}

func TestProductionForbidsStubProvider(t *testing.T) {
	cfg := testConfig()
	cfg.Environment = "production"
	cfg.InternalAPIToken = "internal-token-that-is-at-least-32-bytes"
	cfg.ProviderMode = providerModeStub
	if err := cfg.Validate(); err == nil {
		t.Fatal("production accepted the synthetic stub provider")
	}
}

func TestUnknownEnvironmentFailsClosed(t *testing.T) {
	t.Setenv("APP_ENV", "prodution")
	t.Setenv("PROVIDER_MODE", "stub")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("unknown APP_ENV was silently treated as development")
	}
}

func TestRouterBaseURLCannotRedirectProviderEgress(t *testing.T) {
	cfg := testConfig()
	cfg.YandexRouterBaseURL = "http://127.0.0.1:8080"
	if err := cfg.Validate(); err == nil {
		t.Fatal("non-official router base URL was accepted")
	}
}

func TestGeocoderBaseURLCannotRedirectProviderEgress(t *testing.T) {
	cfg := testConfig()
	cfg.YandexGeocoderBaseURL = "http://169.254.169.254"
	if err := cfg.Validate(); err == nil {
		t.Fatal("non-official geocoder base URL was accepted")
	}
}

func TestDedicatedGeocoderKeyOverridesRouterKey(t *testing.T) {
	t.Setenv("PROVIDER_MODE", "yandex")
	t.Setenv("YANDEX_ROUTER_API_KEY", "router-key")
	t.Setenv("YANDEX_GEOCODER_API_KEY", "geocoder-key")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.YandexAPIKey != "router-key" || cfg.YandexGeocoderAPIKey != "geocoder-key" {
		t.Fatalf("unexpected key selection: router=%q geocoder=%q", cfg.YandexAPIKey, cfg.YandexGeocoderAPIKey)
	}
}

func TestGeocoderKeyFallsBackToRouterKey(t *testing.T) {
	t.Setenv("PROVIDER_MODE", "yandex")
	t.Setenv("YANDEX_ROUTER_API_KEY", "shared-entitled-key")
	t.Setenv("YANDEX_GEOCODER_API_KEY", "")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.YandexGeocoderAPIKey != "shared-entitled-key" {
		t.Fatalf("geocoder fallback=%q", cfg.YandexGeocoderAPIKey)
	}
}

func TestDGISModeRequiresDedicatedServerKey(t *testing.T) {
	t.Setenv("PROVIDER_MODE", "2gis")
	t.Setenv("DGIS_API_KEY", "")
	if _, err := LoadConfig(); err == nil || !strings.Contains(err.Error(), "DGIS_API_KEY") {
		t.Fatalf("2GIS mode accepted missing key: %v", err)
	}
}

func TestDGISModeLoadsQuotaGuardrails(t *testing.T) {
	t.Setenv("PROVIDER_MODE", "2gis")
	t.Setenv("DGIS_API_KEY", "test-only-key")
	t.Setenv("DGIS_RATE_LIMIT_PER_MINUTE", "5")
	t.Setenv("DGIS_MONTHLY_LIMIT", "1000")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProviderMode != providerModeDGIS || cfg.DGISRateLimitPerMinute != 5 || cfg.DGISMonthlyLimit != 1000 {
		t.Fatalf("unexpected 2GIS config: mode=%s minute=%d monthly=%d", cfg.ProviderMode, cfg.DGISRateLimitPerMinute, cfg.DGISMonthlyLimit)
	}
}

func TestDGISBaseURLCannotRedirectProviderEgress(t *testing.T) {
	cfg := dgisTestConfig()
	cfg.DGISRoutingBaseURL = "http://169.254.169.254"
	if err := cfg.Validate(); err == nil {
		t.Fatal("non-official 2GIS router base URL was accepted")
	}
}

func TestDGISAddressProviderAutoPrefersDedicatedYandexGeocoder(t *testing.T) {
	cfg := dgisTestConfig()
	cfg.AddressProviderMode = addressProviderAuto
	cfg.YandexGeocoderAPIKey = "dedicated-yandex-geocoder-key"
	if got := resolveAddressProvider(cfg); got != addressProviderYandex {
		t.Fatalf("address provider=%q, want yandex", got)
	}
	cfg.YandexGeocoderAPIKey = ""
	if got := resolveAddressProvider(cfg); got != addressProviderDGIS {
		t.Fatalf("address provider=%q, want 2gis fallback", got)
	}
}

func TestExplicitYandexAddressProviderRequiresGeocoderKey(t *testing.T) {
	cfg := dgisTestConfig()
	cfg.AddressProviderMode = addressProviderYandex
	cfg.YandexGeocoderAPIKey = ""
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "YANDEX_GEOCODER_API_KEY") {
		t.Fatalf("missing Yandex geocoder key was accepted: %v", err)
	}
}

func TestDGISGeocoderLocationBiasValidation(t *testing.T) {
	for _, value := range []string{"", "37.6", "181,55", "37,91", "NaN,55"} {
		cfg := dgisTestConfig()
		cfg.DGISGeocoderLocationBias = value
		if err := cfg.Validate(); err == nil {
			t.Fatalf("invalid location bias %q was accepted", value)
		}
	}
}

func TestAmbientProxyIsIgnoredForProviderClient(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://attacker.invalid:8080")
	client := testConfig().HTTPClient()
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type %T", client.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("provider transport accepted an ambient proxy")
	}
}
