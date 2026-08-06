/**
 * Builds the saved analysis used by the README screenshots.
 *
 * The daily 2GIS Routing API allowance is 50 objects, and a single green search
 * spends up to eight of them, so re-shooting the documentation must not depend
 * on it. Road geometry comes from the public OSRM demo server, which knows the
 * real street network; the traffic colours are a fixed, obviously synthetic
 * pattern. The screenshots are taken in demo mode, where the app labels itself
 * "Демо-данные", so nothing here is presented as a live measurement.
 *
 *   node tools/docs-media/build-demo-analysis.mjs
 */
import { writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const OSRM = "https://router.project-osrm.org/route/v1/driving";

// Кутузовский проспект, 32 → улица Академика Королёва, 12 (Останкино). Public
// addresses on purpose: documentation should not carry anybody's home or work.
const ORIGIN = { latitude: 55.7411, longitude: 37.5343 };
const DESTINATION = { latitude: 55.8206, longitude: 37.6118 };
// Each variant is nudged through a different part of the city so the three
// routes are genuinely different roads rather than the same line three times.
const VARIANTS = [
  { id: "green-rank-1", via: [{ latitude: 55.7601, longitude: 37.5045 }, { latitude: 55.8082, longitude: 37.5323 }], label: "через Мнёвники" },
  { id: "green-rank-2", via: [{ latitude: 55.7684, longitude: 37.5623 }], label: "через Пресню" },
  { id: "green-rank-3", via: [{ latitude: 55.7539, longitude: 37.6215 }, { latitude: 55.7801, longitude: 37.6335 }], label: "через центр" },
];

// Deterministic pseudo-randomness: same fixture on every machine and every run.
function makeRandom(seed) {
  let state = seed >>> 0;
  return () => {
    state = (state * 1664525 + 1013904223) >>> 0;
    return state / 0x1_0000_0000;
  };
}

function haversineMeters(a, b) {
  const R = 6_371_000;
  const toRad = (deg) => (deg * Math.PI) / 180;
  const dLat = toRad(b.latitude - a.latitude);
  const dLon = toRad(b.longitude - a.longitude);
  const lat1 = toRad(a.latitude);
  const lat2 = toRad(b.latitude);
  const h = Math.sin(dLat / 2) ** 2 + Math.cos(lat1) * Math.cos(lat2) * Math.sin(dLon / 2) ** 2;
  return 2 * R * Math.asin(Math.sqrt(h));
}

async function fetchRoute(points) {
  const path = points.map((p) => `${p.longitude},${p.latitude}`).join(";");
  const response = await fetch(`${OSRM}/${path}?overview=full&geometries=geojson`);
  if (!response.ok) throw new Error(`OSRM ${response.status}`);
  const body = await response.json();
  const route = body.routes?.[0];
  if (!route) throw new Error("OSRM returned no route");
  return {
    geometry: route.geometry.coordinates.map(([longitude, latitude]) => ({ latitude, longitude })),
    distanceMeters: route.distance,
    baselineDurationSeconds: route.duration,
  };
}

// Congestion classes in the proportions a green route actually has: mostly
// free-flow with a few slower stretches where it crosses a main road.
const PATTERNS = [
  { green: 0.83, mix: ["YELLOW", "ORANGE", "GREEN", "YELLOW"] },
  { green: 0.79, mix: ["YELLOW", "ORANGE", "RED", "YELLOW"] },
  { green: 0.74, mix: ["ORANGE", "YELLOW", "RED", "ORANGE"] },
];

const RATIO = { GREEN: 1.02, YELLOW: 1.25, ORANGE: 1.55, RED: 2.1, UNKNOWN: 1.1 };

function buildSegments(geometry, pattern, seed) {
  const random = makeRandom(seed);
  const segments = [];
  const target = 26;
  const step = Math.max(2, Math.floor(geometry.length / target));
  let mixIndex = 0;

  for (let start = 0; start < geometry.length - 1; start += step) {
    const slice = geometry.slice(start, Math.min(start + step + 1, geometry.length));
    if (slice.length < 2) break;
    const distanceMeters = slice.slice(1).reduce((sum, point, index) => sum + haversineMeters(slice[index], point), 0);
    if (distanceMeters <= 0) continue;

    const green = random() < pattern.green;
    const congestionClass = green ? "GREEN" : pattern.mix[mixIndex++ % pattern.mix.length];
    const ratio = RATIO[congestionClass];
    // 11 m/s ≈ 40 km/h of free-flow city driving.
    const baselineDurationSeconds = distanceMeters / 11;
    segments.push({
      segmentId: `seg-${segments.length + 1}`,
      geometry: slice,
      distanceMeters: Math.round(distanceMeters),
      liveDurationSeconds: Math.round(baselineDurationSeconds * ratio),
      baselineDurationSeconds: Math.round(baselineDurationSeconds),
      trafficRatio: ratio,
      congestionClass,
      confidence: { score: congestionClass === "UNKNOWN" ? 0.4 : 0.88, level: congestionClass === "UNKNOWN" ? "LOW" : "HIGH", reasons: ["DEMO_FIXTURE"] },
      source: "demo",
    });
  }
  return segments;
}

function durationByClass(segments, congestionClass) {
  return segments
    .filter((segment) => segment.congestionClass === congestionClass)
    .reduce((sum, segment) => sum + segment.liveDurationSeconds, 0);
}

function buildCandidate(variant, route, pattern, index) {
  const segments = buildSegments(route.geometry, pattern, 7 + index * 31);
  const liveDurationSeconds = segments.reduce((sum, segment) => sum + segment.liveDurationSeconds, 0);
  const baselineDurationSeconds = segments.reduce((sum, segment) => sum + segment.baselineDurationSeconds, 0);
  const greenSeconds = durationByClass(segments, "GREEN");
  const share = Math.round((greenSeconds / liveDurationSeconds) * 100);

  return {
    candidateId: variant.id,
    provider: "2gis",
    trafficDataType: "SYNTHETIC",
    geometry: route.geometry,
    distanceMeters: Math.round(route.distanceMeters),
    liveDurationSeconds,
    baselineDurationSeconds,
    trafficDelaySeconds: Math.max(0, liveDurationSeconds - baselineDurationSeconds),
    segments,
    blocked: false,
    tolls: false,
    unpaved: false,
    confidence: { score: 0.88, level: "HIGH", reasons: ["DEMO_FIXTURE"] },
    score: 100 - index,
    reasonCodes: [`GREEN_RANK_${index + 1}`, "DETOUR_ACCEPTED"],
    explanation: `${share}% времени по зелёному, ${variant.label}`,
    metrics: {
      greenDurationSeconds: greenSeconds,
      yellowDurationSeconds: durationByClass(segments, "YELLOW"),
      orangeDurationSeconds: durationByClass(segments, "ORANGE"),
      redDurationSeconds: durationByClass(segments, "RED"),
      unknownDurationSeconds: durationByClass(segments, "UNKNOWN"),
    },
    generatedBy: "demo-fixture",
    providerRequestCount: index + 1,
  };
}

const routes = [];
for (const [index, variant] of VARIANTS.entries()) {
  const route = await fetchRoute([ORIGIN, ...variant.via, DESTINATION]);
  routes.push(buildCandidate(variant, route, PATTERNS[index], index));
  console.log(`${variant.id}: ${(route.distanceMeters / 1000).toFixed(1)} км, ${route.geometry.length} точек`);
}
routes.sort((a, b) => b.metrics.greenDurationSeconds / b.liveDurationSeconds - a.metrics.greenDurationSeconds / a.liveDurationSeconds);

const generatedAt = "2026-08-06T09:41:12.000Z";
const result = {
  searchId: "demo-analysis-readme",
  requestId: "3f2b7c14-8f1e-4c8a-9d02-6b5a1e77c410",
  status: "COMPLETED",
  selectedRoute: routes[0],
  alternatives: routes.slice(1),
  bestEffortRoutes: [],
  greenTopRoutes: routes,
  fastestReferenceRoute: routes[routes.length - 1],
  constraints: {
    maxExtraDistanceMeters: 150_000,
    maxExtraDistancePercent: 300,
    maxExtraTimeSeconds: 18_000,
    minimumConfidenceScore: 0.35,
    avoidTolls: false,
    avoidUnpaved: true,
  },
  providerUsage: { provider: "2gis", requestsUsed: 6, requestBudget: 8, estimatedBillableUnits: 6, estimatedCost: 0, budgetExhausted: false },
  warnings: [],
  scoringPolicyVersion: "greenroute-scoring-v2.0.0",
  generatedAt,
  expiresAt: "2036-08-06T09:41:12.000Z",
};

const request = {
  requestId: "3f2b7c14-8f1e-4c8a-9d02-6b5a1e77c410",
  origin: ORIGIN,
  destination: DESTINATION,
  waypoints: [],
  departureTime: generatedAt,
  routingMode: "GREENEST",
  maxExtraDistanceMeters: 150_000,
  maxExtraDistancePercent: 300,
  maxExtraTimeSeconds: 18_000,
  avoidTolls: false,
  avoidUnpaved: true,
  strictness: 0.82,
  maxProviderRequests: 8,
  searchDeadlineMs: 20_000,
};

const labels = { origin: "Кутузовский проспект, 32", destination: "улица Академика Королёва, 12" };
const payload = JSON.stringify({ result, request, labels });
const here = dirname(fileURLToPath(import.meta.url));
const out = join(here, "demo-analysis.json");
writeFileSync(out, payload, "utf8");
// The same analysis is what the browser demo opens with.
writeFileSync(join(here, "..", "..", "apps", "web", "public", "demo", "analysis.json"), payload, "utf8");
console.log(`→ ${out}`);
for (const route of routes) {
  const share = Math.round((route.metrics.greenDurationSeconds / route.liveDurationSeconds) * 100);
  console.log(`  ${route.candidateId}: ${share}% зелени, ${Math.round(route.liveDurationSeconds / 60)} мин, ${(route.distanceMeters / 1000).toFixed(1)} км`);
}
