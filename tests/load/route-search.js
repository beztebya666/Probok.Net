import { sleep } from 'k6';
import { Counter, Rate } from 'k6/metrics';
import { startSearch } from './helpers.js';

const accepted = new Counter('route_searches_accepted');
const rejected = new Rate('route_searches_rejected');

export const options = {
  discardResponseBodies: false,
  scenarios: {
    sustained: {
      executor: 'constant-arrival-rate',
      rate: Number(__ENV.SUSTAINED_RPS || 5),
      timeUnit: '1s',
      duration: __ENV.SUSTAINED_DURATION || '2m',
      preAllocatedVUs: 20,
      maxVUs: 100,
      exec: 'search',
      tags: { load_profile: 'sustained' },
    },
    peak: {
      executor: 'ramping-arrival-rate',
      startTime: __ENV.PEAK_START || '2m10s',
      startRate: 5,
      timeUnit: '1s',
      preAllocatedVUs: 50,
      maxVUs: 250,
      stages: [
        { target: Number(__ENV.PEAK_RPS || 40), duration: '30s' },
        { target: Number(__ENV.PEAK_RPS || 40), duration: __ENV.PEAK_HOLD || '1m' },
        { target: 5, duration: '30s' },
      ],
      exec: 'search',
      tags: { load_profile: 'peak' },
    },
  },
  thresholds: {
    http_req_failed: [{ threshold: 'rate<0.01', abortOnFail: true, delayAbortEval: '30s' }],
    'http_req_duration{endpoint:create-search}': ['p(95)<3000', 'p(99)<8000'],
    route_searches_rejected: ['rate<0.01'],
    dropped_iterations: ['count<10'],
  },
};

export function search() {
  const result = startSearch(__ENV.ROUTING_MODE || 'GREENEST');
  rejected.add(!result.valid);
  if (result.valid) accepted.add(1);
  sleep(Math.random() * 0.2);
}
