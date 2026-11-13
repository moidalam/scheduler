import { check, fail } from 'k6';

// Verify HTTP response is success
export function expectSuccess(res, expectedStatus = 200) {
  return check(res, {
    'status is success': (r) => r.status === expectedStatus,
    'has body': (r) => r.body && r.body.length > 0,
  });
}

// Verify availability response
export function expectAvailability(res) {
  expectSuccess(res);
  return check(res, {
    'has slots array': (r) => {
      try {
        const body = JSON.parse(r.body);
        return Array.isArray(body.slots);
      } catch {
        return false;
      }
    },
  });
}

// Verify booking created successfully
export function expectBookingCreated(res) {
  check(res, {
    'status is 201': (r) => r.status === 201,
    'has booking id': (r) => {
      try {
        const body = JSON.parse(r.body);
        return body.id && body.id > 0;
      } catch {
        return false;
      }
    },
  });
  return res.status === 201 ? JSON.parse(res.body) : null;
}

// Verify booking conflict (expected 409)
export function expectConflict(res) {
  return check(res, {
    'status is 409': (r) => r.status === 409,
  });
}

// Verify rate limit rejection (expected 429)
export function expectRateLimited(res) {
  return check(res, {
    'status is 429': (r) => r.status === 429,
  });
}

// Verify bad request (expected 400 or 422)
export function expectBadRequest(res) {
  return check(res, {
    'status is 4xx': (r) => r.status >= 400 && r.status < 500,
  });
}

// Verify no 500 errors
export function expectNoServerError(res) {
  if (res.status >= 500) {
    fail(`Unexpected 5xx error: ${res.status} ${res.body}`);
  }
  return true;
}

// Verify idempotent booking returns same result
export function expectIdempotentBooking(booking1, booking2) {
  check({ booking1, booking2 }, {
    'ids match': (b) => b.booking1.id === b.booking2.id,
    'starts_at match': (b) => b.booking1.starts_at === b.booking2.starts_at,
    'ends_at match': (b) => b.booking1.ends_at === b.booking2.ends_at,
  });
}

// Verify tenant isolation (same resource, different tenants)
export function expectTenantIsolation(bookingFromTenant1, bookingFromTenant2) {
  // Both bookings for same time should succeed because they're in different tenants
  return check({ b1: bookingFromTenant1, b2: bookingFromTenant2 }, {
    'both bookings created': (b) => b.b1 && b.b1.id > 0 && b.b2 && b.b2.id > 0,
  });
}
