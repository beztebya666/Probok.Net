/**
 * Turns a saved analysis into the fixture the public demo ships.
 *
 * A bookmark is a real trip: its labels name the addresses and its geometry
 * starts and ends at the door. The demo is public, so both are removed here —
 * the endpoints are trimmed back onto the road network and the labels are
 * replaced with nearby landmarks. Everything that makes the analysis worth
 * showing (the road the route takes, the measured segment colours, the times)
 * is untouched.
 *
 *   node tools/docs-media/anonymise-bookmark.mjs <bookmark.json> "<from>" "<to>"
 */
import { readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join, resolve } from "node:path";
import { randomUUID } from "node:crypto";

const [input, originLabel, destinationLabel] = process.argv.slice(2);
if (!input || !originLabel || !destinationLabel) {
  throw new Error('usage: anonymise-bookmark.mjs <bookmark.json> "<from>" "<to>"');
}

// Far enough that the ends land on a through road rather than a driveway.
const TRIM_METRES = 400;

function metresBetween(a, b) {
  const R = 6_371_000;
  const toRad = (deg) => (deg * Math.PI) / 180;
  const dLat = toRad(b.latitude - a.latitude);
  const dLon = toRad(b.longitude - a.longitude);
  const h = Math.sin(dLat / 2) ** 2 +
    Math.cos(toRad(a.latitude)) * Math.cos(toRad(b.latitude)) * Math.sin(dLon / 2) ** 2;
  return 2 * R * Math.asin(Math.sqrt(h));
}

function trimStart(points, metres) {
  let walked = 0;
  for (let index = 1; index < points.length; index++) {
    walked += metresBetween(points[index - 1], points[index]);
    if (walked >= metres) return points.slice(index);
  }
  return points;
}

function trimEnds(points) {
  const forward = trimStart(points, TRIM_METRES);
  const backward = trimStart([...forward].reverse(), TRIM_METRES).reverse();
  return backward.length >= 2 ? backward : points;
}

function trimRoute(route) {
  const geometry = trimEnds(route.geometry);
  const first = geometry[0];
  const last = geometry[geometry.length - 1];
  const inside = (point) =>
    metresBetween(point, first) + metresBetween(point, last) <=
    metresBetween(first, last) + 2 * TRIM_METRES;
  // A segment survives only if it still lies within the trimmed corridor; the
  // ones covering the removed ends go with them.
  const segments = route.segments
    .map((segment) => ({ ...segment, geometry: segment.geometry.filter(inside) }))
    .filter((segment) => segment.geometry.length >= 2);
  return { ...route, geometry, segments };
}

const bookmark = JSON.parse(readFileSync(input, "utf8")).entries[0];
if (!bookmark) throw new Error("the export contains no bookmark");
const source = bookmark.result;

const trim = (route) => (route ? trimRoute(route) : route);
const result = {
  ...source,
  searchId: "demo-frozen-analysis",
  requestId: randomUUID(),
  selectedRoute: trim(source.selectedRoute),
  greenTopRoutes: (source.greenTopRoutes ?? []).map(trim),
  alternatives: (source.alternatives ?? []).map(trim),
  bestEffortRoutes: (source.bestEffortRoutes ?? []).map(trim),
  fastestReferenceRoute: trim(source.fastestReferenceRoute),
  // The demo must not expire: it is a frozen exhibit, not a live answer.
  expiresAt: "2099-01-01T00:00:00.000Z",
};

const anchor = result.greenTopRoutes[0] ?? result.bestEffortRoutes[0];
if (!anchor) throw new Error("the bookmark carries no routes");
const constraints = source.constraints ?? {};
const request = bookmark.request ?? {
  requestId: result.requestId,
  origin: anchor.geometry[0],
  destination: anchor.geometry[anchor.geometry.length - 1],
  waypoints: [],
  departureTime: source.generatedAt,
  routingMode: bookmark.routingMode ?? "GREENEST",
  maxExtraDistanceMeters: Number(constraints.maxExtraDistanceMeters ?? 150_000),
  maxExtraDistancePercent: Number(constraints.maxExtraDistancePercent ?? 300),
  maxExtraTimeSeconds: Number(constraints.maxExtraTimeSeconds ?? 18_000),
  avoidTolls: Boolean(constraints.avoidTolls),
  avoidUnpaved: constraints.avoidUnpaved !== false,
  strictness: 0.82,
  maxProviderRequests: 8,
  searchDeadlineMs: 20_000,
};

const payload = {
  result,
  request,
  labels: { origin: originLabel, destination: destinationLabel },
  capturedAt: source.generatedAt,
};

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = process.env.PROBOK_ROOT ?? resolve(here, "..", "..");
const text = JSON.stringify(payload);
writeFileSync(join(repoRoot, "apps", "web", "public", "demo", "analysis.json"), text, "utf8");
writeFileSync(join(repoRoot, "tools", "docs-media", "demo-analysis.json"), text, "utf8");

const summary = result.greenTopRoutes.map((route) => {
  const total = route.liveDurationSeconds;
  const metrics = route.metrics ?? {};
  return {
    green: `${Math.round(((metrics.greenDurationSeconds ?? 0) / total) * 100)}%`,
    minutes: Math.round(total / 60),
    km: +(route.distanceMeters / 1000).toFixed(1),
    red: `${Math.round((metrics.redDurationSeconds ?? 0) / 60)} min`,
  };
});
console.log(JSON.stringify({
  capturedAt: payload.capturedAt,
  trimmedMetres: TRIM_METRES,
  origin: payload.labels.origin,
  destination: payload.labels.destination,
  routes: summary,
  reference: result.fastestReferenceRoute && {
    minutes: Math.round(result.fastestReferenceRoute.liveDurationSeconds / 60),
    km: +(result.fastestReferenceRoute.distanceMeters / 1000).toFixed(1),
    red: `${Math.round((result.fastestReferenceRoute.metrics?.redDurationSeconds ?? 0) / 60)} min`,
  },
}, null, 2));
