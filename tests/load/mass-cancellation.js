import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate } from 'k6/metrics';
import { baseUrl, startSearch } from './helpers.js';

const cancellationFailures = new Rate('cancellation_failures');

export const options = {
  scenarios: {
    cancellation_wave: {
      executor: 'ramping-vus',
      stages: [
        { duration: '20s', target: 100 },
        { duration: '1m', target: 100 },
        { duration: '20s', target: 0 },
      ],
    },
  },
  thresholds: {
    cancellation_failures: ['rate<0.01'],
    'http_req_duration{endpoint:cancel-search}': ['p(95)<1000'],
  },
};

export default function () {
  const result = startSearch('GREENEST');
  if (!result.valid) {
    cancellationFailures.add(true);
    return;
  }
  sleep(Math.random() * 0.2);
  const response = http.del(`${baseUrl}/api/v1/route-searches/${encodeURIComponent(result.body.searchId)}`, null, {
    headers: { Accept: 'application/json' },
    tags: { endpoint: 'cancel-search' },
    timeout: '2s',
  });
  const okay = check(response, {
    'cancel returns 204': (r) => r.status === 204,
    'cancel has no body': (r) => !r.body,
  });
  cancellationFailures.add(!okay);
}
