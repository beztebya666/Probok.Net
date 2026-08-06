import { asset } from "./base-path";
import { RouteSearchRequestSchema, RouteSearchResultSchema, type RouteSearchRequest, type RouteSearchResult } from "./schemas";

/**
 * A finished analysis shipped with the demo build.
 *
 * Without it the demo opens on an empty planner and an empty map, which reads
 * as a broken deployment rather than a working product: nothing appears until
 * the visitor happens to type two addresses. The file holds real road geometry
 * with a fixed, synthetic set of segment colours, and the page says so.
 */
export type DemoPreload = {
  result: RouteSearchResult;
  request: RouteSearchRequest;
  origin: string;
  destination: string;
};

export async function loadDemoPreload(signal?: AbortSignal): Promise<DemoPreload | undefined> {
  try {
    const response = await fetch(asset("/demo/analysis.json"), signal ? { signal } : {});
    if (!response.ok) return undefined;
    const body = (await response.json()) as Record<string, unknown>;
    const result = RouteSearchResultSchema.safeParse(body["result"]);
    const request = RouteSearchRequestSchema.safeParse(body["request"]);
    if (!result.success || !request.success) return undefined;
    const labels = body["labels"] as { origin?: unknown; destination?: unknown } | undefined;
    return {
      result: result.data,
      request: request.data,
      origin: typeof labels?.origin === "string" ? labels.origin : "",
      destination: typeof labels?.destination === "string" ? labels.destination : "",
    };
  } catch {
    // A demo that cannot preload is still a working demo.
    return undefined;
  }
}
