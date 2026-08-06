package main

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	minimumRatePerMinute = 1
	maximumRatePerMinute = 1_000_000
)

type config struct {
	Environment           string
	Address               string
	OrchestratorURL       string
	ProviderURL           string
	InternalToken         string
	RedisURL              string
	DatabaseURL           string
	RequireRedis          bool
	RequireDatabase       bool
	AnonymousUsage        bool
	LocalAnonymousAdmin   bool
	OIDCIssuerURL         string
	OIDCClientID          string
	OIDCAdminGroup        string
	CORSOrigins           []string
	TrustedProxyCIDRs     []string
	IPRequestsPerMinute   int
	UserRequestsPerMinute int
	SearchesPerMinute     int
	MaximumConcurrent     int
	MaxConcurrentSSE      int
	MaxSSEPerPrincipal    int
	SSEMaxLifetime        time.Duration
	SSEIdleTimeout        time.Duration
	IdempotencyTTL        time.Duration
	OwnershipTTL          time.Duration
	ShutdownGrace         time.Duration
	AuditHashKey          string
	AbuseHashKey          string
	AbusePreviousHashKey  string
	AuditRetention        time.Duration
	AuditPurgeInterval    time.Duration
}

func loadConfig() (config, error) {
	environment, err := parseEnvironment(os.Getenv("APP_ENV"))
	if err != nil {
		return config{}, err
	}

	c := config{
		Environment: environment, Address: env("EDGE_ADDR", ":8080"),
		OrchestratorURL: env("ORCHESTRATOR_URL", "http://localhost:8081"), ProviderURL: env("PROVIDER_URL", "http://localhost:8082"),
		InternalToken: strings.TrimSpace(os.Getenv("INTERNAL_API_TOKEN")), RedisURL: strings.TrimSpace(os.Getenv("REDIS_URL")), DatabaseURL: strings.TrimSpace(os.Getenv("DATABASE_URL")),
		OIDCIssuerURL: strings.TrimSpace(os.Getenv("OIDC_ISSUER_URL")), OIDCClientID: strings.TrimSpace(os.Getenv("OIDC_CLIENT_ID")),
		OIDCAdminGroup: env("OIDC_ADMIN_GROUP", "greenroute-admins"), CORSOrigins: splitCSV(os.Getenv("CORS_ALLOWED_ORIGINS")),
		TrustedProxyCIDRs: splitCSV(os.Getenv("TRUSTED_PROXY_CIDRS")), AuditHashKey: strings.TrimSpace(os.Getenv("AUDIT_HASH_KEY")),
		AbuseHashKey: strings.TrimSpace(os.Getenv("ABUSE_HASH_KEY")), AbusePreviousHashKey: strings.TrimSpace(os.Getenv("ABUSE_PREVIOUS_HASH_KEY")),
	}

	if c.RequireRedis, err = envBool("REQUIRE_REDIS", false); err != nil {
		return config{}, err
	}
	if c.RequireDatabase, err = envBool("REQUIRE_DATABASE", false); err != nil {
		return config{}, err
	}
	if c.AnonymousUsage, err = envBool("ENABLE_ANONYMOUS_USAGE", false); err != nil {
		return config{}, err
	}
	if c.LocalAnonymousAdmin, err = envBool("ENABLE_LOCAL_ANONYMOUS_ADMIN", c.Environment == "local"); err != nil {
		return config{}, err
	}
	if c.IPRequestsPerMinute, err = envInt("RATE_LIMIT_IP_PER_MINUTE", 60); err != nil {
		return config{}, err
	}
	if c.UserRequestsPerMinute, err = envInt("RATE_LIMIT_USER_PER_MINUTE", 120); err != nil {
		return config{}, err
	}
	if c.SearchesPerMinute, err = envInt("RATE_LIMIT_SEARCH_PER_MINUTE", 12); err != nil {
		return config{}, err
	}
	if c.MaximumConcurrent, err = envInt("MAX_CONCURRENT_EDGE_REQUESTS", 256); err != nil {
		return config{}, err
	}
	if c.MaxConcurrentSSE, err = envInt("MAX_CONCURRENT_SSE", 64); err != nil {
		return config{}, err
	}
	if c.MaxSSEPerPrincipal, err = envInt("MAX_SSE_PER_PRINCIPAL", 3); err != nil {
		return config{}, err
	}
	if c.SSEMaxLifetime, err = envDuration("SSE_MAX_LIFETIME", 30*time.Minute); err != nil {
		return config{}, err
	}
	if c.SSEIdleTimeout, err = envDuration("SSE_IDLE_TIMEOUT", 45*time.Second); err != nil {
		return config{}, err
	}
	if c.IdempotencyTTL, err = envDuration("IDEMPOTENCY_TTL", 15*time.Minute); err != nil {
		return config{}, err
	}
	if c.OwnershipTTL, err = envDuration("SEARCH_OWNERSHIP_TTL", 30*time.Minute); err != nil {
		return config{}, err
	}
	if c.ShutdownGrace, err = envDuration("SHUTDOWN_GRACE_PERIOD", 20*time.Second); err != nil {
		return config{}, err
	}
	if c.AuditRetention, err = envDuration("AUDIT_RETENTION", 2160*time.Hour); err != nil {
		return config{}, err
	}
	if c.AuditPurgeInterval, err = envDuration("AUDIT_PURGE_INTERVAL", 24*time.Hour); err != nil {
		return config{}, err
	}

	if _, err := validatedServiceURL(c.OrchestratorURL); err != nil {
		return config{}, fmt.Errorf("ORCHESTRATOR_URL: %w", err)
	}
	if _, err := validatedServiceURL(c.ProviderURL); err != nil {
		return config{}, fmt.Errorf("PROVIDER_URL: %w", err)
	}
	if c.Environment == "production" {
		if len(c.InternalToken) < 32 {
			return config{}, fmt.Errorf("INTERNAL_API_TOKEN of at least 32 characters is required in production")
		}
		if c.AnonymousUsage {
			return config{}, fmt.Errorf("ENABLE_ANONYMOUS_USAGE must be false in production")
		}
		if c.OIDCIssuerURL == "" || c.OIDCClientID == "" {
			return config{}, fmt.Errorf("OIDC_ISSUER_URL and OIDC_CLIENT_ID are required in production")
		}
		if !c.RequireRedis || !c.RequireDatabase {
			return config{}, fmt.Errorf("production requires REQUIRE_REDIS=true and REQUIRE_DATABASE=true")
		}
		if len(c.AuditHashKey) < 32 {
			return config{}, fmt.Errorf("AUDIT_HASH_KEY of at least 32 characters is required in production")
		}
		if len(c.AbuseHashKey) < 32 {
			return config{}, fmt.Errorf("ABUSE_HASH_KEY of at least 32 characters is required in production")
		}
	}
	if c.AbusePreviousHashKey != "" && len(c.AbusePreviousHashKey) < 32 {
		return config{}, fmt.Errorf("ABUSE_PREVIOUS_HASH_KEY must be empty or at least 32 characters")
	}
	if c.LocalAnonymousAdmin && c.Environment != "local" {
		return config{}, fmt.Errorf("ENABLE_LOCAL_ANONYMOUS_ADMIN is allowed only when APP_ENV=local")
	}
	if c.LocalAnonymousAdmin && !c.AnonymousUsage {
		return config{}, fmt.Errorf("ENABLE_LOCAL_ANONYMOUS_ADMIN requires ENABLE_ANONYMOUS_USAGE=true")
	}
	if !c.AnonymousUsage && (c.OIDCIssuerURL == "" || c.OIDCClientID == "") {
		return config{}, fmt.Errorf("OIDC configuration is required when anonymous usage is disabled")
	}
	if c.RequireRedis && c.RedisURL == "" {
		return config{}, fmt.Errorf("REDIS_URL is required")
	}
	if c.RequireDatabase && c.DatabaseURL == "" {
		return config{}, fmt.Errorf("DATABASE_URL is required")
	}
	if c.IPRequestsPerMinute < minimumRatePerMinute || c.IPRequestsPerMinute > maximumRatePerMinute {
		return config{}, fmt.Errorf("RATE_LIMIT_IP_PER_MINUTE must be between %d and %d", minimumRatePerMinute, maximumRatePerMinute)
	}
	if c.UserRequestsPerMinute < minimumRatePerMinute || c.UserRequestsPerMinute > maximumRatePerMinute {
		return config{}, fmt.Errorf("RATE_LIMIT_USER_PER_MINUTE must be between %d and %d", minimumRatePerMinute, maximumRatePerMinute)
	}
	if c.SearchesPerMinute < minimumRatePerMinute || c.SearchesPerMinute > maximumRatePerMinute {
		return config{}, fmt.Errorf("RATE_LIMIT_SEARCH_PER_MINUTE must be between %d and %d", minimumRatePerMinute, maximumRatePerMinute)
	}
	if c.MaximumConcurrent < 1 || c.MaximumConcurrent > 10_000 {
		return config{}, fmt.Errorf("MAX_CONCURRENT_EDGE_REQUESTS must be between 1 and 10000")
	}
	if c.MaxConcurrentSSE < 1 || c.MaxConcurrentSSE > 1_000 {
		return config{}, fmt.Errorf("MAX_CONCURRENT_SSE must be between 1 and 1000")
	}
	if c.MaxConcurrentSSE >= c.MaximumConcurrent {
		return config{}, fmt.Errorf("MAX_CONCURRENT_SSE must be less than MAX_CONCURRENT_EDGE_REQUESTS")
	}
	if c.MaxSSEPerPrincipal < 1 || c.MaxSSEPerPrincipal > 20 {
		return config{}, fmt.Errorf("MAX_SSE_PER_PRINCIPAL must be between 1 and 20")
	}
	if c.SSEMaxLifetime < time.Minute || c.SSEMaxLifetime > 2*time.Hour {
		return config{}, fmt.Errorf("SSE_MAX_LIFETIME must be between 1m and 2h")
	}
	if c.SSEIdleTimeout < 5*time.Second || c.SSEIdleTimeout > 5*time.Minute {
		return config{}, fmt.Errorf("SSE_IDLE_TIMEOUT must be between 5s and 5m")
	}
	if c.IdempotencyTTL < time.Minute || c.IdempotencyTTL > 24*time.Hour {
		return config{}, fmt.Errorf("IDEMPOTENCY_TTL must be between 1m and 24h")
	}
	if c.OwnershipTTL < time.Minute || c.OwnershipTTL > 24*time.Hour {
		return config{}, fmt.Errorf("SEARCH_OWNERSHIP_TTL must be between 1m and 24h")
	}
	if c.ShutdownGrace < time.Second || c.ShutdownGrace > 5*time.Minute {
		return config{}, fmt.Errorf("SHUTDOWN_GRACE_PERIOD must be between 1s and 5m")
	}
	if c.AuditRetention < 24*time.Hour || c.AuditRetention > 365*24*time.Hour {
		return config{}, fmt.Errorf("AUDIT_RETENTION must be between 24h and 8760h")
	}
	if c.AuditPurgeInterval < time.Minute || c.AuditPurgeInterval > 24*time.Hour {
		return config{}, fmt.Errorf("AUDIT_PURGE_INTERVAL must be between 1m and 24h")
	}
	if c.AuditPurgeInterval > c.AuditRetention {
		return config{}, fmt.Errorf("AUDIT_PURGE_INTERVAL must not exceed AUDIT_RETENTION")
	}
	return c, nil
}

func parseEnvironment(raw string) (string, error) {
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
		return "", fmt.Errorf("APP_ENV must be one of local, development, test, staging, or production")
	}
}

func validatedServiceURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("must be an absolute http(s) URL without userinfo, query, or fragment")
	}
	return parsed, nil
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
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

func envInt(name string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	return parsed, nil
}

func envDuration(name string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration", name)
	}
	return parsed, nil
}

func splitCSV(value string) []string {
	values := strings.Split(value, ",")
	result := make([]string, 0, len(values))
	for _, item := range values {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}
