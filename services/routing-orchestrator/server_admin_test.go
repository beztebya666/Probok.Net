package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/greenroute/greenroute/internal/contracts"
	"github.com/greenroute/greenroute/internal/scoring"
	"github.com/greenroute/greenroute/internal/telemetry"
)

func TestAdminOverviewPublishesOnlySafeSelectedAPIIntegrations(t *testing.T) {
	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health/ready" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(providerServer.Close)
	client, err := newProviderClient(providerServer.URL, "", telemetry.NewMetrics("admin-overview-test"))
	if err != nil {
		t.Fatal(err)
	}
	engine := &engine{provider: client, scoring: scoring.DefaultConfig()}
	engine.capabilities = contracts.ProviderCapabilities{
		Provider: "2gis", Mode: "2gis",
		APIIntegrations: []contracts.APIIntegration{
			{
				ID: "2gis-routing", Provider: "2gis", Product: "Routing API", APIVersion: "7.0.0",
				Capability: "routing", Role: "primary", State: "active",
			},
		},
	}
	server := &apiServer{engine: engine}
	recorder := httptest.NewRecorder()
	server.adminOverview(recorder, httptest.NewRequest(http.MethodGet, "/internal/v1/admin/overview", nil).WithContext(context.Background()))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		APIIntegrations []contracts.APIIntegration `json:"apiIntegrations"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.APIIntegrations) != 1 || response.APIIntegrations[0].APIVersion != "7.0.0" {
		t.Fatalf("unexpected integrations: %#v", response.APIIntegrations)
	}
	for _, forbidden := range []string{"apiKey", "credential", "officialEndpoint", "addressSearchEndpoint"} {
		if strings.Contains(recorder.Body.String(), forbidden) {
			t.Fatalf("admin overview leaked forbidden provider metadata %q", forbidden)
		}
	}
}
