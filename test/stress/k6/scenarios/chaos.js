import { group, sleep } from 'k6';
import { scenarios, thresholds } from '../config.js';
import * as helpers from '../utils/helpers.js';
import * as randomizers from '../utils/randomizers.js';
import * as assertions from '../utils/assertions.js';

export const options = {
  scenarios: {
    chaos: scenarios.chaos,
  },
  thresholds: thresholds.chaos,
};

export default function () {
  // Random sequence of operations with chaotic payloads

  group('Chaos: Random Valid Bookings', () => {
    const payload = randomizers.randomChaosPayload();
    const res = helpers.createBooking(payload, randomizers.randomTenantId());

    if (res.status === 201) {
      assertions.expectBookingCreated(res);
    } else if (res.status === 409) {
      assertions.expectConflict(res); // Overlap is ok in chaos
    } else {
      assertions.expectNoServerError(res); // But no 500s
    }
  });

  group('Chaos: Random Availability Queries', () => {
    const resourceId = randomizers.randomResourceId();
    const res = helpers.getAvailability(resourceId, randomizers.randomTenantId());
    assertions.expectSuccess(res);
  });

  group('Chaos: Random Invalid Payloads', () => {
    const payload = randomizers.randomInvalidPayload();
    const res = helpers.createBooking(payload);

    assertions.expectBadRequest(res); // Should be 400/422, not 500
  });

  group('Chaos: Concurrent Bookings on Same Resource', () => {
    // Multiple random bookings for the same resource within a small time window
    // Some will conflict (expected 409), some will succeed (expected 201)
    const resourceId = randomizers.randomResourceId();
    const baseTime = randomizers.randomFutureDate();

    for (let i = 0; i < 5; i++) {
      const payload = helpers.buildBookingPayload({
        resource_id: resourceId,
        idempotency_key: `concurrent-${Date.now()}-${i}`,
        // Slightly different times to increase overlap probability
        starts_at: new Date(baseTime.getTime() + i * 60000).toISOString(),
        ends_at: new Date(baseTime.getTime() + (i + 1) * 60000).toISOString(),
      });

      const res = helpers.createBooking(payload);
      if (res.status === 201) {
        assertions.expectBookingCreated(res);
      } else if (res.status === 409) {
        assertions.expectConflict(res);
      } else if (res.status === 429) {
        assertions.expectRateLimited(res);
      }
    }
  });

  group('Chaos: List with Random Filters', () => {
    const res = helpers.listBookings(randomizers.randomResourceId(), randomizers.randomTenantId());
    assertions.expectSuccess(res);
  });

  group('Chaos: Mix of All Operations', () => {
    // Random sequence of different operations
    const operations = [
      () => helpers.getAvailability(),
      () => helpers.createBooking(randomizers.randomChaosPayload()),
      () => helpers.listBookings(),
      () => helpers.createBooking(randomizers.randomInvalidPayload()),
    ];

    const randomOp = operations[Math.floor(Math.random() * operations.length)];
    const res = randomOp();

    assertions.expectNoServerError(res); // No 500s under chaos
  });

  // Small random think time between operations
  sleep(randomizers.randomThinkTime() / 1000);
}
