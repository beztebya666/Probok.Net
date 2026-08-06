import type { CongestionClass, RouteCandidate, RouteSearchResult, RouteSegment, RoutingMode } from "./schemas";

const congestionWeight: Record<CongestionClass, number> = {
  GREEN: 0,
  YELLOW: 1,
  ORANGE: 2,
  RED: 4,
  UNKNOWN: Number.POSITIVE_INFINITY,
};

export function routeCandidates(result: {
  selectedRoute: RouteCandidate | null;
  alternatives: RouteCandidate[];
  fastestReferenceRoute: RouteCandidate | null;
}): RouteCandidate[] {
  const candidates = [result.selectedRoute, result.fastestReferenceRoute, ...result.alternatives].filter(
    (route): route is RouteCandidate => Boolean(route),
  );
  return [...new Map(candidates.map((route) => [route.candidateId, route])).values()];
}

/**
 * STRICT_GREEN is deliberately fail-closed in the browser. A route is only
 * presented as an all-green result when both the candidate and every segment
 * carry usable traffic evidence and none of the segments is anything other
 * than green. Provider reference routes remain useful for scoring, but they
 * must never silently become the user's strict result.
 */
export function isVerifiedAllGreenRoute(route: RouteCandidate | null | undefined): route is RouteCandidate {
  if (!route || route.blocked || route.segments.length === 0) return false;
  if (["UNKNOWN", "BASELINE"].includes(route.trafficDataType) || route.confidence.level === "LOW") return false;
  return route.segments.every(
    (segment) => segment.congestionClass === "GREEN" && segment.confidence.level !== "LOW",
  );
}

export function routeCandidatesForMode(result: RouteSearchResult, routingMode?: RoutingMode): RouteCandidate[] {
  const candidates = routeCandidates(result);
  if (routingMode !== "STRICT_GREEN") return candidates;

  // A strict result is authoritative only when the orchestrator selected a
  // candidate that also passes the browser's fail-closed evidence check.
  if (!isVerifiedAllGreenRoute(result.selectedRoute)) return [];
  return candidates.filter(isVerifiedAllGreenRoute);
}

export function defaultCandidateId(result: RouteSearchResult, routingMode?: RoutingMode): string | undefined {
  if (routingMode === "STRICT_GREEN") {
    return isVerifiedAllGreenRoute(result.selectedRoute) ? result.selectedRoute.candidateId : undefined;
  }
  return result.selectedRoute?.candidateId ?? result.fastestReferenceRoute?.candidateId ?? undefined;
}

export function selectedCandidateForMode(
  result: RouteSearchResult,
  selectedCandidateId: string | undefined,
  routingMode?: RoutingMode,
): RouteCandidate | undefined {
  const candidates = routeCandidatesForMode(result, routingMode);
  const explicitlySelected = candidates.find((candidate) => candidate.candidateId === selectedCandidateId);
  if (explicitlySelected) return explicitlySelected;
  if (routingMode === "STRICT_GREEN") {
    return isVerifiedAllGreenRoute(result.selectedRoute) ? result.selectedRoute : undefined;
  }
  return result.selectedRoute ?? result.fastestReferenceRoute ?? undefined;
}

export function congestionSeconds(route: RouteCandidate, congestion: CongestionClass): number {
  const metricKey = `${congestion.toLowerCase()}DurationSeconds`;
  const metric = route.metrics[metricKey];
  if (typeof metric === "number" && Number.isFinite(metric)) return metric;
  return route.segments
    .filter((segment) => segment.congestionClass === congestion)
    .reduce((total, segment) => total + segment.liveDurationSeconds, 0);
}

export function estimatedFreePercent(route: RouteCandidate): number {
  const total = route.segments.reduce((sum, segment) => sum + segment.distanceMeters, 0);
  if (total <= 0) return 0;
  const green = route.segments
    .filter((segment) => segment.congestionClass === "GREEN")
    .reduce((sum, segment) => sum + segment.distanceMeters, 0);
  return Math.round((green / total) * 100);
}

export function unknownPercent(route: RouteCandidate): number {
  const total = route.segments.reduce((sum, segment) => sum + segment.distanceMeters, 0);
  if (total <= 0) return 100;
  const unknown = route.segments
    .filter((segment) => segment.congestionClass === "UNKNOWN")
    .reduce((sum, segment) => sum + segment.distanceMeters, 0);
  return Math.round((unknown / total) * 100);
}

/**
 * Share of the trip that is confidently green, by driving time. This is the
 * headline number of the product: "how much of this trip do I actually spend
 * moving freely". Length not covered by segment evidence counts as not-green,
 * so the percentage can never be inflated by missing data.
 */
export function greenTimePercent(route: RouteCandidate): number {
  const covered = route.segments.reduce((sum, segment) => sum + segment.liveDurationSeconds, 0);
  const total = Math.max(covered, route.liveDurationSeconds);
  if (total <= 0) return 0;
  const green = route.segments
    .filter((segment) => segment.congestionClass === "GREEN")
    .reduce((sum, segment) => sum + segment.liveDurationSeconds, 0);
  return Math.round((green / total) * 100);
}

export function greenDistancePercent(route: RouteCandidate): number {
  const covered = route.segments.reduce((sum, segment) => sum + segment.distanceMeters, 0);
  const total = Math.max(covered, route.distanceMeters);
  if (total <= 0) return 0;
  const green = route.segments
    .filter((segment) => segment.congestionClass === "GREEN")
    .reduce((sum, segment) => sum + segment.distanceMeters, 0);
  return Math.round((green / total) * 100);
}

/**
 * Every distinct route this search produced, including the ones the routing
 * mode rejected. The result pane needs this to prove what was searched even
 * when the strict mode legitimately selects nothing.
 */
export function allSearchedRoutes(result: RouteSearchResult): RouteCandidate[] {
  const candidates = [
    ...result.greenTopRoutes,
    result.selectedRoute,
    ...result.alternatives,
    ...result.bestEffortRoutes,
    result.fastestReferenceRoute,
  ].filter((route): route is RouteCandidate => Boolean(route));
  return [...new Map(candidates.map((route) => [route.candidateId, route])).values()];
}

/**
 * Top routes by measured green share. The orchestrator already ranks these, but
 * the browser re-ranks the union of everything it received so a partial SSE
 * snapshot still shows a correctly ordered podium.
 */
export function greenRankedRoutes(result: RouteSearchResult, limit = 3): RouteCandidate[] {
  return allSearchedRoutes(result)
    .filter((route) => route.trafficDataType !== "UNKNOWN" && route.trafficDataType !== "BASELINE" && route.segments.length > 0)
    .sort(
      (a, b) =>
        greenTimePercent(b) - greenTimePercent(a) ||
        greenDistancePercent(b) - greenDistancePercent(a) ||
        congestionSeconds(a, "RED") - congestionSeconds(b, "RED") ||
        a.liveDurationSeconds - b.liveDurationSeconds,
    )
    .slice(0, limit);
}

export function greenScore(route: RouteCandidate): number {
  return route.segments.reduce(
    (total, segment) => total + congestionWeight[segment.congestionClass] * segment.liveDurationSeconds,
    0,
  );
}

export function findGreenest(routes: RouteCandidate[]): RouteCandidate | undefined {
  const comparable = routes.filter(
    (route) =>
      route.trafficDataType !== "UNKNOWN" &&
      route.trafficDataType !== "BASELINE" &&
      route.confidence.level !== "LOW" &&
      unknownPercent(route) === 0,
  );
  return [...comparable].sort((a, b) => greenScore(a) - greenScore(b) || a.liveDurationSeconds - b.liveDurationSeconds)[0];
}

export function segmentColor(segment: RouteSegment): string {
  return {
    GREEN: "#238554",
    YELLOW: "#d1a20a",
    ORANGE: "#d96b26",
    RED: "#c93b3b",
    UNKNOWN: "#66746d",
  }[segment.congestionClass];
}

export function formatDistance(meters: number, locale: "ru" | "en"): string {
  if (meters < 1_000) return `${Math.round(meters)} ${locale === "ru" ? "м" : "m"}`;
  return `${new Intl.NumberFormat(locale, { maximumFractionDigits: 1 }).format(meters / 1_000)} ${locale === "ru" ? "км" : "km"}`;
}

export function formatDuration(seconds: number, locale: "ru" | "en"): string {
	const rounded = Math.round(seconds / 60);
	const minutes = seconds <= 0 ? 0 : Math.max(1, rounded);
  const hours = Math.floor(minutes / 60);
  const rest = minutes % 60;
  if (!hours) return `${minutes} ${locale === "ru" ? "мин" : "min"}`;
  return locale === "ru" ? `${hours} ч ${rest} мин` : `${hours} h ${rest} min`;
}

/**
 * The routes the interface shows for a mode — one answer for the list and the
 * map, so a card can never offer a route the map refuses to draw.
 *
 * When the strict mode qualifies nothing, the ranked candidates are shown
 * instead of an empty screen, minus the fastest reference: strict mode must
 * never present the fast route as its answer (ADR-014).
 */
export function displayedRoutes(result: RouteSearchResult, routingMode?: RoutingMode): RouteCandidate[] {
  const qualified = routeCandidatesForMode(result, routingMode);
  if (qualified.length > 0) return qualified;
  const reference = result.fastestReferenceRoute?.candidateId;
  return greenRankedRoutes(result, 3).filter(
    (route) => routingMode !== "STRICT_GREEN" || route.candidateId !== reference,
  );
}

/** The route the map draws: the chosen one, or the best of what is shown. */
export function displayedSelectedRoute(
  result: RouteSearchResult,
  selectedCandidateId: string | undefined,
  routingMode?: RoutingMode,
): RouteCandidate | undefined {
  const shown = displayedRoutes(result, routingMode);
  return shown.find((route) => route.candidateId === selectedCandidateId) ?? shown[0];
}
