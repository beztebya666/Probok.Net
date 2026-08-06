import { describe, expect, it } from "vitest";
import {
  AdminOverviewSchema,
  ConfidenceSchema,
  RouteCandidateSchema,
  RouteSearchRequestSchema,
  RouteSearchResultSchema,
} from "./schemas";

const candidate = {
  candidateId: "candidate-1",
  provider: "YANDEX",
	trafficDataType: "UNKNOWN",
  geometry: [
    { latitude: 55.75, longitude: 37.61 },
    { latitude: 55.76, longitude: 37.62 },
  ],
  distanceMeters: 1_000,
  liveDurationSeconds: 240,
  baselineDurationSeconds: 200,
  trafficDelaySeconds: 40,
  segments: [{
    segmentId: "segment-1",
    geometry: [
      { latitude: 55.75, longitude: 37.61 },
      { latitude: 55.76, longitude: 37.62 },
    ],
    distanceMeters: 1_000,
    liveDurationSeconds: 240,
    baselineDurationSeconds: 200,
    trafficRatio: 1.2,
    congestionClass: "UNKNOWN",
    confidence: 0.4,
    geometrySimilarity: 0.7,
    source: "GREENROUTE_ESTIMATE",
  }],
  blocked: false,
  tolls: false,
  confidence: "MEDIUM",
  score: 10,
  reasonCodes: ["PARTIAL_BASELINE_COVERAGE"],
	explanation: "Traffic evidence is incomplete.",
  generatedBy: "INITIAL_PROVIDER_ALTERNATIVE",
};

describe("runtime API validation", () => {
  it("normalizes all supported confidence shapes", () => {
    expect(ConfidenceSchema.parse(0.91)).toEqual({ score: 0.91, level: "HIGH", reasons: [] });
    expect(ConfidenceSchema.parse("LOW")).toEqual({ score: 0.35, level: "LOW", reasons: [] });
    expect(ConfidenceSchema.parse({ score: 0.6, reasons: ["PARTIAL"] })).toEqual({
      score: 0.6,
      level: "MEDIUM",
      reasons: ["PARTIAL"],
    });
  });

  it("preserves UNKNOWN congestion instead of treating it as GREEN", () => {
    const parsed = RouteCandidateSchema.parse(candidate);
    expect(parsed.segments[0]?.congestionClass).toBe("UNKNOWN");
    expect(parsed.segments[0]?.confidence.level).toBe("LOW");
  });

  it("normalizes legacy null alternatives while preserving a degraded fastest-reference route", () => {
    const parsed = RouteSearchResultSchema.parse({
      searchId: "76d78dc8-b2f3-4964-812a-cf2532fefc00",
      requestId: "35aa95f5-9f14-44b9-8589-b488cb108aa9",
      status: "DEGRADED",
      alternatives: null,
      fastestReferenceRoute: candidate,
      constraints: {},
      providerUsage: { provider: "2gis", requestsUsed: 4, requestBudget: 4 },
      warnings: ["NO_ROUTE_WITHIN_HARD_CONSTRAINTS"],
      scoringPolicyVersion: "greenroute-v1",
      generatedAt: "2026-08-05T18:36:38.6623652Z",
      expiresAt: "2026-08-05T19:06:38.6623652Z",
    });

    expect(parsed.alternatives).toEqual([]);
    expect(parsed.bestEffortRoutes).toEqual([]);
    expect(parsed.fastestReferenceRoute?.candidateId).toBe(candidate.candidateId);
    expect(parsed.warnings).toEqual([{ code: "NO_ROUTE_WITHIN_HARD_CONSTRAINTS", message: undefined }]);
  });

  it("accepts up to three explicitly separated best-effort strict candidates", () => {
    const parsed = RouteSearchResultSchema.parse({
      searchId: "76d78dc8-b2f3-4964-812a-cf2532fefc00",
      status: "DEGRADED",
      alternatives: [],
      bestEffortRoutes: [candidate],
      constraints: {},
      providerUsage: {},
      warnings: [],
      generatedAt: "2026-08-05T18:36:38.6623652Z",
      expiresAt: "2026-08-05T19:06:38.6623652Z",
    });
    expect(parsed.bestEffortRoutes[0]?.candidateId).toBe(candidate.candidateId);
    expect(RouteSearchResultSchema.safeParse({ ...parsed, bestEffortRoutes: [candidate, candidate, candidate, candidate] }).success).toBe(false);
  });

	it("accepts an unavailable baseline only as explicit UNKNOWN evidence", () => {
		const degraded = structuredClone(candidate);
		degraded.baselineDurationSeconds = 0;
		degraded.trafficDataType = "UNKNOWN";
		degraded.segments[0]!.baselineDurationSeconds = 0;
		degraded.segments[0]!.congestionClass = "UNKNOWN";
		expect(RouteCandidateSchema.parse(degraded)).toMatchObject({
			baselineDurationSeconds: 0,
			trafficDataType: "UNKNOWN",
		});
	});

  it("rejects unsafe coordinates and impossible budgets", () => {
    const request = {
      requestId: "f8a7d69e-64e6-45d9-b3a0-1b4ef3c84916",
      origin: { latitude: 120, longitude: 37.61 },
      destination: { latitude: 55.76, longitude: 37.62 },
      waypoints: [],
      departureTime: new Date().toISOString(),
      routingMode: "GREENEST",
      maxExtraDistanceMeters: 30_000,
      maxExtraDistancePercent: 100,
      maxExtraTimeSeconds: 1_200,
      avoidTolls: false,
      avoidUnpaved: true,
      strictness: 0.8,
      maxProviderRequests: 500,
      searchDeadlineMs: 20_000,
    };
    expect(RouteSearchRequestSchema.safeParse(request).success).toBe(false);
  });

  it("normalizes the nested operations overview returned by the orchestrator", () => {
    const overview = AdminOverviewSchema.parse({
      provider: { name: "YANDEX", status: "UP", requestCount: 120, estimatedBillableUnits: 45, circuitBreaker: "CLOSED" },
      scoringPolicy: { policyVersion: "greenroute-v1" },
      searches: { total: 20, degradedPercent: 5, lowConfidenceCount: 2, searchBudgetExhaustion: 1 },
      featureFlags: { ENABLE_ENHANCED_SEARCH: true },
      apiIntegrations: [{
        id: "2gis-routing",
        provider: "2gis",
        product: "Routing API",
        apiVersion: "7.0.0",
        capability: "routing",
        role: "primary",
        state: "active",
      }],
    });
    expect(overview).toMatchObject({
      provider: "YANDEX",
      status: "UP",
      requestCount: 120,
      estimatedCost: 45,
      scoringPolicy: "greenroute-v1",
      lowConfidencePercent: 10,
      apiIntegrations: [{ id: "2gis-routing", apiVersion: "7.0.0" }],
    });
  });

  it("rejects credential-shaped fields in API integration metadata", () => {
    const result = AdminOverviewSchema.safeParse({
      provider: { name: "2gis", status: "UP" },
      scoringPolicy: { policyVersion: "greenroute-v1" },
      searches: {},
      featureFlags: {},
      apiIntegrations: [{
        id: "2gis-routing",
        provider: "2gis",
        product: "Routing API",
        apiVersion: "7.0.0",
        capability: "routing",
        role: "primary",
        state: "active",
        apiKey: "must-not-cross-the-contract",
      }],
    });
    expect(result.success).toBe(false);
  });
});
