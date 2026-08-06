package main

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

type config struct {
	Environment             string
	Address                 string
	ProviderURL             string
	InternalToken           string
	RedisURL                string
	RequireRedis            bool
	ProviderDataStorage     bool
	StateTTL                time.Duration
	StaleSearchAfter        time.Duration
	EnableEnhancedSearch    bool
	EnableAvoidZones        bool
	EnableCorridorAnchors   bool
	EnableReranking         bool
	MaxActiveCandidates     int
	MaxConcurrentSearches   int
	MaxEnhancedIterations   int
	MinimumScoreImprovement float64
	ScoringPolicyFile       string
	SSEHeartbeat            time.Duration
	ShutdownGrace           time.Duration
}

func loadConfig() (config, error) {
	environment, err := parseEnvironment(os.Getenv("APP_ENV"))
	if err != nil {
		return config{}, err
	}

	c := config{
		Environment: environment, Address: env("ORCHESTRATOR_ADDR", ":8081"),
		ProviderURL: env("PROVIDER_URL", "http://localhost:8082"), InternalToken: strings.TrimSpace(os.Getenv("INTERNAL_API_TOKEN")),
		RedisURL: strings.TrimSpace(os.Getenv("REDIS_URL")), ScoringPolicyFile: strings.TrimSpace(os.Getenv("SCORING_POLICY_FILE")),
	}
	if c.RequireRedis, err = envBool("REQUIRE_REDIS", false); err != nil {
		return config{}, err
	}
	if c.ProviderDataStorage, err = envBool("PROVIDER_DATA_STORAGE_ALLOWED", false); err != nil {
		return config{}, err
	}
	if c.EnableEnhancedSearch, err = envBool("ENABLE_ENHANCED_SEARCH", true); err != nil {
		return config{}, err
	}
	// Forbidding the congested stretch outright is the primary green-search
	// instrument, not an experiment: without it the provider routes straight
	// back onto the jam the detour was supposed to avoid.
	if c.EnableAvoidZones, err = envBool("ENABLE_AVOID_ZONE_GENERATION", true); err != nil {
		return config{}, err
	}
	if c.EnableCorridorAnchors, err = envBool("ENABLE_CORRIDOR_ANCHORS", true); err != nil {
		return config{}, err
	}
	if c.EnableReranking, err = envBool("ENABLE_ROUTE_RERANKING", true); err != nil {
		return config{}, err
	}
	if c.StateTTL, err = envDuration("SEARCH_STATE_TTL", 30*time.Minute); err != nil {
		return config{}, err
	}
	if c.StaleSearchAfter, err = envDuration("STALE_SEARCH_AFTER", 45*time.Second); err != nil {
		return config{}, err
	}
	if c.MaxActiveCandidates, err = envInt("MAX_ACTIVE_CANDIDATES", 12); err != nil {
		return config{}, err
	}
	if c.MaxConcurrentSearches, err = envInt("MAX_CONCURRENT_SEARCHES", 64); err != nil {
		return config{}, err
	}
	if c.MaxEnhancedIterations, err = envInt("MAX_ENHANCED_ITERATIONS", 5); err != nil {
		return config{}, err
	}
	if c.MinimumScoreImprovement, err = envFloat("MINIMUM_SCORE_IMPROVEMENT", 0.02); err != nil {
		return config{}, err
	}
	if c.SSEHeartbeat, err = envDuration("SSE_HEARTBEAT_INTERVAL", 15*time.Second); err != nil {
		return config{}, err
	}
	if c.ShutdownGrace, err = envDuration("SHUTDOWN_GRACE_PERIOD", 20*time.Second); err != nil {
		return config{}, err
	}

	if c.Environment == "production" && len(c.InternalToken) < 32 {
		return config{}, fmt.Errorf("INTERNAL_API_TOKEN of at least 32 characters is required in production")
	}
	if c.RequireRedis && c.RedisURL == "" {
		return config{}, fmt.Errorf("REDIS_URL is required when REQUIRE_REDIS=true")
	}
	if c.RequireRedis && !c.ProviderDataStorage {
		return config{}, fmt.Errorf("REQUIRE_REDIS=true for route state requires PROVIDER_DATA_STORAGE_ALLOWED=true")
	}
	if c.StateTTL < time.Minute || c.StateTTL > 24*time.Hour {
		return config{}, fmt.Errorf("SEARCH_STATE_TTL must be between 1m and 24h")
	}
	if c.StaleSearchAfter < 35*time.Second || c.StaleSearchAfter > 5*time.Minute {
		return config{}, fmt.Errorf("STALE_SEARCH_AFTER must be between 35s and 5m")
	}
	if c.MaxActiveCandidates < 1 || c.MaxActiveCandidates > 50 {
		return config{}, fmt.Errorf("MAX_ACTIVE_CANDIDATES must be between 1 and 50")
	}
	if c.MaxConcurrentSearches < 1 || c.MaxConcurrentSearches > 1000 {
		return config{}, fmt.Errorf("MAX_CONCURRENT_SEARCHES must be between 1 and 1000")
	}
	if c.MaxEnhancedIterations < 0 || c.MaxEnhancedIterations > 8 {
		return config{}, fmt.Errorf("MAX_ENHANCED_ITERATIONS must be between 0 and 8")
	}
	if math.IsNaN(c.MinimumScoreImprovement) || math.IsInf(c.MinimumScoreImprovement, 0) || c.MinimumScoreImprovement < 0 || c.MinimumScoreImprovement > 1 {
		return config{}, fmt.Errorf("MINIMUM_SCORE_IMPROVEMENT must be a finite number between 0 and 1")
	}
	if c.SSEHeartbeat < time.Second || c.SSEHeartbeat > 5*time.Minute {
		return config{}, fmt.Errorf("SSE_HEARTBEAT_INTERVAL must be between 1s and 5m")
	}
	if c.ShutdownGrace < time.Second || c.ShutdownGrace > 5*time.Minute {
		return config{}, fmt.Errorf("SHUTDOWN_GRACE_PERIOD must be between 1s and 5m")
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

func envFloat(name string, fallback float64) (float64, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a number", name)
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
