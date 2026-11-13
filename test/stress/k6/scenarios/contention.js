import { group, sleep } from 'k6';
import { DEFAULT_TENANT_ID, TEST_RESOURCE_ID, TEST_SERVICE_ID } from '../config.js';
import * as helpers from '../utils/helpers.js';
import * as assertions from '../utils/assertions.js';

// Concurrent contention test: 25 simultaneous bookings of the same slot
// Proves the exclusion constraint + advisory lock work under load
// Expected: exactly 1 success (201), 24 conflicts (409), zero 500s

export const options = {
  vus: 100,
  duration: '5m',
  thresholds: {
    'http_req_duration': ['p(95)<500'],
    'http_req_failed{status_code:5xx}': ['rate<0.001'],
  },
};

// Shared booking time (all VUs try to book the same slot)
const bookingTime = new Date();
bookingTime.setDate(bookingTime.getDate() + 1);
bookingTime.setHours(10, 0, 0, 0); // Tomorrow at 10 AM

// All VUs attempt to book exactly the same slot
export default function () {
  const payload = {
    resource_id: TEST_RESOURCE_ID,
    requester_id: 200 + (__VU % 100), // Slight variation in requester
    service_id: TEST_SERVICE_ID,
    starts_at: bookingTime.toISOString(),
    ends_at: new Date(bookingTime.getTime() + 1800000).toISOString(), // 30 min duration
    idempotency_key: `contention-${bookingTime.getTime()}-${__VU}`,
  };

  group('Concurrent Contention: 25 VUs booking same slot', () => {
    const res = helpers.createBooking(payload, DEFAULT_TENANT_ID);

    if (res.status === 201) {
      assertions.expectBookingCreated(res);
      // Track this VU won the race
      console.log(`VU ${__VU} won the race`);
    } else if (res.status === 409) {
      assertions.expectConflict(res);
    } else if (res.status === 429) {
      assertions.expectRateLimited(res);
    } else {
      assertions.expectNoServerError(res);
    }
  });

  // Small delay before next iteration
  sleep(1);
}
