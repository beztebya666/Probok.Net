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

const [input, originLabel, destinationLabel, primaryIndex = "0"] = process.argv.slice(2);
if (!input || !originLabel || !destinationLabel) {
  throw new Error('usage: anonymise-bookmark.mjs <bookmark.json> "<from>" "<to>" [primary-index]');
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

function routeLength(points) {
  let total = 0;
  for (let index = 1; index < points.length; index++) total += metresBetween(points[index - 1], points[index]);
  return total;
}

/**
 * Removes the first and last few hundred metres of a route.
 *
 * Trimming has to follow the road, not the straight line between the ends: an
 * earlier version kept only the segments lying near that line, which deleted
 * exactly the parts a detour is made of and left the map full of holes. The
 * measure here is distance walked along the route, so everything between the
 * two trimmed ends survives untouched.
 */
function trimRoute(route) {
  const total = routeLength(route.geometry);
  if (total <= 4 * TRIM_METRES) return route;

  const kept = [];
  let walked = 0;
  for (let index = 0; index < route.segments.length; index++) {
    const segment = route.segments[index];
    const length = routeLength(segment.geometry);
    const from = walked;
    walked += length;
    // Whole segments only: a segment is the unit the colours were measured on,
    // and half of one would be a number nobody reported.
    if (from >= TRIM_METRES && walked <= total - TRIM_METRES) kept.push(segment);
  }
  if (kept.length === 0) return route;

  const geometry = kept.flatMap((segment, index) => (index === 0 ? segment.geometry : segment.geometry.slice(1)));
  return {
    ...route,
    geometry,
    segments: kept,
    distanceMeters: Math.round(kept.reduce((sum, segment) => sum + segment.distanceMeters, 0)),
    liveDurationSeconds: kept.reduce((sum, segment) => sum + segment.liveDurationSeconds, 0),
    baselineDurationSeconds: kept.reduce((sum, segment) => sum + segment.baselineDurationSeconds, 0),
  };
}

const entries = JSON.parse(readFileSync(input, "utf8")).entries ?? [];
if (entries.length === 0) throw new Error("the export contains no bookmark");
// The primary analysis is what the demo opens with; the rest become its saved
// bookmarks, so more than one story is reachable without a provider call.
const order = [Number(primaryIndex), ...entries.keys()].filter((index, position, all) =>
  Number.isInteger(index) && index >= 0 && index < entries.length && all.indexOf(index) === position);

const trim = (route) => (route ? trimRoute(route) : route);

function anonymise(bookmark, id) {
  const source = bookmark.result;
  const result = {
  ...source,
  searchId: id,
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
  if (!anchor) throw new Error("a bookmark carries no routes");
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
  return { result, request, labels: { origin: originLabel, destination: destinationLabel }, capturedAt: source.generatedAt };
}

const shaped = order.map((index, position) => anonymise(entries[index], position === 0 ? "demo-frozen-analysis" : `demo-frozen-${position}`));
const payload = { ...shaped[0], extra: shaped.slice(1) };

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = process.env.PROBOK_ROOT ?? resolve(here, "..", "..");
const text = JSON.stringify(payload);
writeFileSync(join(repoRoot, "apps", "web", "public", "demo", "analysis.json"), text, "utf8");
writeFileSync(join(repoRoot, "tools", "docs-media", "demo-analysis.json"), text, "utf8");

const summary = payload.result.greenTopRoutes.map((route) => {
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
  reference: payload.result.fastestReferenceRoute && {
    minutes: Math.round(payload.result.fastestReferenceRoute.liveDurationSeconds / 60),
    km: +(payload.result.fastestReferenceRoute.distanceMeters / 1000).toFixed(1),
    red: `${Math.round((payload.result.fastestReferenceRoute.metrics?.redDurationSeconds ?? 0) / 60)} min`,
  },
  alsoSaved: payload.extra.length,
}, null, 2));
