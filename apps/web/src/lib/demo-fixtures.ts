import type {
  GeoSuggestion,
  RouteCandidate,
  RouteSearchRequest,
  RouteSearchResult,
  SearchEvent,
  Session,
} from "./schemas";

const now = () => new Date().toISOString();

// The demo mirrors the orchestrator's proof-of-search ranking so the podium is
// exercised locally with the same shape the real API returns.
function demoGreenTop(candidates: RouteCandidate[]): RouteCandidate[] {
  const greenShare = (route: RouteCandidate) => {
    const total = route.segments.reduce((sum, segment) => sum + segment.liveDurationSeconds, 0);
    if (total <= 0) return 0;
    return route.segments
      .filter((segment) => segment.congestionClass === "GREEN")
      .reduce((sum, segment) => sum + segment.liveDurationSeconds, 0) / total;
  };
  return [...new Map(candidates.map((route) => [route.candidateId, route])).values()]
    .filter((route) => route.trafficDataType !== "UNKNOWN" && route.trafficDataType !== "BASELINE")
    .sort((a, b) => greenShare(b) - greenShare(a) || a.liveDurationSeconds - b.liveDurationSeconds)
    .slice(0, 3);
}

const suggestions: GeoSuggestion[] = [
  {
    id: "demo-kremlin",
    label: "Московский Кремль",
    subtitle: "Москва, Кремль",
    point: { latitude: 55.75222, longitude: 37.61556 },
  },
  {
    id: "demo-vdnh",
    label: "ВДНХ",
    subtitle: "Москва, проспект Мира, 119",
    point: { latitude: 55.8299, longitude: 37.6331 },
  },
  {
    id: "demo-gorky",
    label: "Парк Горького",
    subtitle: "Москва, Крымский Вал, 9",
    point: { latitude: 55.72976, longitude: 37.6011 },
  },
  {
    id: "demo-city",
    label: "Москва-Сити",
    subtitle: "Москва, Пресненская набережная",
    point: { latitude: 55.7497, longitude: 37.5377 },
  },
];

const paths = {
  fastest: [
    { latitude: 55.75222, longitude: 37.61556 },
    { latitude: 55.765, longitude: 37.607 },
    { latitude: 55.781, longitude: 37.619 },
    { latitude: 55.799, longitude: 37.627 },
    { latitude: 55.8299, longitude: 37.6331 },
  ],
  recommended: [
    { latitude: 55.75222, longitude: 37.61556 },
    { latitude: 55.758, longitude: 37.655 },
    { latitude: 55.782, longitude: 37.671 },
    { latitude: 55.812, longitude: 37.661 },
    { latitude: 55.8299, longitude: 37.6331 },
  ],
  balanced: [
    { latitude: 55.75222, longitude: 37.61556 },
    { latitude: 55.77, longitude: 37.637 },
    { latitude: 55.792, longitude: 37.646 },
    { latitude: 55.812, longitude: 37.64 },
    { latitude: 55.8299, longitude: 37.6331 },
  ],
};

function segment(
  candidate: string,
  index: number,
  geometry: Array<{ latitude: number; longitude: number }>,
  congestionClass: "GREEN" | "YELLOW" | "ORANGE" | "RED" | "UNKNOWN",
  duration: number,
  distance: number,
) {
  const score = congestionClass === "UNKNOWN" ? 0.42 : 0.88;
  return {
    segmentId: `${candidate}-segment-${index}`,
    geometry,
    distanceMeters: distance,
    liveDurationSeconds: duration,
    baselineDurationSeconds: Math.max(60, Math.round(duration / (congestionClass === "RED" ? 1.8 : 1.08))),
    trafficRatio: congestionClass === "RED" ? 1.8 : congestionClass === "ORANGE" ? 1.45 : congestionClass === "UNKNOWN" ? 1 : 1.08,
    congestionClass,
    confidence: {
      level: score > 0.8 ? ("HIGH" as const) : ("LOW" as const),
      score,
      reasons: congestionClass === "UNKNOWN" ? ["INSUFFICIENT_BASELINE_DATA"] : ["GEOMETRY_MATCH_CONFIRMED"],
    },
    geometrySimilarity: congestionClass === "UNKNOWN" ? 0.61 : 0.94,
    source: "GREENROUTE_ESTIMATE",
  };
}

function candidate(
  candidateId: string,
  geometry: typeof paths.fastest,
  duration: number,
  distance: number,
  classes: Array<"GREEN" | "YELLOW" | "ORANGE" | "RED" | "UNKNOWN">,
  score: number,
  reasonCodes: string[],
): RouteCandidate {
  const partDistance = Math.round(distance / (geometry.length - 1));
  const partDuration = Math.round(duration / (geometry.length - 1));
  const segments = geometry.slice(0, -1).map((point, index) =>
    segment(candidateId, index, [point, geometry[index + 1]!], classes[index]!, partDuration, partDistance),
  );
  const redDurationSeconds = segments
    .filter((item) => item.congestionClass === "RED")
    .reduce((sum, item) => sum + item.liveDurationSeconds, 0);
  const unknownDurationSeconds = segments
    .filter((item) => item.congestionClass === "UNKNOWN")
    .reduce((sum, item) => sum + item.liveDurationSeconds, 0);

  return {
    candidateId,
    provider: "YANDEX",
	trafficDataType: unknownDurationSeconds ? "UNKNOWN" : "SYNTHETIC",
    geometry,
    distanceMeters: distance,
    liveDurationSeconds: duration,
    baselineDurationSeconds: duration - 240,
    trafficDelaySeconds: 240,
    segments,
    blocked: false,
    tolls: false,
    unpaved: false,
    confidence: {
      level: unknownDurationSeconds ? "MEDIUM" : "HIGH",
      score: unknownDurationSeconds ? 0.66 : 0.88,
      reasons: unknownDurationSeconds ? ["PARTIAL_BASELINE_COVERAGE"] : ["SUFFICIENT_BASELINE_COVERAGE"],
    },
    score,
    reasonCodes,
	explanation: unknownDurationSeconds
		? "Часть маршрута не имеет достаточных данных; неопределённость показана явно."
		: "Демонстрационная оценка маршрута в пределах заданных ограничений.",
    metrics: {
      redDurationSeconds,
      unknownDurationSeconds,
      stopAndGoScore: redDurationSeconds ? 0.7 : 0.18,
      routeStabilityScore: unknownDurationSeconds ? 0.58 : 0.86,
    },
    generatedBy: candidateId === "route-green" ? "ENHANCED_SEARCH" : "INITIAL_PROVIDER_ALTERNATIVE",
    providerRequestCount: candidateId === "route-green" ? 3 : 1,
  };
}

const fastest = candidate(
  "route-fastest",
  paths.fastest,
  1_680,
  13_800,
  ["YELLOW", "RED", "ORANGE", "GREEN"],
  68.4,
  ["FASTEST_REFERENCE", "TRAFFIC_DELAY_PRESENT"],
);
const recommended = candidate(
  "route-green",
  paths.recommended,
  1_920,
  16_500,
  ["GREEN", "GREEN", "YELLOW", "GREEN"],
  91.2,
  ["LOW_RED_EXPOSURE", "WITHIN_DETOUR_LIMIT", "STABLE_FLOW"],
);
const balanced = candidate(
  "route-balanced",
  paths.balanced,
  1_790,
  14_700,
  ["GREEN", "UNKNOWN", "YELLOW", "GREEN"],
  79.1,
  ["SHORT_DETOUR", "PARTIAL_BASELINE_COVERAGE"],
);

const searches = new Map<string, { request: RouteSearchRequest; startedAt: number }>();

function withEndpoints(route: RouteCandidate, request: RouteSearchRequest): RouteCandidate {
  const geometry = route.geometry.map((point) => ({ ...point }));
  geometry[0] = request.origin;
  geometry[geometry.length - 1] = request.destination;
  const segments = route.segments.map((item, index) => ({
    ...item,
    geometry: [geometry[index]!, geometry[index + 1]!],
  }));
  return { ...route, geometry, segments };
}

export function demoSuggestions(query: string): GeoSuggestion[] {
  const normalized = query.toLocaleLowerCase("ru");
  const matching = suggestions.filter((suggestion) =>
    `${suggestion.label} ${suggestion.subtitle ?? ""}`.toLocaleLowerCase("ru").includes(normalized),
  );
  return (matching.length ? matching : suggestions).slice(0, 5);
}

export function createDemoSearch(request: RouteSearchRequest): RouteSearchResult {
  const searchId = `demo-${request.requestId}`;
  searches.set(searchId, { request, startedAt: Date.now() });
  const reference = withEndpoints(fastest, request);
  return {
    searchId,
    status: "SEARCHING",
    selectedRoute: reference,
    alternatives: [withEndpoints(balanced, request)],
    bestEffortRoutes: [],
    greenTopRoutes: demoGreenTop([reference, withEndpoints(balanced, request)]),
    fastestReferenceRoute: reference,
    constraints: {
      maxExtraDistanceMeters: request.maxExtraDistanceMeters,
      maxExtraTimeSeconds: request.maxExtraTimeSeconds,
      routingMode: request.routingMode,
    },
    providerUsage: { requestCount: 2, budget: request.maxProviderRequests },
    warnings: [],
    generatedAt: now(),
    expiresAt: new Date(Date.now() + 10 * 60_000).toISOString(),
  };
}

export function getDemoSearch(searchId: string, forceComplete = false): RouteSearchResult {
  const entry = searches.get(searchId);
  if (!entry) throw new Error("DEMO_SEARCH_NOT_FOUND");
  const complete = forceComplete || Date.now() - entry.startedAt > 1_200;
  const reference = withEndpoints(fastest, entry.request);
  const green = withEndpoints(recommended, entry.request);
  const middle = withEndpoints(balanced, entry.request);
  return {
    searchId,
    status: complete ? "COMPLETED" : "SEARCHING",
    selectedRoute: complete && entry.request.routingMode !== "FASTEST" ? green : reference,
    alternatives: complete ? [reference, middle, green] : [middle],
    bestEffortRoutes: [],
    greenTopRoutes: demoGreenTop(complete ? [green, middle, reference] : [middle, reference]),
    fastestReferenceRoute: reference,
    constraints: {
      maxExtraDistanceMeters: entry.request.maxExtraDistanceMeters,
      maxExtraTimeSeconds: entry.request.maxExtraTimeSeconds,
      routingMode: entry.request.routingMode,
    },
    providerUsage: { requestCount: complete ? 6 : 2, budget: entry.request.maxProviderRequests },
    warnings: middle.confidence.level === "MEDIUM" ? [{ code: "PARTIAL_BASELINE_COVERAGE", message: undefined }] : [],
    generatedAt: now(),
    expiresAt: new Date(Date.now() + 10 * 60_000).toISOString(),
  };
}

export function cancelDemoSearch(searchId: string): void {
  searches.delete(searchId);
}

export async function playDemoEvents(
  searchId: string,
  signal: AbortSignal,
  onEvent: (event: SearchEvent) => void,
): Promise<void> {
  const steps: Array<Omit<SearchEvent, "eventId" | "searchId" | "timestamp">> = [
    { type: "INITIAL_CANDIDATES_READY", message: "Provider alternatives received", progress: 30 },
    { type: "BASELINE_EVALUATION_STARTED", message: "Baseline matching", progress: 52 },
    { type: "SCORING_STARTED", message: "Candidate scoring", progress: 76 },
    { type: "COMPLETED", message: "Search completed", progress: 100, result: getDemoSearch(searchId, true) },
  ];
  for (const [index, step] of steps.entries()) {
    await new Promise<void>((resolve, reject) => {
      const timer = window.setTimeout(resolve, 280);
      signal.addEventListener("abort", () => {
        window.clearTimeout(timer);
        reject(new DOMException("Aborted", "AbortError"));
      }, { once: true });
    });
    onEvent({ ...step, eventId: String(index + 1), searchId, timestamp: now() });
  }
}

export const demoSession: Session = {
  userId: "demo-admin",
  displayName: "Demo Operator",
  roles: ["user", "admin"],
  authenticated: true,
};
