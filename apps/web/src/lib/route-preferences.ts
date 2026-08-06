import { z } from "zod";
import { RoutingModeSchema, type RoutingMode } from "./schemas";

export const ROUTE_PREFERENCES_STORAGE_KEY = "greenroute.route-preferences.v1";
const MAX_STORED_BYTES = 4_000;

export const MAX_EXTRA_DISTANCE_KM = 150;
export const MAX_EXTRA_TIME_MINUTES = 300;

export type RoutePreferences = {
  routingMode: RoutingMode;
  extraDistanceKm: number;
  extraTimeMinutes: number;
  avoidTolls: boolean;
  avoidUnpaved: boolean;
};

export const defaultRoutePreferences: RoutePreferences = {
  routingMode: "GREENEST",
  extraDistanceKm: 30,
  extraTimeMinutes: 20,
  avoidTolls: false,
  avoidUnpaved: true,
};

const RoutePreferencesSchema = z.object({
  routingMode: RoutingModeSchema,
  extraDistanceKm: z.number().int().min(0).max(MAX_EXTRA_DISTANCE_KM),
  extraTimeMinutes: z.number().int().min(0).max(MAX_EXTRA_TIME_MINUTES),
  avoidTolls: z.boolean(),
  avoidUnpaved: z.boolean(),
}).strict();

const EnvelopeSchema = z.object({
  version: z.literal(1),
  preferences: RoutePreferencesSchema,
}).strict();

export function parseRoutePreferences(raw: string | null): RoutePreferences {
  if (!raw || raw.length > MAX_STORED_BYTES) return defaultRoutePreferences;
  try {
    const parsed = EnvelopeSchema.safeParse(JSON.parse(raw));
    return parsed.success ? parsed.data.preferences : defaultRoutePreferences;
  } catch {
    return defaultRoutePreferences;
  }
}

export function readRoutePreferences(storage?: Pick<Storage, "getItem">): RoutePreferences {
  if (!storage) return defaultRoutePreferences;
  try {
    return parseRoutePreferences(storage.getItem(ROUTE_PREFERENCES_STORAGE_KEY));
  } catch {
    return defaultRoutePreferences;
  }
}

export function writeRoutePreferences(preferences: RoutePreferences, storage?: Pick<Storage, "setItem">): void {
  const parsed = RoutePreferencesSchema.safeParse(preferences);
  if (!storage || !parsed.success) return;
  try {
    storage.setItem(ROUTE_PREFERENCES_STORAGE_KEY, JSON.stringify({ version: 1, preferences: parsed.data }));
  } catch {
    // Private browsing and storage quotas can make localStorage unavailable.
  }
}

/**
 * How much longer than the fastest route a candidate may be, in percent.
 *
 * The absolute kilometre allowance is meaningless on short trips without this:
 * a 6 km trip with a +150 km allowance would still reject every real detour if
 * the percentage stayed at its conservative default. The green modes exist
 * precisely to trade distance for free-flow, so they use the full documented
 * range and let the kilometre and minute limits do the bounding.
 */
export function extraDistancePercentFor(mode: RoutingMode): number {
  return mode === "GREENEST" || mode === "STRICT_GREEN" ? 300 : 100;
}

/**
 * How much of the route must be confirmed green for a candidate to qualify,
 * from "anything faster wins" to "every segment or nothing".
 */
export function strictnessFor(mode: RoutingMode): number {
  if (mode === "STRICT_GREEN") return 1;
  if (mode === "GREENEST") return 0.82;
  return mode === "BALANCED" ? 0.5 : 0;
}

/**
 * Provider request budget for one search. Each probe of the green ladder costs
 * one routing request, and 2GIS free-tier daily quotas are small, so the green
 * modes get a deliberately bounded search depth rather than an open budget.
 */
export function providerRequestBudgetFor(mode: RoutingMode): number {
  if (mode === "FASTEST") return 2;
  if (mode === "BALANCED") return 4;
  return 8;
}
