import { describe, expect, it } from "vitest";
import {
  MAX_ROUTE_PRESETS,
  parseTrafficProvider,
  parseRoutePresets,
  rememberRoutePreset,
  ROUTE_PRESETS_VERSION,
  toggleRoutePresetFavorite,
  type RoutePresetDraft,
} from "./route-presets";

function draft(index = 0): RoutePresetDraft {
  return {
    origin: { label: `Откуда ${index}`, point: { latitude: 55.7 + index * 0.001, longitude: 37.6 } },
    destination: { label: `Куда ${index}`, point: { latitude: 55.8, longitude: 37.7 + index * 0.001 } },
    waypoints: [],
    routingMode: "STRICT_GREEN",
    extraDistanceKm: 30,
    extraTimeMinutes: 20,
    avoidTolls: false,
    avoidUnpaved: true,
    trafficProvider: "2gis",
  };
}

describe("local route presets", () => {
  it("fails closed for malformed, oversized and unknown-version storage", () => {
    expect(parseRoutePresets("not-json")).toEqual([]);
    expect(parseRoutePresets(JSON.stringify({ version: 2, entries: [] }))).toEqual([]);
    expect(parseRoutePresets("x".repeat(96_001))).toEqual([]);
    expect(parseRoutePresets(JSON.stringify({
      version: ROUTE_PRESETS_VERSION,
      entries: [{ id: "bad", favorite: false, lastUsedAt: 1, ...draft(), extraDistanceKm: 999 }],
    }))).toEqual([]);
  });

  it("migrates saved routes without traffic provider and validates the independent preference", () => {
    const legacy = { id: "legacy", favorite: false, lastUsedAt: 1, ...draft() } as Record<string, unknown>;
    delete legacy.trafficProvider;
    const parsed = parseRoutePresets(JSON.stringify({ version: ROUTE_PRESETS_VERSION, entries: [legacy] }));
    expect(parsed[0]?.trafficProvider).toBe("off");
    expect(parseTrafficProvider(JSON.stringify({ version: 1, provider: "2gis" }))).toBe("2gis");
    expect(parseTrafficProvider(JSON.stringify({ version: 1, provider: "invalid" }))).toBe("off");
  });

  it("deduplicates a repeated draft and preserves its favourite state", () => {
    let entries = rememberRoutePreset([], draft(), "first-id", 100);
    entries = toggleRoutePresetFavorite(entries, "first-id");
    entries = rememberRoutePreset(entries, draft(), "discarded-id", 200);

    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({ id: "first-id", favorite: true, lastUsedAt: 200 });
  });

  it("enforces the cap while retaining pinned routes ahead of disposable history", () => {
    let entries = rememberRoutePreset([], draft(0), "favorite", 1);
    entries = toggleRoutePresetFavorite(entries, "favorite");
    for (let index = 1; index <= MAX_ROUTE_PRESETS + 4; index += 1) {
      entries = rememberRoutePreset(entries, draft(index), `route-${index}`, index + 1);
    }

    expect(entries).toHaveLength(MAX_ROUTE_PRESETS);
    expect(entries.some((entry) => entry.id === "favorite" && entry.favorite)).toBe(true);
    expect(entries[0]?.id).toBe(`route-${MAX_ROUTE_PRESETS + 4}`);
  });
});
