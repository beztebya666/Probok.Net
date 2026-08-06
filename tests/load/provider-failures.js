import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';
import { baseUrl, getSearch, startSearch } from './helpers.js';

http.setResponseCallback(http.expectedStatuses({ min: 200, max: 299 }, 429, 503, 504));

const degraded = new Rate('degraded_results');
const unbounded = new Rate('unbounded_searches');
const completion = new Trend('search_completion_ms', true);
const completed = new Counter('searches_observed_terminal');
const handled = new Counter('handled_searches');

export const options = {
  scenarios: {
    failure_pressure: {
      executor: 'constant-arrival-rate',
      rate: Number(__ENV.FAILURE_RPS || 10),
      timeUnit: '1s',
      duration: __ENV.FAILURE_DURATION || '2m',
      preAllocatedVUs: 50,
      maxVUs: 200,
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.10'],
    unbounded_searches: ['rate==0'],
    search_completion_ms: ['p(99)<15000'],
    handled_searches: ['count>0'],
  },
};

export function setup() {
  const scenario = __ENV.EXPECTED_FAILURE_MODE || 'degraded';
  if (!['slow', 'rate_limit', 'outage', 'degraded'].includes(scenario)) {
    throw new Error(`unsupported EXPECTED_FAILURE_MODE: ${scenario}`);
  }
  return { scenario };
}

export default function (data) {
  const startedAt = Date.now();
  const result = startSearch('GREENEST', { failure_mode: data.scenario });
  if (!result.valid) {
    const normalized = check(result.response, { 'failure is normalized, not raw 500': (r) => r.status !== 500 });
    if (normalized) handled.add(1);
    return;
  }

  let terminal = result.body.status !== 'PENDING' && result.body.status !== 'RUNNING';
  let body = result.body;
  for (let i = 0; i < 12 && !terminal; i += 1) {
    sleep(0.5);
    const response = getSearch(body.searchId, { failure_mode: data.scenario });
    if (response.status === 200) {
      try { body = response.json(); } catch (_) { body = {}; }
      terminal = ['COMPLETED', 'DEGRADED', 'FAILED', 'CANCELLED'].includes(body.status);
    }
  }
  const elapsed = Date.now() - startedAt;
  completion.add(elapsed);
  unbounded.add(!terminal || elapsed > 15000);
  degraded.add(body.status === 'DEGRADED' || body.status === 'FAILED');
  if (terminal) {
    completed.add(1);
    handled.add(1);
  }
}

export function teardown() {
  if (!__ENV.PROMETHEUS_URL) return;
  const prom = __ENV.PROMETHEUS_URL.replace(/\/$/, '');
  const provider = http.get(`${prom}/api/v1/query?query=${encodeURIComponent('sum(rate(provider_requests_total[2m]))')}`, { timeout: '3s' });
  const searches = http.get(`${prom}/api/v1/query?query=${encodeURIComponent('sum(rate(route_search_total[2m]))')}`, { timeout: '3s' });
  check(provider, { 'provider metrics query succeeds': (r) => r.status === 200 });
  check(searches, { 'search metrics query succeeds': (r) => r.status === 200 });
  if (provider.status === 200 && searches.status === 200) {
    const providerRate = Number(provider.json('data.result.0.value.1') || 0);
    const searchRate = Number(searches.json('data.result.0.value.1') || 0);
    const budget = Number(__ENV.MAX_PROVIDER_REQUESTS || 8);
    if (searchRate > 0 && providerRate > searchRate * budget * 1.05) {
      throw new Error(`retry storm detected: provider rate ${providerRate} exceeds search rate ${searchRate} x budget ${budget}`);
    }
  }
}
