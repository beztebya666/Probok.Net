import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate } from 'k6/metrics';
import { baseUrl, startSearch } from './helpers.js';

const reconnectFailures = new Rate('sse_reconnect_failures');

export const options = {
  scenarios: {
    reconnects: {
      executor: 'per-vu-iterations',
      vus: Number(__ENV.SSE_VUS || 20),
      iterations: Number(__ENV.SSE_ITERATIONS || 5),
      maxDuration: '5m',
    },
  },
  thresholds: {
    sse_reconnect_failures: ['rate<0.02'],
    'http_req_duration{endpoint:sse}': ['p(95)<12000'],
  },
};

export default function () {
  const result = startSearch('GREENEST');
  if (!result.valid) {
    reconnectFailures.add(true);
    return;
  }
  const url = `${baseUrl}/api/v1/route-searches/${encodeURIComponent(result.body.searchId)}/events`;
  const first = http.get(url, {
    headers: { Accept: 'text/event-stream' },
    timeout: '12s',
    tags: { endpoint: 'sse', connection: 'initial' },
  });
  const firstOkay = check(first, {
    'SSE content type': (r) => (r.headers['Content-Type'] || '').includes('text/event-stream'),
    'SSE emits event framing': (r) => r.body.includes('event:') || r.body.includes('data:'),
  });
  const ids = [...first.body.matchAll(/^id:\s*(.+)$/gm)];
  const lastEventId = ids.length ? ids[ids.length - 1][1].trim() : '0';
  sleep(0.1);
  const second = http.get(url, {
    headers: { Accept: 'text/event-stream', 'Last-Event-ID': lastEventId },
    timeout: '12s',
    tags: { endpoint: 'sse', connection: 'reconnect' },
  });
  const secondOkay = check(second, {
    'SSE reconnect accepted': (r) => r.status === 200 || r.status === 204,
    'SSE reconnect is not server error': (r) => r.status < 500,
  });
  reconnectFailures.add(!(firstOkay && secondOkay));
}
