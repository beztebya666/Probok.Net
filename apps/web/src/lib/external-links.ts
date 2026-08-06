import type { GeoPoint, RouteCandidate } from "./schemas";

// 2GIS web directions accept at most ten points. Yandex accepts a longer
// `rtext` chain, and more points is what keeps the external route pinned to
// ours through turns and U-turns.
const MAX_TWO_GIS_POINTS = 10;
const MAX_YANDEX_POINTS = 24;
const COORDINATE_PRECISION = 6;
const METERS_PER_DEGREE = 111_320;
// How far past a turn a via point is placed, and how close two of them may sit.
const VIA_POINT_OFFSET_METERS = 60;
const MIN_VIA_SPACING_METERS = 40;

function round(value: number): string {
  return Number(value.toFixed(COORDINATE_PRECISION)).toString();
}

function toMeters(point: GeoPoint, origin: GeoPoint): [number, number] {
  const scale = Math.cos((origin.latitude * Math.PI) / 180);
  return [(point.longitude - origin.longitude) * METERS_PER_DEGREE * scale, (point.latitude - origin.latitude) * METERS_PER_DEGREE];
}

function perpendicularMeters(point: GeoPoint, start: GeoPoint, end: GeoPoint): number {
  const [px, py] = toMeters(point, start);
  const [ex, ey] = toMeters(end, start);
  const lengthSquared = ex * ex + ey * ey;
  if (lengthSquared === 0) return Math.hypot(px, py);
  const t = Math.max(0, Math.min(1, (px * ex + py * ey) / lengthSquared));
  return Math.hypot(px - ex * t, py - ey * t);
}

/**
 * Ramer–Douglas–Peucker. Keeps the vertices that carry the shape of the line
 * and drops the ones that only interpolate a straight stretch.
 */
function simplify(points: GeoPoint[], toleranceMeters: number): GeoPoint[] {
  if (points.length < 3) return points;
  const keep = new Array<boolean>(points.length).fill(false);
  keep[0] = true;
  keep[points.length - 1] = true;

  const stack: Array<[number, number]> = [[0, points.length - 1]];
  while (stack.length > 0) {
    const [first, last] = stack.pop()!;
    let farthest = -1;
    let farthestDistance = toleranceMeters;
    for (let index = first + 1; index < last; index++) {
      const distance = perpendicularMeters(points[index]!, points[first]!, points[last]!);
      if (distance > farthestDistance) {
        farthest = index;
        farthestDistance = distance;
      }
    }
    if (farthest > 0) {
      keep[farthest] = true;
      stack.push([first, farthest], [farthest, last]);
    }
  }
  return points.filter((_, index) => keep[index]);
}

function metersBetween(a: GeoPoint, b: GeoPoint): number {
  const [dx, dy] = toMeters(b, a);
  return Math.hypot(dx, dy);
}

/**
 * Moves a via point off a turn apex and a short way down the outgoing leg.
 *
 * RDP deliberately selects maximum-deviation vertices, which are junctions —
 * and a junction is exactly where the opposite carriageway, a slip road or a
 * parallel service road lies within snapping distance. A point snapped onto the
 * wrong one forces the external router into a U-turn to reach it. A few dozen
 * metres past the apex the geometry is unambiguous and one-directional.
 */
function nudgeDownstream(points: GeoPoint[], index: number, metersAhead: number): GeoPoint {
  let travelled = 0;
  for (let cursor = index; cursor < points.length - 1; cursor++) {
    travelled += metersBetween(points[cursor]!, points[cursor + 1]!);
    if (travelled >= metersAhead) return points[cursor + 1]!;
  }
  return points[index]!;
}

/**
 * Picks the via points for an external directions link.
 *
 * Evenly spaced points are a poor way to pin a route: they land in the middle
 * of straight stretches, where the external router had no choice to make, and
 * miss the junctions where it did. These points are chosen at the turns
 * instead — the tolerance is raised until the simplified line fits the URL
 * budget — so the external map is constrained exactly where it would otherwise
 * diverge.
 */
export function externalRoutePoints(geometry: GeoPoint[], maximum = MAX_TWO_GIS_POINTS): GeoPoint[] {
  const valid = geometry.filter((point) => Number.isFinite(point.latitude) && Number.isFinite(point.longitude));
  if (valid.length <= 2 || valid.length <= maximum) return valid;

  let low = 5;
  let high = 20_000;
  let best = simplify(valid, high);
  for (let iteration = 0; iteration < 24 && high - low > 1; iteration++) {
    const middle = (low + high) / 2;
    const candidate = simplify(valid, middle);
    if (candidate.length > maximum) {
      low = middle;
    } else {
      high = middle;
      best = candidate;
    }
  }
  // The tolerance search converges from above, so `best` is the most detailed
  // simplification that still fits. A pathological line can still exceed it.
  const chosen = best.length <= maximum ? best : best.slice(0, maximum - 1).concat(best[best.length - 1]!);

  // Endpoints stay exact; the intermediate ones step off their apex.
  const indexOf = new Map(valid.map((point, index) => [`${point.latitude},${point.longitude}`, index]));
  const nudged: GeoPoint[] = [chosen[0]!];
  for (const point of chosen.slice(1, -1)) {
    const index = indexOf.get(`${point.latitude},${point.longitude}`);
    const moved = index === undefined ? point : nudgeDownstream(valid, index, VIA_POINT_OFFSET_METERS);
    if (metersBetween(nudged[nudged.length - 1]!, moved) >= MIN_VIA_SPACING_METERS) nudged.push(moved);
  }
  nudged.push(chosen[chosen.length - 1]!);
  return nudged;
}

/**
 * 2GIS web directions accept a pipe-separated list of `lon,lat` points.
 */
export function twoGisRouteUrl(route: Pick<RouteCandidate, "geometry">): string | undefined {
  const points = externalRoutePoints(route.geometry, MAX_TWO_GIS_POINTS);
  if (points.length < 2) return undefined;
  // No leading separator: it creates an empty first slot, which 2GIS fills with
  // the viewer's own location instead of the requested origin.
  const path = points.map((point) => `${round(point.longitude)},${round(point.latitude)}`).join("|");
  return `https://2gis.ru/directions/points/${encodeURIComponent(path)}?traffic=`;
}

/**
 * Yandex Maps accept `rtext` as `lat,lon` pairs separated by `~`, with `rtt=auto`
 * selecting the car router.
 */
export function yandexRouteUrl(route: Pick<RouteCandidate, "geometry">): string | undefined {
  const points = externalRoutePoints(route.geometry, MAX_YANDEX_POINTS);
  if (points.length < 2) return undefined;
  const rtext = points.map((point) => `${round(point.latitude)},${round(point.longitude)}`).join("~");
  return `https://yandex.ru/maps/?${new URLSearchParams({ rtext, rtt: "auto" }).toString()}`;
}
