package main

import (
	"strings"
	"testing"
	"time"
)

var edgeConfigEnvironment = []string{
	"APP_ENV", "EDGE_ADDR", "ORCHESTRATOR_URL", "PROVIDER_URL", "INTERNAL_API_TOKEN", "REDIS_URL", "DATABASE_URL",
	"REQUIRE_REDIS", "REQUIRE_DATABASE", "ENABLE_ANONYMOUS_USAGE", "ENABLE_LOCAL_ANONYMOUS_ADMIN", "OIDC_ISSUER_URL", "OIDC_CLIENT_ID", "OIDC_ADMIN_GROUP",
	"CORS_ALLOWED_ORIGINS", "TRUSTED_PROXY_CIDRS", "RATE_LIMIT_IP_PER_MINUTE", "RATE_LIMIT_USER_PER_MINUTE",
	"RATE_LIMIT_SEARCH_PER_MINUTE", "MAX_CONCURRENT_EDGE_REQUESTS", "MAX_CONCURRENT_SSE", "MAX_SSE_PER_PRINCIPAL",
	"SSE_MAX_LIFETIME", "SSE_IDLE_TIMEOUT", "IDEMPOTENCY_TTL", "SEARCH_OWNERSHIP_TTL", "SHUTDOWN_GRACE_PERIOD", "AUDIT_HASH_KEY",
	"ABUSE_HASH_KEY", "ABUSE_PREVIOUS_HASH_KEY", "AUDIT_RETENTION", "AUDIT_PURGE_INTERVAL",
}

func prepareEdgeConfig(t *testing.T) {
	t.Helper()
	for _, name := range edgeConfigEnvironment {
		t.Setenv(name, "")
	}
	t.Setenv("ENABLE_ANONYMOUS_USAGE", "true")
}

func TestLoadConfigNormalizesEnvironment(t *testing.T) {
	tests := []struct {
		value string
		want  string
	}{
		{value: "Production", want: "production"},
		{value: " PROD ", want: "production"},
		{value: "DEV", want: "development"},
		{value: "Local", want: "local"},
		{value: "TESTING", want: "test"},
		{value: "Stage", want: "staging"},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			prepareEdgeConfig(t)
			t.Setenv("APP_ENV", tt.value)
			if tt.want == "production" {
				configureProductionEdge(t)
			}
			cfg, err := loadConfig()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Environment != tt.want {
				t.Fatalf("Environment=%q, want %q", cfg.Environment, tt.want)
			}
		})
	}
}

func TestProductionCaseInsensitiveFailsClosed(t *testing.T) {
	prepareEdgeConfig(t)
	t.Setenv("APP_ENV", "Production")
	_, err := loadConfig()
	if err == nil || !strings.Contains(err.Error(), "INTERNAL_API_TOKEN") {
		t.Fatalf("loadConfig() error=%v, want production token error", err)
	}
}

func TestLoadConfigRejectsUnknownEnvironment(t *testing.T) {
	prepareEdgeConfig(t)
	t.Setenv("APP_ENV", "preview-42")
	_, err := loadConfig()
	if err == nil || !strings.Contains(err.Error(), "APP_ENV") {
		t.Fatalf("loadConfig() error=%v, want APP_ENV error", err)
	}
}

func TestLoadConfigRejectsMalformedValues(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "REQUIRE_REDIS", value: "sometimes"},
		{name: "ENABLE_ANONYMOUS_USAGE", value: "yes-please"},
		{name: "ENABLE_LOCAL_ANONYMOUS_ADMIN", value: "yes-please"},
		{name: "RATE_LIMIT_IP_PER_MINUTE", value: "many"},
		{name: "MAX_CONCURRENT_SSE", value: "64.5"},
		{name: "SSE_MAX_LIFETIME", value: "half-hour"},
		{name: "SSE_IDLE_TIMEOUT", value: "forty-five-seconds"},
		{name: "IDEMPOTENCY_TTL", value: "900"},
		{name: "AUDIT_RETENTION", value: "ninety-days"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prepareEdgeConfig(t)
			t.Setenv(tt.name, tt.value)
			_, err := loadConfig()
			if err == nil || !strings.Contains(err.Error(), tt.name) {
				t.Fatalf("loadConfig() error=%v, want error naming %s", err, tt.name)
			}
		})
	}
}

func TestLoadConfigRejectsOutOfRangeValues(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		alsoName string
		also     string
	}{
		{name: "RATE_LIMIT_IP_PER_MINUTE", value: "0"},
		{name: "RATE_LIMIT_IP_PER_MINUTE", value: "1000001"},
		{name: "RATE_LIMIT_USER_PER_MINUTE", value: "0"},
		{name: "RATE_LIMIT_SEARCH_PER_MINUTE", value: "0"},
		{name: "MAX_CONCURRENT_EDGE_REQUESTS", value: "0"},
		{name: "MAX_CONCURRENT_EDGE_REQUESTS", value: "10001"},
		{name: "MAX_CONCURRENT_SSE", value: "0"},
		{name: "MAX_CONCURRENT_SSE", value: "1001"},
		{name: "MAX_CONCURRENT_SSE", value: "64", alsoName: "MAX_CONCURRENT_EDGE_REQUESTS", also: "64"},
		{name: "MAX_SSE_PER_PRINCIPAL", value: "0"},
		{name: "MAX_SSE_PER_PRINCIPAL", value: "21"},
		{name: "SSE_MAX_LIFETIME", value: "59s"},
		{name: "SSE_MAX_LIFETIME", value: "2h0m1s"},
		{name: "SSE_IDLE_TIMEOUT", value: "4999ms"},
		{name: "SSE_IDLE_TIMEOUT", value: "5m0s1ms"},
		{name: "IDEMPOTENCY_TTL", value: "59s"},
		{name: "IDEMPOTENCY_TTL", value: "24h0m1s"},
		{name: "SEARCH_OWNERSHIP_TTL", value: "59s"},
		{name: "SEARCH_OWNERSHIP_TTL", value: "24h0m1s"},
		{name: "SHUTDOWN_GRACE_PERIOD", value: "999ms"},
		{name: "SHUTDOWN_GRACE_PERIOD", value: "5m0s1ms"},
		{name: "AUDIT_RETENTION", value: "23h59m59s"},
		{name: "AUDIT_RETENTION", value: "8760h0m1s"},
		{name: "AUDIT_PURGE_INTERVAL", value: "59s"},
		{name: "AUDIT_PURGE_INTERVAL", value: "24h0m1s"},
	}
	for _, tt := range tests {
		t.Run(tt.name+"="+tt.value, func(t *testing.T) {
			prepareEdgeConfig(t)
			t.Setenv(tt.name, tt.value)
			if tt.alsoName != "" {
				t.Setenv(tt.alsoName, tt.also)
			}
			_, err := loadConfig()
			if err == nil || !strings.Contains(err.Error(), tt.name) {
				t.Fatalf("loadConfig() error=%v, want range error naming %s", err, tt.name)
			}
		})
	}
}

func TestLoadConfigAuditAndSSEDefaults(t *testing.T) {
	prepareEdgeConfig(t)
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuditRetention != 2160*time.Hour || cfg.AuditPurgeInterval != 24*time.Hour {
		t.Fatalf("unexpected audit defaults: retention=%s purge=%s", cfg.AuditRetention, cfg.AuditPurgeInterval)
	}
	if cfg.MaxConcurrentSSE != 64 || cfg.MaxSSEPerPrincipal != 3 || cfg.SSEMaxLifetime != 30*time.Minute || cfg.SSEIdleTimeout != 45*time.Second {
		t.Fatalf("unexpected SSE defaults: concurrent=%d principal=%d lifetime=%s idle=%s", cfg.MaxConcurrentSSE, cfg.MaxSSEPerPrincipal, cfg.SSEMaxLifetime, cfg.SSEIdleTimeout)
	}
}

func TestLocalEnvironmentGrantsAnonymousAdminByDefault(t *testing.T) {
	prepareEdgeConfig(t)
	t.Setenv("APP_ENV", "local")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.LocalAnonymousAdmin {
		t.Fatal("LocalAnonymousAdmin=false, want true for the explicit local environment")
	}
}

func TestLocalAnonymousAdminFailsClosedOutsideLocal(t *testing.T) {
	prepareEdgeConfig(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("ENABLE_LOCAL_ANONYMOUS_ADMIN", "true")
	_, err := loadConfig()
	if err == nil || !strings.Contains(err.Error(), "ENABLE_LOCAL_ANONYMOUS_ADMIN") {
		t.Fatalf("loadConfig() error=%v, want local anonymous admin environment error", err)
	}
}

func TestLocalAnonymousAdminRequiresExplicitAnonymousTransport(t *testing.T) {
	prepareEdgeConfig(t)
	t.Setenv("APP_ENV", "local")
	t.Setenv("ENABLE_ANONYMOUS_USAGE", "false")
	_, err := loadConfig()
	if err == nil || !strings.Contains(err.Error(), "ENABLE_LOCAL_ANONYMOUS_ADMIN requires ENABLE_ANONYMOUS_USAGE=true") {
		t.Fatalf("loadConfig() error=%v, want anonymous transport requirement", err)
	}
}

func TestLoadConfigRejectsWeakPreviousAbuseKey(t *testing.T) {
	prepareEdgeConfig(t)
	t.Setenv("ABUSE_PREVIOUS_HASH_KEY", "too-short")
	_, err := loadConfig()
	if err == nil || !strings.Contains(err.Error(), "ABUSE_PREVIOUS_HASH_KEY") {
		t.Fatalf("loadConfig() error=%v, want previous abuse key error", err)
	}
}

func TestProductionRequiresAbuseHashKey(t *testing.T) {
	prepareEdgeConfig(t)
	t.Setenv("APP_ENV", "Production")
	configureProductionEdge(t)
	t.Setenv("ABUSE_HASH_KEY", "")
	_, err := loadConfig()
	if err == nil || !strings.Contains(err.Error(), "ABUSE_HASH_KEY") {
		t.Fatalf("loadConfig() error=%v, want abuse key error", err)
	}
}

func configureProductionEdge(t *testing.T) {
	t.Helper()
	t.Setenv("INTERNAL_API_TOKEN", strings.Repeat("t", 32))
	t.Setenv("ENABLE_ANONYMOUS_USAGE", "false")
	t.Setenv("OIDC_ISSUER_URL", "https://identity.example.com")
	t.Setenv("OIDC_CLIENT_ID", "greenroute")
	t.Setenv("REQUIRE_REDIS", "true")
	t.Setenv("REDIS_URL", "redis://redis:6379/0")
	t.Setenv("REQUIRE_DATABASE", "true")
	t.Setenv("DATABASE_URL", "postgres://greenroute:secret@postgres/greenroute")
	t.Setenv("AUDIT_HASH_KEY", strings.Repeat("a", 32))
	t.Setenv("ABUSE_HASH_KEY", strings.Repeat("b", 32))
}
