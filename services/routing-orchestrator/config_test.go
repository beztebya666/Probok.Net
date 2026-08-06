package main

import (
	"strings"
	"testing"
	"time"
)

var orchestratorConfigEnvironment = []string{
	"APP_ENV", "ORCHESTRATOR_ADDR", "PROVIDER_URL", "INTERNAL_API_TOKEN", "REDIS_URL", "REQUIRE_REDIS",
	"PROVIDER_DATA_STORAGE_ALLOWED", "SEARCH_STATE_TTL", "ENABLE_ENHANCED_SEARCH", "ENABLE_AVOID_ZONE_GENERATION",
	"ENABLE_CORRIDOR_ANCHORS", "ENABLE_ROUTE_RERANKING", "MAX_ACTIVE_CANDIDATES", "MAX_ENHANCED_ITERATIONS",
	"MINIMUM_SCORE_IMPROVEMENT", "SSE_HEARTBEAT_INTERVAL", "SCORING_POLICY_FILE", "SHUTDOWN_GRACE_PERIOD",
}

func prepareOrchestratorConfig(t *testing.T) {
	t.Helper()
	for _, name := range orchestratorConfigEnvironment {
		t.Setenv(name, "")
	}
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
			prepareOrchestratorConfig(t)
			t.Setenv("APP_ENV", tt.value)
			if tt.want == "production" {
				t.Setenv("INTERNAL_API_TOKEN", strings.Repeat("t", 32))
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
	prepareOrchestratorConfig(t)
	t.Setenv("APP_ENV", "Production")
	_, err := loadConfig()
	if err == nil || !strings.Contains(err.Error(), "INTERNAL_API_TOKEN") {
		t.Fatalf("loadConfig() error=%v, want production token error", err)
	}
}

func TestLoadConfigRejectsUnknownEnvironment(t *testing.T) {
	prepareOrchestratorConfig(t)
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
		{name: "REQUIRE_REDIS", value: "occasionally"},
		{name: "ENABLE_ENHANCED_SEARCH", value: "enabled"},
		{name: "MAX_ACTIVE_CANDIDATES", value: "a-dozen"},
		{name: "MINIMUM_SCORE_IMPROVEMENT", value: "small"},
		{name: "SEARCH_STATE_TTL", value: "thirty-minutes"},
		{name: "SSE_HEARTBEAT_INTERVAL", value: "15000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prepareOrchestratorConfig(t)
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
		name  string
		value string
	}{
		{name: "SEARCH_STATE_TTL", value: "59s"},
		{name: "SEARCH_STATE_TTL", value: "24h0m1s"},
		{name: "MAX_ACTIVE_CANDIDATES", value: "0"},
		{name: "MAX_ACTIVE_CANDIDATES", value: "51"},
		{name: "MAX_ENHANCED_ITERATIONS", value: "-1"},
		{name: "MAX_ENHANCED_ITERATIONS", value: "9"},
		{name: "MINIMUM_SCORE_IMPROVEMENT", value: "-0.0001"},
		{name: "MINIMUM_SCORE_IMPROVEMENT", value: "1.0001"},
		{name: "MINIMUM_SCORE_IMPROVEMENT", value: "NaN"},
		{name: "MINIMUM_SCORE_IMPROVEMENT", value: "+Inf"},
		{name: "SSE_HEARTBEAT_INTERVAL", value: "999ms"},
		{name: "SSE_HEARTBEAT_INTERVAL", value: "5m0s1ms"},
		{name: "SHUTDOWN_GRACE_PERIOD", value: "999ms"},
		{name: "SHUTDOWN_GRACE_PERIOD", value: "5m0s1ms"},
	}
	for _, tt := range tests {
		t.Run(tt.name+"="+tt.value, func(t *testing.T) {
			prepareOrchestratorConfig(t)
			t.Setenv(tt.name, tt.value)
			_, err := loadConfig()
			if err == nil || !strings.Contains(err.Error(), tt.name) {
				t.Fatalf("loadConfig() error=%v, want range error naming %s", err, tt.name)
			}
		})
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	prepareOrchestratorConfig(t)
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StateTTL != 30*time.Minute || cfg.SSEHeartbeat != 15*time.Second || cfg.ShutdownGrace != 20*time.Second {
		t.Fatalf("unexpected duration defaults: state=%s heartbeat=%s shutdown=%s", cfg.StateTTL, cfg.SSEHeartbeat, cfg.ShutdownGrace)
	}
	if cfg.MinimumScoreImprovement != 0.02 {
		t.Fatalf("MinimumScoreImprovement=%v, want 0.02", cfg.MinimumScoreImprovement)
	}
}
