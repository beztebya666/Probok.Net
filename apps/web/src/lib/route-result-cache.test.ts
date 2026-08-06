import { describe, expect, it } from "vitest";
import { createDemoSearch, getDemoSearch } from "./demo-fixtures";
import {
  clearCachedRouteResult,
  parseCachedRouteResult,
  readCachedRouteResult,
  ROUTE_RESULT_CACHE_KEY,
  writeCachedRouteResult,
} from "./route-result-cache";
import type { RouteSearchRequest } from "./schemas";

function memoryStorage(): Storage {
  const entries = new Map<string, string>();
  return {
    get length() {
      return entries.size;
    },
    clear: () => entries.clear(),
    getItem: (key: string) => entries.get(key) ?? null,
    key: (index: number) => [...entries.keys()][index] ?? null,
    removeItem: (key: string) => entries.delete(key),
    setItem: (key: string, value: string) => void entries.set(key, value),
  };
}

const request: RouteSearchRequest = {
  requestId: "0f8b1f0e-3ac2-4d63-9a6b-6c1f5e1a2d44",
  origin: { latitude: 55.75222, longitude: 37.61556 },
  destination: { latitude: 55.8299, longitude: 37.6331 },
  waypoints: [],
  departureTime: "2026-08-05T12:00:00.000Z",
  routingMode: "GREENEST",
  maxExtraDistanceMeters: 30_000,
  maxExtraDistancePercent: 300,
  maxExtraTimeSeconds: 1_200,
  avoidTolls: false,
  avoidUnpaved: true,
  strictness: 0.82,
  maxProviderRequests: 8,
  searchDeadlineMs: 20_000,
};

function completedResult() {
  createDemoSearch(request);
  return getDemoSearch(`demo-${request.requestId}`, true);
}

describe("route result cache", () => {
  it("restores the analysis together with the mode and selection that were on screen", () => {
    const storage = memoryStorage();
    const result = completedResult();
    writeCachedRouteResult(
      { result, routingMode: "STRICT_GREEN", selectedCandidateId: "route-green" },
      storage,
      Date.parse("2026-08-05T12:00:00.000Z"),
    );

    const restored = readCachedRouteResult(storage, Date.parse("2026-08-05T12:01:00.000Z"))!;
    expect(restored.result.searchId).toBe(result.searchId);
    expect(restored.result.greenTopRoutes.length).toBe(result.greenTopRoutes.length);
    expect(restored.routingMode).toBe("STRICT_GREEN");
    expect(restored.selectedCandidateId).toBe("route-green");
  });

  it("refuses to present an expired analysis as current traffic", () => {
    const storage = memoryStorage();
    const result = completedResult();
    writeCachedRouteResult({ result }, storage, Date.now());
    const afterExpiry = Date.parse(result.expiresAt) + 1_000;
    expect(readCachedRouteResult(storage, afterExpiry)).toBeUndefined();
  });

  it("survives corrupt, foreign and cleared records without throwing", () => {
    const storage = memoryStorage();
    expect(parseCachedRouteResult("{oops", Date.now())).toBeUndefined();
    expect(parseCachedRouteResult(JSON.stringify({ version: 99, result: {} }), Date.now())).toBeUndefined();
    expect(parseCachedRouteResult(JSON.stringify({ version: 1, result: { nope: true } }), Date.now())).toBeUndefined();

    writeCachedRouteResult({ result: completedResult() }, storage, Date.now());
    expect(storage.getItem(ROUTE_RESULT_CACHE_KEY)).not.toBeNull();
    clearCachedRouteResult(storage);
    expect(storage.getItem(ROUTE_RESULT_CACHE_KEY)).toBeNull();
    expect(readCachedRouteResult(undefined, Date.now())).toBeUndefined();
  });
});
