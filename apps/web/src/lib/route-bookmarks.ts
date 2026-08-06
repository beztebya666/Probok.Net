import { extraDistancePercentFor, providerRequestBudgetFor, strictnessFor, type RoutePreferences } from "./route-preferences";
import { RouteSearchRequestSchema, RouteSearchResultSchema, RoutingModeSchema, type RouteSearchRequest, type RouteSearchResult, type RoutingMode } from "./schemas";

export const ROUTE_BOOKMARKS_STORAGE_KEY = "greenroute.bookmarks.v1";
export const MAX_ROUTE_BOOKMARKS = 5;

// Bookmarks carry full route geometry, so the whole collection is capped well
// below the localStorage budget. Saved routes and preferences matter more and
// must not be evicted by a large analysis.
const MAX_STORED_BYTES = 2_400_000;

export type RouteBookmark = {
  id: string;
  label: string;
  savedAt: number;
  routingMode?: RoutingMode | undefined;
  selectedCandidateId?: string | undefined;
  result: RouteSearchResult;
  // The search that produced this analysis, kept so it can be re-run on demand.
  // Absent on records saved before refresh existed; those simply cannot refresh.
  request?: RouteSearchRequest | undefined;
};

type StoredBookmark = Omit<RouteBookmark, "result"> & { result: unknown };

function parseBookmark(raw: unknown): RouteBookmark | undefined {
  if (!raw || typeof raw !== "object") return undefined;
  const value = raw as Partial<StoredBookmark>;
  if (typeof value.id !== "string" || !value.id || value.id.length > 80) return undefined;
  if (typeof value.label !== "string" || !value.label || value.label.length > 240) return undefined;
  if (typeof value.savedAt !== "number" || !Number.isFinite(value.savedAt)) return undefined;

  const result = RouteSearchResultSchema.safeParse(value.result);
  if (!result.success) return undefined;
  const mode = RoutingModeSchema.safeParse(value.routingMode);
  const request = RouteSearchRequestSchema.safeParse((value as { request?: unknown }).request);
  return {
    ...(request.success ? { request: request.data } : {}),
    id: value.id,
    label: value.label,
    savedAt: value.savedAt,
    result: result.data,
    ...(mode.success ? { routingMode: mode.data } : {}),
    ...(typeof value.selectedCandidateId === "string" && value.selectedCandidateId
      ? { selectedCandidateId: value.selectedCandidateId }
      : {}),
  };
}

export function parseRouteBookmarks(raw: string | null): RouteBookmark[] {
  if (!raw || raw.length > MAX_STORED_BYTES) return [];
  try {
    const envelope = JSON.parse(raw) as Record<string, unknown>;
    if (envelope?.["version"] !== 1 || !Array.isArray(envelope["entries"])) return [];
    return envelope["entries"]
      .slice(0, MAX_ROUTE_BOOKMARKS)
      .map(parseBookmark)
      .filter((entry): entry is RouteBookmark => Boolean(entry));
  } catch {
    return [];
  }
}

export function readRouteBookmarks(storage?: Pick<Storage, "getItem">): RouteBookmark[] {
  if (!storage) return [];
  try {
    return parseRouteBookmarks(storage.getItem(ROUTE_BOOKMARKS_STORAGE_KEY));
  } catch {
    return [];
  }
}

/**
 * Writes as many of the newest bookmarks as fit the byte budget. Dropping the
 * oldest is preferable to failing the whole write and silently losing the one
 * the user just made.
 */
export function writeRouteBookmarks(entries: RouteBookmark[], storage?: Pick<Storage, "setItem" | "removeItem">): void {
  if (!storage) return;
  const ordered = [...entries].sort((a, b) => b.savedAt - a.savedAt).slice(0, MAX_ROUTE_BOOKMARKS);
  try {
    for (let count = ordered.length; count > 0; count--) {
      const payload = JSON.stringify({ version: 1, entries: ordered.slice(0, count) });
      if (payload.length <= MAX_STORED_BYTES) {
        storage.setItem(ROUTE_BOOKMARKS_STORAGE_KEY, payload);
        return;
      }
    }
    storage.removeItem(ROUTE_BOOKMARKS_STORAGE_KEY);
  } catch {
    // Quota or private browsing: bookmarks stay in memory for this session only.
  }
}

export function rememberRouteBookmark(entries: RouteBookmark[], bookmark: RouteBookmark): RouteBookmark[] {
  // One bookmark per analysis: re-saving the same search updates it in place.
  const withoutDuplicate = entries.filter((entry) => entry.result.searchId !== bookmark.result.searchId);
  return [bookmark, ...withoutDuplicate].sort((a, b) => b.savedAt - a.savedAt).slice(0, MAX_ROUTE_BOOKMARKS);
}

export function removeRouteBookmark(entries: RouteBookmark[], id: string): RouteBookmark[] {
  return entries.filter((entry) => entry.id !== id);
}

export function isBookmarked(entries: RouteBookmark[], searchId: string | undefined): boolean {
  return Boolean(searchId) && entries.some((entry) => entry.result.searchId === searchId);
}

function bookmarkEndpoints(bookmark: RouteBookmark) {
  const route = bookmark.result.selectedRoute
    ?? bookmark.result.greenTopRoutes[0]
    ?? bookmark.result.fastestReferenceRoute;
  const origin = route?.geometry[0];
  const destination = route?.geometry.at(-1);
  return origin && destination ? { origin, destination } : undefined;
}

/** Whether the refresh control has anything to re-ask for this record. */
export function canRefreshBookmark(bookmark: RouteBookmark): boolean {
  return Boolean(bookmark.request ?? bookmarkEndpoints(bookmark));
}

/**
 * Rebuilds a runnable request for a bookmark saved before requests were stored.
 *
 * Endpoints come from the analysed geometry itself, the mode from the record;
 * the allowances fall back to the current preferences. It is a faithful re-ask
 * of the same trip rather than a byte-identical replay, which is what a refresh
 * needs to be useful on older records.
 */
export function reconstructBookmarkRequest(
  bookmark: RouteBookmark,
  preferences: RoutePreferences,
): RouteSearchRequest | undefined {
  if (bookmark.request) return bookmark.request;
  const endpoints = bookmarkEndpoints(bookmark);
  if (!endpoints) return undefined;
  const routingMode = bookmark.routingMode ?? preferences.routingMode;
  return {
    requestId: crypto.randomUUID(),
    ...endpoints,
    waypoints: [],
    departureTime: new Date().toISOString(),
    routingMode,
    maxExtraDistanceMeters: preferences.extraDistanceKm * 1_000,
    maxExtraDistancePercent: extraDistancePercentFor(routingMode),
    maxExtraTimeSeconds: preferences.extraTimeMinutes * 60,
    avoidTolls: preferences.avoidTolls,
    avoidUnpaved: preferences.avoidUnpaved,
    strictness: strictnessFor(routingMode),
    maxProviderRequests: providerRequestBudgetFor(routingMode),
    searchDeadlineMs: 20_000,
  };
}
