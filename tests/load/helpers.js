import http from 'k6/http';
import { check } from 'k6';

export const baseUrl = (__ENV.BASE_URL || 'http://localhost:8080').replace(/\/$/, '');

export function searchPayload(mode = 'GREENEST') {
  const jitter = (__VU % 50) * 0.0001;
  return JSON.stringify({
    origin: { latitude: 55.751244 + jitter, longitude: 37.618423 + jitter },
    destination: { latitude: 55.796127 - jitter, longitude: 37.537922 - jitter },
    waypoints: [],
    routingMode: mode,
    maxExtraDistanceMeters: 30000,
    maxExtraDistancePercent: 50,
    maxExtraTimeSeconds: 1200,
    avoidTolls: false,
    avoidUnpaved: false,
    strictness: 0.8,
    maxProviderRequests: Number(__ENV.MAX_PROVIDER_REQUESTS || 8),
    searchDeadlineMs: Number(__ENV.SEARCH_DEADLINE_MS || 10000),
  });
}

export function requestHeaders() {
  return {
    'Content-Type': 'application/json',
    Accept: 'application/json',
    'Idempotency-Key': `k6-${__VU}-${__ITER}-${Date.now()}`,
  };
}

export function startSearch(mode = 'GREENEST', tags = {}) {
  const response = http.post(`${baseUrl}/api/v1/route-searches`, searchPayload(mode), {
    headers: requestHeaders(),
    tags: { endpoint: 'create-search', ...tags },
    timeout: `${Number(__ENV.SEARCH_DEADLINE_MS || 10000) + 2000}ms`,
  });
  let body = {};
  try { body = response.json(); } catch (_) { body = {}; }
  const valid = check(response, {
    'create returns 200 or 202': (r) => r.status === 200 || r.status === 202,
    'response has search id': () => typeof body.searchId === 'string' && body.searchId.length > 0,
    'response has status': () => typeof body.status === 'string' && body.status.length > 0,
  });
  return { response, body, valid };
}

export function getSearch(searchId, tags = {}) {
  return http.get(`${baseUrl}/api/v1/route-searches/${encodeURIComponent(searchId)}`, {
    headers: { Accept: 'application/json' },
    tags: { endpoint: 'get-search', ...tags },
    timeout: '3s',
  });
}
