import { describe, expect, it } from "vitest";
import { createDemoSearch, getDemoSearch } from "./demo-fixtures";
import {
  canRefreshBookmark,
  isBookmarked,
  reconstructBookmarkRequest,
  MAX_ROUTE_BOOKMARKS,
  parseRouteBookmarks,
  readRouteBookmarks,
  rememberRouteBookmark,
  removeRouteBookmark,
  ROUTE_BOOKMARKS_STORAGE_KEY,
  writeRouteBookmarks,
  type RouteBookmark,
} from "./route-bookmarks";
import { defaultRoutePreferences } from "./route-preferences";
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

function bookmark(id: string, savedAt: number): RouteBookmark {
  const request: RouteSearchRequest = {
    requestId: `0f8b1f0e-3ac2-4d63-9a6b-6c1f5e1a2d${id.padStart(2, "0")}`,
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
  createDemoSearch(request);
  const result = getDemoSearch(`demo-${request.requestId}`, true);
  return { id, label: `Маршрут ${id}`, savedAt, result, routingMode: "GREENEST" };
}

describe("route bookmarks", () => {
  it("round-trips a saved analysis so reopening it needs no provider request", () => {
    const storage = memoryStorage();
    const entry = bookmark("1", 1_000);
    writeRouteBookmarks([entry], storage);

    const restored = readRouteBookmarks(storage);
    expect(restored).toHaveLength(1);
    expect(restored[0]!.label).toBe("Маршрут 1");
    expect(restored[0]!.result.searchId).toBe(entry.result.searchId);
    expect(restored[0]!.result.greenTopRoutes.length).toBe(entry.result.greenTopRoutes.length);
    expect(isBookmarked(restored, entry.result.searchId)).toBe(true);
    expect(isBookmarked(restored, "other-search")).toBe(false);
  });

  it("keeps the newest bookmarks and updates rather than duplicates the same analysis", () => {
    const entries = Array.from({ length: MAX_ROUTE_BOOKMARKS + 2 }, (_, index) => bookmark(String(index + 1), index * 1_000));
    const stored = entries.reduce<RouteBookmark[]>((current, entry) => rememberRouteBookmark(current, entry), []);
    expect(stored).toHaveLength(MAX_ROUTE_BOOKMARKS);
    expect(stored[0]!.savedAt).toBeGreaterThan(stored[1]!.savedAt);

    const again = rememberRouteBookmark(stored, { ...stored[0]!, label: "Переименован", savedAt: 99_000 });
    expect(again).toHaveLength(MAX_ROUTE_BOOKMARKS);
    expect(again.filter((entry) => entry.result.searchId === stored[0]!.result.searchId)).toHaveLength(1);
    expect(again[0]!.label).toBe("Переименован");
  });

  it("drops corrupt records instead of losing the whole collection", () => {
    expect(parseRouteBookmarks("{oops")).toEqual([]);
    expect(parseRouteBookmarks(JSON.stringify({ version: 2, entries: [] }))).toEqual([]);
    const mixed = JSON.stringify({
      version: 1,
      entries: [{ id: "broken", label: "x", savedAt: 1, result: { nope: true } }],
    });
    expect(parseRouteBookmarks(mixed)).toEqual([]);
  });

  it("removes a bookmark by id and clears the record when none remain", () => {
    const storage = memoryStorage();
    const entry = bookmark("1", 1_000);
    writeRouteBookmarks([entry], storage);
    const remaining = removeRouteBookmark(readRouteBookmarks(storage), entry.id);
    writeRouteBookmarks(remaining, storage);
    expect(readRouteBookmarks(storage)).toEqual([]);
    expect(storage.getItem(ROUTE_BOOKMARKS_STORAGE_KEY)).toBeNull();
  });

  it("refreshes a bookmark saved before requests were stored", () => {
    // Records written by earlier versions carry only the analysis. The refresh
    // control must still work for them, rebuilt from the analysed endpoints.
    const legacy = bookmark("9", 9_000);
    expect(legacy.request).toBeUndefined();
    expect(canRefreshBookmark(legacy)).toBe(true);

    const rebuilt = reconstructBookmarkRequest(legacy, defaultRoutePreferences);
    const route = legacy.result.selectedRoute ?? legacy.result.greenTopRoutes[0]!;
    expect(rebuilt?.origin).toEqual(route.geometry[0]);
    expect(rebuilt?.destination).toEqual(route.geometry.at(-1));
    expect(rebuilt?.routingMode).toBe("GREENEST");
    expect(rebuilt?.maxExtraDistanceMeters).toBe(defaultRoutePreferences.extraDistanceKm * 1_000);

    // A stored request is replayed verbatim rather than approximated.
    const exact = { ...legacy, request: { ...rebuilt!, requestId: "11111111-2222-4333-8444-555555555555" } };
    expect(reconstructBookmarkRequest(exact, defaultRoutePreferences)).toBe(exact.request);
  });

  it("hides refresh only when the record has no geometry to re-ask from", () => {
    const empty = bookmark("8", 8_000);
    empty.result = { ...empty.result, selectedRoute: null, greenTopRoutes: [], fastestReferenceRoute: null };
    expect(canRefreshBookmark(empty)).toBe(false);
    expect(reconstructBookmarkRequest(empty, defaultRoutePreferences)).toBeUndefined();
  });
});
