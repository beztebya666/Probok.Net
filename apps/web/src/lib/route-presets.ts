import { z } from "zod";
import { GeoPointSchema, RoutingModeSchema, type GeoPoint, type RoutingMode } from "./schemas";

export const ROUTE_PRESETS_STORAGE_KEY = "greenroute.route-presets.v1";
export const ROUTE_PRESETS_VERSION = 1 as const;
export const MAX_ROUTE_PRESETS = 12;
export const TRAFFIC_PROVIDER_STORAGE_KEY = "greenroute.traffic-provider.v1";
const MAX_STORED_BYTES = 96_000;
// "osm" is a base map rather than a traffic source: OpenStreetMap has no
// congestion layer. It sits in the same control because from the user's side
// the question is one question — which map am I looking at.
const TrafficProviderSchema = z.enum(["off", "yandex", "2gis", "osm"]);
export type TrafficProvider = z.infer<typeof TrafficProviderSchema>;

export type StoredLocation = {
  label: string;
  point: GeoPoint;
};

export type RoutePresetDraft = {
  origin: StoredLocation;
  destination: StoredLocation;
  waypoints: StoredLocation[];
  routingMode: RoutingMode;
  extraDistanceKm: number;
  extraTimeMinutes: number;
  avoidTolls: boolean;
  avoidUnpaved: boolean;
  trafficProvider: TrafficProvider;
};

export type RoutePreset = RoutePresetDraft & {
  id: string;
  favorite: boolean;
  lastUsedAt: number;
};

const StoredLocationSchema = z.object({
  label: z.string().trim().min(1).max(240),
  point: GeoPointSchema,
}).strict();

const RoutePresetDraftSchema = z.object({
  origin: StoredLocationSchema,
  destination: StoredLocationSchema,
  waypoints: z.array(StoredLocationSchema).max(3),
  routingMode: RoutingModeSchema,
  extraDistanceKm: z.number().int().min(0).max(150),
  extraTimeMinutes: z.number().int().min(0).max(300),
  avoidTolls: z.boolean(),
  avoidUnpaved: z.boolean(),
  // v1 records created before traffic-provider persistence migrate to off.
  trafficProvider: TrafficProviderSchema.optional().default("off"),
}).strict();

const RoutePresetSchema = RoutePresetDraftSchema.extend({
  id: z.string().min(1).max(80),
  favorite: z.boolean(),
  lastUsedAt: z.number().int().nonnegative(),
}).strict();

const RoutePresetEnvelopeSchema = z.object({
  version: z.literal(ROUTE_PRESETS_VERSION),
  entries: z.array(RoutePresetSchema).max(64),
}).strict();

function canonicalDraft(draft: RoutePresetDraft): string {
  return JSON.stringify({
    origin: draft.origin,
    destination: draft.destination,
    waypoints: draft.waypoints,
    routingMode: draft.routingMode,
    extraDistanceKm: draft.extraDistanceKm,
    extraTimeMinutes: draft.extraTimeMinutes,
    avoidTolls: draft.avoidTolls,
    avoidUnpaved: draft.avoidUnpaved,
    trafficProvider: draft.trafficProvider,
  });
}

function capPresets(entries: RoutePreset[]): RoutePreset[] {
  const sorted = [...entries].sort((a, b) => b.lastUsedAt - a.lastUsedAt);
  while (sorted.length > MAX_ROUTE_PRESETS) {
    const oldestNonFavorite = sorted.findLastIndex((entry) => !entry.favorite);
    sorted.splice(oldestNonFavorite >= 0 ? oldestNonFavorite : sorted.length - 1, 1);
  }
  return sorted;
}

export function parseRoutePresets(raw: string | null): RoutePreset[] {
  if (!raw || raw.length > MAX_STORED_BYTES) return [];
  try {
    const parsed = RoutePresetEnvelopeSchema.safeParse(JSON.parse(raw));
    if (!parsed.success) return [];
    return capPresets(parsed.data.entries);
  } catch {
    return [];
  }
}

export function readRoutePresets(storage?: Pick<Storage, "getItem">): RoutePreset[] {
  if (!storage) return [];
  try {
    return parseRoutePresets(storage.getItem(ROUTE_PRESETS_STORAGE_KEY));
  } catch {
    return [];
  }
}

export function writeRoutePresets(entries: RoutePreset[], storage?: Pick<Storage, "setItem">): void {
  if (!storage) return;
  const valid = entries.flatMap((entry) => {
    const parsed = RoutePresetSchema.safeParse(entry);
    return parsed.success ? [parsed.data] : [];
  });
  try {
    storage.setItem(ROUTE_PRESETS_STORAGE_KEY, JSON.stringify({
      version: ROUTE_PRESETS_VERSION,
      entries: capPresets(valid),
    }));
  } catch {
    // Private browsing and storage quotas can make localStorage unavailable.
  }
}

export function rememberRoutePreset(
  entries: RoutePreset[],
  draft: RoutePresetDraft,
  id: string,
  lastUsedAt: number,
): RoutePreset[] {
  const parsedDraft = RoutePresetDraftSchema.safeParse(draft);
  if (!parsedDraft.success) return capPresets(entries);

  const key = canonicalDraft(parsedDraft.data);
  const existing = entries.find((entry) => canonicalDraft(entry) === key);
  const next: RoutePreset = {
    ...parsedDraft.data,
    id: existing?.id ?? id,
    favorite: existing?.favorite ?? false,
    lastUsedAt,
  };
  return capPresets([next, ...entries.filter((entry) => entry.id !== existing?.id)]);
}

export function toggleRoutePresetFavorite(entries: RoutePreset[], id: string): RoutePreset[] {
  return entries.map((entry) => entry.id === id ? { ...entry, favorite: !entry.favorite } : entry);
}

export function parseTrafficProvider(raw: string | null): TrafficProvider {
  if (!raw || raw.length > 128) return "off";
  try {
    const parsed = z.object({ version: z.literal(1), provider: TrafficProviderSchema }).strict().safeParse(JSON.parse(raw));
    return parsed.success ? parsed.data.provider : "off";
  } catch {
    return "off";
  }
}

export function readTrafficProvider(storage?: Pick<Storage, "getItem">): TrafficProvider {
  if (!storage) return "off";
  try {
    return parseTrafficProvider(storage.getItem(TRAFFIC_PROVIDER_STORAGE_KEY));
  } catch {
    return "off";
  }
}

export function writeTrafficProvider(provider: TrafficProvider, storage?: Pick<Storage, "setItem">): void {
  if (!storage || !TrafficProviderSchema.safeParse(provider).success) return;
  try {
    storage.setItem(TRAFFIC_PROVIDER_STORAGE_KEY, JSON.stringify({ version: 1, provider }));
  } catch {
    // Keep the current in-memory preference when persistence is unavailable.
  }
}
