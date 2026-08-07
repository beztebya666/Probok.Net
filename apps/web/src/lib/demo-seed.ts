import type { DemoPreload } from "./demo-preload";
import { readRouteBookmarks, writeRouteBookmarks, type RouteBookmark } from "./route-bookmarks";
import { MAX_EXTRA_DISTANCE_KM, MAX_EXTRA_TIME_MINUTES, defaultRoutePreferences, readRoutePreferences, writeRoutePreferences } from "./route-preferences";
import { readRoutePresets, readTrafficProvider, writeRoutePresets, writeTrafficProvider, type RoutePreset } from "./route-presets";

/**
 * Fills the demo's saved lists so its features are visible rather than merely
 * present.
 *
 * A first-time visitor cannot tell that favourites, recent searches and saved
 * analyses exist when all three tabs are empty. This writes one of each, using
 * the same storage the app uses at runtime, and only when the visitor has
 * nothing of their own: anybody who has already used the page keeps their list.
 */
const DAY = 24 * 60 * 60 * 1000;

export function seedDemoLists(preload: DemoPreload, storage: Storage | undefined, now: number): void {
  if (!storage) return;
  seedPresets(preload, storage, now);
  seedBookmarks(preload, storage, now);
  seedPreferences(preload, storage);
  seedBaseMap(storage);
}

/**
 * The planner must say what the shown analysis was actually asked for.
 * Without this the demo opened with "Свободнее" selected above routes marked
 * "не весь зелёный" — a picture of the interface contradicting itself.
 */
function seedPreferences(preload: DemoPreload, storage: Storage): void {
  const stored = readRoutePreferences(storage);
  if (stored.routingMode !== defaultRoutePreferences.routingMode) return;
  writeRoutePreferences({
    routingMode: preload.request.routingMode,
    extraDistanceKm: Math.min(MAX_EXTRA_DISTANCE_KM, Math.round(preload.request.maxExtraDistanceMeters / 1000)),
    extraTimeMinutes: Math.min(MAX_EXTRA_TIME_MINUTES, Math.round(preload.request.maxExtraTimeSeconds / 60)),
    avoidTolls: preload.request.avoidTolls,
    avoidUnpaved: preload.request.avoidUnpaved,
  }, storage);
}

/**
 * The demo ships no provider credential, so OpenStreetMap is the only map it
 * can draw. Choosing it here rather than falling back to it inside the map
 * keeps a development build honest: there a missing key still reports itself.
 */
function seedBaseMap(storage: Storage): void {
  if (readTrafficProvider(storage) !== "off") return;
  writeTrafficProvider("osm", storage);
}

function seedPresets(preload: DemoPreload, storage: Storage, now: number): void {
  if (readRoutePresets(storage).length > 0) return;
  const base = {
    waypoints: [],
    routingMode: preload.request.routingMode,
    extraDistanceKm: Math.round(preload.request.maxExtraDistanceMeters / 1000),
    extraTimeMinutes: Math.round(preload.request.maxExtraTimeSeconds / 60),
    avoidTolls: preload.request.avoidTolls,
    avoidUnpaved: preload.request.avoidUnpaved,
    trafficProvider: "2gis" as const,
  };
  const trip = {
    origin: { label: preload.origin, point: preload.request.origin },
    destination: { label: preload.destination, point: preload.request.destination },
  };
  const presets: RoutePreset[] = [
    // Starred: the trip the open analysis belongs to.
    { ...base, ...trip, id: "demo-favourite", favorite: true, lastUsedAt: now },
    // Recent: the same pair the other way round, and a shorter city hop, so the
    // recent tab shows what it is for.
    {
      ...base,
      origin: trip.destination,
      destination: trip.origin,
      id: "demo-recent-return",
      favorite: false,
      lastUsedAt: now - 2 * 60 * 60 * 1000,
    },
    {
      ...base,
      origin: { label: "Московский Кремль", point: { latitude: 55.75222, longitude: 37.61556 } },
      destination: { label: "ВДНХ", point: { latitude: 55.8299, longitude: 37.6331 } },
      id: "demo-recent-vdnh",
      favorite: false,
      lastUsedAt: now - DAY,
    },
  ];
  writeRoutePresets(presets, storage);
}

function seedBookmarks(preload: DemoPreload, storage: Storage, now: number): void {
  if (readRouteBookmarks(storage).length > 0) return;
  const analyses = [preload, ...preload.extra];
  const bookmarks: RouteBookmark[] = analyses.map((analysis, index) => ({
    id: analysis.result.searchId,
    label: `${analysis.origin} → ${analysis.destination}`,
    // Ordered so the open analysis is the newest of the saved ones.
    savedAt: now - index * 60 * 60 * 1000,
    routingMode: analysis.request.routingMode,
    result: analysis.result,
    request: analysis.request,
    ...(analysis.result.greenTopRoutes[0]
      ? { selectedCandidateId: analysis.result.greenTopRoutes[0].candidateId }
      : {}),
  }));
  writeRouteBookmarks(bookmarks, storage);
}
