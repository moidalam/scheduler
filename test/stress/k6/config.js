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
      { duration: '30s', target: 50 },   // Ramp to 50 VUs
      { duration: '1m', target: 50 },    // Hold for 1 min
      { duration: '10s', target: 0 },    // Ramp down
    ],
    thresholds: {
      'http_req_duration': ['p(95)<500', 'p(99)<1000'],
      'http_req_failed': ['rate<0.01'], // <1% errors
    },
  },
  chaos: {
    stages: [
      { duration: '20s', target: 100 },
      { duration: '10s', target: 300 },  // Spike
      { duration: '20s', target: 100 },
      { duration: '10s', target: 500 },  // Another spike
      { duration: '20s', target: 100 },
      { duration: '10s', target: 0 },
    ],
    thresholds: {
      'http_req_failed{status_code:5xx}': ['rate<0.001'], // <0.1% 500s
    },
  },
  spike: {
    stages: [
      { duration: '10s', target: 50 },
      { duration: '10s', target: 200 },  // Sudden spike
      { duration: '10s', target: 50 },
      { duration: '10s', target: 0 },
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
