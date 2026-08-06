import type { ZodType } from "zod";
import type { components } from "@/generated/api";
import {
  AdminOverviewSchema,
  ApiErrorSchema,
  GeosuggestResponseSchema,
  RouteSearchRequestSchema,
  RouteSearchResultSchema,
  SessionSchema,
  type AdminOverview,
  type GeoSuggestion,
  type RouteSearchRequest,
  type RouteSearchResult,
  type Session,
} from "./schemas";
import { getRuntimeConfig } from "./runtime-config";
import {
  cancelDemoSearch,
  createDemoSearch,
  demoSession,
  demoSuggestions,
  getDemoSearch,
} from "./demo-fixtures";

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly code: string,
    readonly correlationId?: string,
    readonly retryAfterSeconds?: number,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

function apiUrl(path: string): string {
  const config = getRuntimeConfig();
  if (!config.configured || !config.edgeApiBaseUrl) {
    throw new ApiError("Edge API is not configured", 503, "CLIENT_CONFIGURATION_ERROR");
  }
  return `${config.edgeApiBaseUrl}${path.startsWith("/") ? path : `/${path}`}`;
}

function commonHeaders(): HeadersInit {
  return {
    Accept: "application/json",
  };
}

async function parseError(response: Response): Promise<ApiError> {
  const correlationHeader = response.headers.get("x-correlation-id") ?? undefined;
  const retryAfterRaw = response.headers.get("retry-after");
  const retryAfterSeconds = retryAfterRaw && /^\d+$/.test(retryAfterRaw) ? Number(retryAfterRaw) : undefined;
  let payload: unknown;
  try {
    const text = (await response.text()).slice(0, 65_536);
    payload = text ? JSON.parse(text) : {};
  } catch {
    payload = {};
  }
  const parsed = ApiErrorSchema.safeParse(payload);
  const error = parsed.success ? parsed.data : {};
  return new ApiError(
    error.detail ?? error.message ?? error.title ?? `Request failed with status ${response.status}`,
    response.status,
    error.code ?? error.type ?? `HTTP_${response.status}`,
    error.correlationId ?? error.requestId ?? correlationHeader,
    retryAfterSeconds,
  );
}

async function requestJson<T>(path: string, schema: ZodType<T>, init: RequestInit = {}): Promise<T> {
  const response = await fetch(apiUrl(path), {
    ...init,
    credentials: "include",
    cache: "no-store",
    headers: { ...commonHeaders(), ...init.headers },
  });
  if (!response.ok) throw await parseError(response);
  const payload: unknown = await response.json();
  const parsed = schema.safeParse(payload);
  if (!parsed.success) {
    throw new ApiError("Edge API returned an invalid response", 502, "INVALID_API_RESPONSE");
  }
  return parsed.data;
}

export async function geosuggest(
  query: string,
  language: "ru" | "en",
  signal?: AbortSignal,
): Promise<GeoSuggestion[]> {
  const config = getRuntimeConfig();
  if (config.demoMode) return demoSuggestions(query);
  const parameters = new URLSearchParams({ q: query, lang: language === "ru" ? "ru_RU" : "en_US", limit: "6" });
  const response = await requestJson(`/api/v1/geosuggest?${parameters}`, GeosuggestResponseSchema, signal ? { signal } : {});
  return response.suggestions;
}

export async function createRouteSearch(request: RouteSearchRequest): Promise<RouteSearchResult> {
  const validated = RouteSearchRequestSchema.parse(request);
  if (getRuntimeConfig().demoMode) return createDemoSearch(validated);
  const { requestId, ...body } = validated;
  const contractBody: components["schemas"]["RouteSearchRequest"] = body;
  return requestJson("/api/v1/route-searches", RouteSearchResultSchema, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "Idempotency-Key": requestId,
      "X-Request-ID": requestId,
    },
    body: JSON.stringify(contractBody),
  });
}

export async function getRouteSearch(searchId: string, signal?: AbortSignal): Promise<RouteSearchResult> {
  if (getRuntimeConfig().demoMode) return getDemoSearch(searchId);
  return requestJson(`/api/v1/route-searches/${encodeURIComponent(searchId)}`, RouteSearchResultSchema, signal ? { signal } : {});
}

export async function cancelRouteSearch(searchId: string): Promise<void> {
  if (getRuntimeConfig().demoMode) {
    cancelDemoSearch(searchId);
    return;
  }
  const response = await fetch(apiUrl(`/api/v1/route-searches/${encodeURIComponent(searchId)}`), {
    method: "DELETE",
    credentials: "include",
    cache: "no-store",
    headers: commonHeaders(),
  });
  if (!response.ok && response.status !== 404 && response.status !== 409) throw await parseError(response);
}

export async function getSession(signal?: AbortSignal): Promise<Session> {
  if (getRuntimeConfig().demoMode) return demoSession;
  return requestJson("/api/v1/me", SessionSchema, signal ? { signal } : {});
}

export async function getAdminOverview(signal?: AbortSignal): Promise<AdminOverview> {
  if (getRuntimeConfig().demoMode) {
    throw new ApiError("Operational telemetry is unavailable in demo mode", 503, "DEMO_TELEMETRY_UNAVAILABLE");
  }
  return requestJson("/api/v1/admin/overview", AdminOverviewSchema, signal ? { signal } : {});
}

export function routeEventsRequest(searchId: string, lastEventId?: string): { url: string; init: RequestInit } {
  return {
    url: apiUrl(`/api/v1/route-searches/${encodeURIComponent(searchId)}/events`),
    init: {
      method: "GET",
      credentials: "include",
      cache: "no-store",
      headers: {
        Accept: "text/event-stream",
        ...(lastEventId ? { "Last-Event-ID": lastEventId } : {}),
      },
    },
  };
}

export { parseError };
