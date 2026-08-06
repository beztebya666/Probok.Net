import { RouteSearchRequestSchema, RouteSearchResultSchema, RoutingModeSchema, type RouteSearchRequest, type RouteSearchResult, type RoutingMode } from "./schemas";

export const ROUTE_RESULT_CACHE_KEY = "greenroute.last-result.v1";

// Route geometry is large. Writing more than this into localStorage risks
// evicting the user's saved routes and preferences, which matter more than a
// restorable result.
const MAX_STORED_BYTES = 900_000;

export type CachedRouteResult = {
  result: RouteSearchResult;
  // Kept so a restored analysis can be refreshed: without the question, the
  // answer on screen cannot be asked again.
  request?: RouteSearchRequest | undefined;
  routingMode?: RoutingMode | undefined;
  selectedCandidateId?: string | undefined;
  savedAt: number;
};

/**
 * The last completed analysis, kept so that leaving the page and coming back
 * shows the same routes instead of spending another provider request on a
 * question that was already answered.
 */
export function parseCachedRouteResult(raw: string | null, now: number): CachedRouteResult | undefined {
  if (!raw || raw.length > MAX_STORED_BYTES) return undefined;
  try {
    const envelope = JSON.parse(raw) as Record<string, unknown>;
    if (envelope?.["version"] !== 1) return undefined;
    const parsed = RouteSearchResultSchema.safeParse(envelope["result"]);
    if (!parsed.success) return undefined;
    // A result the orchestrator has already expired is stale evidence about
    // live traffic and must not be presented as current.
    if (Date.parse(parsed.data.expiresAt) <= now) return undefined;

    const mode = RoutingModeSchema.safeParse(envelope["routingMode"]);
    const request = RouteSearchRequestSchema.safeParse(envelope["request"]);
    const selected = envelope["selectedCandidateId"];
    return {
      result: parsed.data,
      ...(request.success ? { request: request.data } : {}),
      ...(mode.success ? { routingMode: mode.data } : {}),
      ...(typeof selected === "string" && selected ? { selectedCandidateId: selected } : {}),
      savedAt: typeof envelope["savedAt"] === "number" ? envelope["savedAt"] : now,
    };
  } catch {
    return undefined;
  }
}

export function readCachedRouteResult(storage: Pick<Storage, "getItem"> | undefined, now: number): CachedRouteResult | undefined {
  if (!storage) return undefined;
  try {
    return parseCachedRouteResult(storage.getItem(ROUTE_RESULT_CACHE_KEY), now);
  } catch {
    return undefined;
  }
}

export function writeCachedRouteResult(
  entry: Omit<CachedRouteResult, "savedAt">,
  storage: Pick<Storage, "setItem" | "removeItem"> | undefined,
  now: number,
): void {
  if (!storage) return;
  try {
    const payload = JSON.stringify({
      version: 1,
      savedAt: now,
      routingMode: entry.routingMode,
      selectedCandidateId: entry.selectedCandidateId,
      request: entry.request,
      result: entry.result,
    });
    if (payload.length > MAX_STORED_BYTES) {
      storage.removeItem(ROUTE_RESULT_CACHE_KEY);
      return;
    }
    storage.setItem(ROUTE_RESULT_CACHE_KEY, payload);
  } catch {
    // Quota or private browsing: the result simply is not restorable.
  }
}

export function clearCachedRouteResult(storage: Pick<Storage, "removeItem"> | undefined): void {
  if (!storage) return;
  try {
    storage.removeItem(ROUTE_RESULT_CACHE_KEY);
  } catch {
    // Nothing to do: the cache is best effort in both directions.
  }
}
