import { beforeEach, describe, expect, it } from "vitest";
import { createDemoSearch, getDemoSearch } from "./demo-fixtures";
import {
  defaultCandidateId,
  estimatedFreePercent,
  findGreenest,
  formatDuration,
  isVerifiedAllGreenRoute,
  routeCandidates,
  routeCandidatesForMode,
  selectedCandidateForMode,
  unknownPercent,
} from "./route-insights";
import type { RouteSearchRequest } from "./schemas";

const request: RouteSearchRequest = {
  requestId: "38bfa07d-c10c-401c-83b8-73e085ea2db8",
  origin: { latitude: 55.75222, longitude: 37.61556 },
  destination: { latitude: 55.8299, longitude: 37.6331 },
  waypoints: [],
  departureTime: "2026-08-05T12:00:00.000Z",
  routingMode: "GREENEST",
  maxExtraDistanceMeters: 30_000,
  maxExtraDistancePercent: 100,
  maxExtraTimeSeconds: 1_200,
  avoidTolls: false,
  avoidUnpaved: true,
  strictness: 0.82,
  maxProviderRequests: 12,
  searchDeadlineMs: 20_000,
};

describe("route insights", () => {
  beforeEach(() => createDemoSearch(request));

  it("deduplicates candidates and identifies greenest independently from fastest", () => {
    const result = getDemoSearch(`demo-${request.requestId}`, true);
    const routes = routeCandidates(result);
    expect(routes).toHaveLength(3);
    expect(findGreenest(routes)?.candidateId).toBe("route-green");
    expect(result.fastestReferenceRoute?.candidateId).toBe("route-fastest");
  });

	it("includes UNKNOWN segments in the honest whole-route free-flow denominator", () => {
		const result = getDemoSearch(`demo-${request.requestId}`, true);
		const balanced = routeCandidates(result).find((route) => route.candidateId === "route-balanced")!;
		expect(unknownPercent(balanced)).toBeGreaterThan(0);
		const greenDistance = balanced.segments
			.filter((segment) => segment.congestionClass === "GREEN")
			.reduce((sum, segment) => sum + segment.distanceMeters, 0);
		const totalDistance = balanced.segments.reduce((sum, segment) => sum + segment.distanceMeters, 0);
		expect(estimatedFreePercent(balanced)).toBe(Math.round((greenDistance / totalDistance) * 100));
	});

	it("renders zero delay truthfully while rounding a positive sub-minute ETA up", () => {
		expect(formatDuration(0, "ru")).toBe("0 мин");
		expect(formatDuration(20, "en")).toBe("1 min");
	});

	it("never labels an UNKNOWN route as lower congestion than confirmed RED", () => {
		const result = getDemoSearch(`demo-${request.requestId}`, true);
		const routes = routeCandidates(result);
		const unknown = structuredClone(routes[0]!);
		unknown.candidateId = "all-unknown";
		unknown.trafficDataType = "UNKNOWN";
		unknown.segments = unknown.segments.map((segment) => ({ ...segment, congestionClass: "UNKNOWN" as const }));
		unknown.confidence = { level: "LOW", score: 0.2, reasons: ["UNKNOWN_SEGMENTS"] };
		const confirmedRed = structuredClone(routes[0]!);
		confirmedRed.candidateId = "confirmed-red";
		confirmedRed.trafficDataType = "REALTIME";
		confirmedRed.segments = confirmedRed.segments.map((segment) => ({ ...segment, congestionClass: "RED" as const }));
		confirmedRed.confidence = { level: "HIGH", score: 0.9, reasons: [] };
		expect(findGreenest([unknown, confirmedRed])?.candidateId).toBe("confirmed-red");
		expect(findGreenest([unknown])).toBeUndefined();
	});

  it("fails closed in strict mode instead of selecting the fastest reference", () => {
    const result = getDemoSearch(`demo-${request.requestId}`, true);
    result.selectedRoute = null;

    expect(defaultCandidateId(result, "STRICT_GREEN")).toBeUndefined();
    expect(selectedCandidateForMode(result, undefined, "STRICT_GREEN")).toBeUndefined();
    expect(routeCandidatesForMode(result, "STRICT_GREEN")).toEqual([]);
    expect(defaultCandidateId(result, "GREENEST")).toBe(result.fastestReferenceRoute?.candidateId);
  });

  it("accepts only an evidence-backed candidate whose every segment is green", () => {
    const result = getDemoSearch(`demo-${request.requestId}`, true);
    const allGreen = structuredClone(result.selectedRoute!);
    allGreen.segments = allGreen.segments.map((segment) => ({
      ...segment,
      congestionClass: "GREEN" as const,
      confidence: { level: "HIGH" as const, score: 0.9, reasons: [] },
    }));
    result.selectedRoute = allGreen;

    expect(isVerifiedAllGreenRoute(allGreen)).toBe(true);
    expect(defaultCandidateId(result, "STRICT_GREEN")).toBe(allGreen.candidateId);
    expect(routeCandidatesForMode(result, "STRICT_GREEN").every(isVerifiedAllGreenRoute)).toBe(true);

    const uncertain = structuredClone(allGreen);
    uncertain.confidence = { level: "LOW", score: 0.3, reasons: ["LOW_CONFIDENCE"] };
    result.selectedRoute = uncertain;
    expect(isVerifiedAllGreenRoute(uncertain)).toBe(false);
    expect(defaultCandidateId(result, "STRICT_GREEN")).toBeUndefined();
  });
});
