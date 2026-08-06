import { describe, expect, it } from "vitest";
import {
  defaultRoutePreferences,
  extraDistancePercentFor,
  parseRoutePreferences,
  providerRequestBudgetFor,
  readRoutePreferences,
  ROUTE_PREFERENCES_STORAGE_KEY,
  writeRoutePreferences,
  type RoutePreferences,
} from "./route-preferences";

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

const preferences: RoutePreferences = {
  routingMode: "STRICT_GREEN",
  extraDistanceKm: 150,
  extraTimeMinutes: 300,
  avoidTolls: true,
  avoidUnpaved: false,
};

describe("route preferences", () => {
  it("round-trips the mode and detour allowance so they survive a reload", () => {
    const storage = memoryStorage();
    writeRoutePreferences(preferences, storage);
    expect(readRoutePreferences(storage)).toEqual(preferences);
    expect(storage.getItem(ROUTE_PREFERENCES_STORAGE_KEY)).toContain("STRICT_GREEN");
  });

  it("falls back to defaults for absent, corrupt and out-of-range records", () => {
    expect(readRoutePreferences(undefined)).toEqual(defaultRoutePreferences);
    expect(parseRoutePreferences(null)).toEqual(defaultRoutePreferences);
    expect(parseRoutePreferences("{not json")).toEqual(defaultRoutePreferences);
    expect(
      parseRoutePreferences(JSON.stringify({ version: 1, preferences: { ...preferences, extraDistanceKm: 4_000 } })),
    ).toEqual(defaultRoutePreferences);
  });

  it("gives green modes the full documented detour percentage and a deeper search budget", () => {
    expect(extraDistancePercentFor("GREENEST")).toBe(300);
    expect(extraDistancePercentFor("STRICT_GREEN")).toBe(300);
    expect(extraDistancePercentFor("FASTEST")).toBe(100);
    expect(providerRequestBudgetFor("STRICT_GREEN")).toBeGreaterThan(providerRequestBudgetFor("BALANCED"));
    expect(providerRequestBudgetFor("FASTEST")).toBe(2);
  });
});
