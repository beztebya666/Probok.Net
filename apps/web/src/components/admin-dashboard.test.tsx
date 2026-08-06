import { render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AppProviders } from "./app-providers";
import { AdminDashboard } from "./admin-dashboard";

const previousDemoMode = process.env.NEXT_PUBLIC_DEMO_MODE;
const previousEdgeURL = process.env.NEXT_PUBLIC_EDGE_API_BASE_URL;
const api = vi.hoisted(() => ({
  getSession: vi.fn(),
  getAdminOverview: vi.fn(),
}));

vi.mock("@/lib/api-client", () => api);

describe("AdminDashboard demo mode", () => {
  beforeEach(() => {
    process.env.NEXT_PUBLIC_DEMO_MODE = "true";
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.unstubAllEnvs();
    if (previousDemoMode === undefined) delete process.env.NEXT_PUBLIC_DEMO_MODE;
    else process.env.NEXT_PUBLIC_DEMO_MODE = previousDemoMode;
    if (previousEdgeURL === undefined) delete process.env.NEXT_PUBLIC_EDGE_API_BASE_URL;
    else process.env.NEXT_PUBLIC_EDGE_API_BASE_URL = previousEdgeURL;
  });

  it("shows plan limits separately and never invents operational telemetry", () => {
    const { container } = render(<AppProviders><AdminDashboard /></AppProviders>);

    expect(screen.getByText("Реальная телеметрия не подключена")).toBeInTheDocument();
    // The inventory always names both providers and every module, including the
    // ones deliberately not used, so an absence is visibly a decision.
    expect(screen.getByText("2ГИС")).toBeInTheDocument();
    expect(screen.getByText("Яндекс")).toBeInTheDocument();
    expect(container.querySelectorAll(".quota-card").length).toBeGreaterThan(1);
    expect(screen.getAllByText("Только на платном тарифе").length).toBeGreaterThan(0);
    expect(screen.getByText("Маршруты строит 2ГИС")).toBeInTheDocument();
    // Without provider evidence every routed module is marked inactive rather
    // than shown as working.
    expect(container.querySelectorAll(".quota-card.is-inactive").length).toBeGreaterThan(0);
    expect(container.querySelectorAll(".metric-card")).toHaveLength(0);
    expect(screen.getByText("100 запросов / сутки")).toBeInTheDocument();
    expect(screen.queryByText("18 426")).not.toBeInTheDocument();
  });

  it("accepts the server-authorized local admin role without pretending it is OIDC authenticated", async () => {
    process.env.NEXT_PUBLIC_DEMO_MODE = "false";
    process.env.NEXT_PUBLIC_EDGE_API_BASE_URL = "http://localhost:8080";
    vi.stubEnv("GREENROUTE_PROVIDER_MODE", "2gis");
    vi.stubEnv("GREENROUTE_ADDRESS_PROVIDER_MODE", "auto");
    vi.stubEnv("GREENROUTE_YANDEX_GEOCODER_CONFIGURED", "true");
    vi.stubEnv("GREENROUTE_YANDEX_GEOSUGGEST_CONFIGURED", "true");
    vi.stubEnv("GREENROUTE_DGIS_CONFIGURED", "true");
    api.getSession.mockResolvedValue({
      userId: "anonymous:local",
      displayName: "Local admin",
      roles: ["user", "admin"],
      authenticated: false,
    });
    api.getAdminOverview.mockResolvedValue({
      provider: "2GIS",
      status: "HEALTHY",
      circuitBreaker: "MANAGED_BY_PROVIDER_ADAPTER",
      requestCount: 0,
      estimatedCost: 0,
      scoringPolicy: "greenroute-v1",
      degradedPercent: 0,
      lowConfidencePercent: 0,
      searchBudgetExhaustion: 0,
      featureFlags: {},
      apiIntegrations: [
        { id: "2gis-routing", provider: "2gis", product: "Routing API", apiVersion: "7.0.0", capability: "routing", role: "primary", state: "active" },
        { id: "yandex-http-geocoder", provider: "yandex", product: "HTTP Geocoder API", apiVersion: "v1", capability: "address-search", role: "primary", state: "active" },
        { id: "2gis-geocoder", provider: "2gis", product: "Geocoder API", apiVersion: "3.0", capability: "address-search", role: "fallback", state: "standby" },
      ],
    });

    render(<AppProviders><AdminDashboard /></AppProviders>);

    // Status is reported inside each provider group, and each group states
    // whether that status is a live probe or just configuration.
    expect(await screen.findByText(/^работает$|^healthy$/)).toBeInTheDocument();
    expect(screen.getByText(/по данным адаптера|reported by the adapter/)).toBeInTheDocument();
    expect(screen.getByText(/по конфигурации|from configuration/)).toBeInTheDocument();
    expect(screen.queryByText(/Доступ не подтверждён|Access not confirmed/)).not.toBeInTheDocument();
    expect(screen.getByText("Local admin")).toBeInTheDocument();
    expect(screen.getByText("Routing API 7.0.0")).toBeInTheDocument();
    expect(screen.getByText("Geocoder API 3.0")).toBeInTheDocument();
    expect(screen.getByText("JavaScript API v3")).toBeInTheDocument();
    expect(screen.getByText(/API Геокодера v1|Geocoder API v1/)).toBeInTheDocument();
    expect(screen.getByText(/^Адреса$|^Addresses$/)).toBeInTheDocument();
    expect(screen.getAllByText(/^Резерв$|^Fallback$/)).toHaveLength(1);
    expect(screen.getByText(/Управляет 2ГИС|Provider-managed/)).toBeInTheDocument();
    // The policy id is shown without its internal product prefix; the full raw
    // value stays available as the element title.
    expect(screen.getByText("v1")).toHaveClass("metric-value-compact");
    expect(screen.getAllByTitle("greenroute-v1").length).toBeGreaterThanOrEqual(1);
    // The Suggest key exists in the Yandex account but no code path calls it, so
    // the module is listed and explicitly marked as never called.
    // Both providers expose a suggest product and neither is called, so the
    // column exists twice; only the Yandex one additionally carries a key.
    expect(screen.getAllByText(/API Геосаджеста|Suggest API/)).toHaveLength(2);
    expect(screen.getAllByText(/Подсказки адресов отдаёт Геокодер|Address hints are served by the Geocoder/)).toHaveLength(2);
    expect(screen.getByText(/^Не вызывается$|^Never called$/)).toBeInTheDocument();
    expect(screen.queryByText(/Search \/ Geocoder \/ Suggest/)).not.toBeInTheDocument();
  });
});
