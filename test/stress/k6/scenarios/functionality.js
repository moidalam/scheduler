import { group } from 'k6';
import { scenarios } from '../config.js';
import * as helpers from '../utils/helpers.js';
import * as assertions from '../utils/assertions.js';

export const options = {
  scenarios: {
    functional: scenarios.functional,
  },
};

export default function () {
  group('Functionality: Availability', () => {
    const res = helpers.getAvailability();
    assertions.expectAvailability(res);
  });

  group('Functionality: Create Booking', () => {
    const payload = helpers.buildBookingPayload();
    const res = helpers.createBooking(payload);
    const booking = assertions.expectBookingCreated(res);

    if (booking) {
      group('Functionality: Get Booking', () => {
        const getRes = helpers.getBooking(booking.id);
        assertions.expectSuccess(getRes);
      });
    }
  });

  group('Functionality: Idempotency', () => {
    const payload = helpers.buildBookingPayload({
      idempotency_key: `idempotent-${Date.now()}`,
    });

    // First request
    const res1 = helpers.createBooking(payload);
    const booking1 = assertions.expectBookingCreated(res1);

    if (booking1) {
      // Second request with same idempotency key
      const res2 = helpers.createBooking(payload);
      assertions.expectSuccess(res2, 200); // Should return 200, not 201
      const booking2 = JSON.parse(res2.body);

      assertions.expectIdempotentBooking(booking1, booking2);
    }
  });

  group('Functionality: Tenant Isolation', () => {
    const payload = helpers.buildBookingPayload();

    // Tenant 1 books a slot
    const res1 = helpers.createBooking(payload, 1);
    const booking1 = assertions.expectBookingCreated(res1);

    if (booking1) {
      // Tenant 2 tries to book the exact same time (should succeed in different tenant)
      const payload2 = {
        ...payload,
        idempotency_key: `tenant2-${Date.now()}`,
      };
      const res2 = helpers.createBooking(payload2, 2);
      const booking2 = assertions.expectBookingCreated(res2);

      assertions.expectTenantIsolation(booking1, booking2);
    }
  });

  group('Functionality: List Bookings', () => {
    const res = helpers.listBookings();
    assertions.expectSuccess(res);
  });
}
