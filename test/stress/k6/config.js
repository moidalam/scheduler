// Shared configuration for all stress test scenarios
export const API_BASE_URL = __ENV.API_URL || 'http://localhost:8080';
export const DEFAULT_TENANT_ID = 1;
export const TEST_RESOURCE_ID = 100;
export const TEST_REQUESTER_ID = 200;
export const TEST_SERVICE_ID = 1;

// Load profiles
export const loadProfiles = {
  functional: {
    stages: [
      { duration: '1m', target: 200 },    // Ramp to 200 VUs
      { duration: '5m', target: 200 },    // Hold for 5 min
      { duration: '30s', target: 0 },     // Ramp down
    ],
    thresholds: {
      'http_req_duration': ['p(95)<500', 'p(99)<1000'],
      'http_req_failed': ['rate<0.01'], // <1% errors
    },
  },
  chaos: {
    stages: [
      { duration: '30s', target: 200 },
      { duration: '30s', target: 500 },   // Ramp up
      { duration: '2m', target: 500 },    // Hold
      { duration: '30s', target: 800 },   // Spike 1
      { duration: '1m', target: 500 },
      { duration: '30s', target: 1000 },  // Spike 2 (big one)
      { duration: '2m', target: 1000 },   // Hold at max
      { duration: '30s', target: 0 },     // Ramp down
    ],
    thresholds: {
      'http_req_failed{status_code:5xx}': ['rate<0.001'], // <0.1% 500s
    },
  },
  spike: {
    stages: [
      { duration: '30s', target: 100 },
      { duration: '30s', target: 500 },   // Sudden spike
      { duration: '1m', target: 500 },    // Hold spike
      { duration: '30s', target: 0 },     // Ramp down
    ],
  },
};

// Scenario config
export const scenarios = {
  functional: {
    name: 'Functionality Tests',
    executor: 'ramping-vus',
    ...loadProfiles.functional,
  },
  chaos: {
    name: 'Chaos/Monkey Tests',
    executor: 'ramping-vus',
    ...loadProfiles.chaos,
  },
  spike: {
    name: 'Spike Test',
    executor: 'ramping-vus',
    ...loadProfiles.spike,
  },
};
