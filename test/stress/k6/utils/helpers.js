import http from 'k6/http';
import { API_BASE_URL, DEFAULT_TENANT_ID, TEST_RESOURCE_ID, TEST_SERVICE_ID } from '../config.js';

// HTTP helper with tenant header
export function httpGet(url, tenantId = DEFAULT_TENANT_ID) {
  return http.get(url, {
    headers: { 'X-Tenant-ID': String(tenantId) },
    tags: { name: 'GET' },
  });
}

export function httpPost(url, payload, tenantId = DEFAULT_TENANT_ID) {
  return http.post(url, JSON.stringify(payload), {
    headers: {
      'X-Tenant-ID': String(tenantId),
      'Content-Type': 'application/json',
    },
    tags: { name: 'POST' },
  });
}

// API endpoints
export function availabilityEndpoint(resourceId = TEST_RESOURCE_ID) {
  return `${API_BASE_URL}/v1/resources/${resourceId}/availability`;
}

export function bookingsEndpoint() {
  return `${API_BASE_URL}/v1/bookings`;
}

export function bookingEndpoint(bookingId) {
  return `${API_BASE_URL}/v1/bookings/${bookingId}`;
}

// Booking payload builder
export function buildBookingPayload(overrides = {}) {
  const now = new Date();
  const startsAt = new Date(now.getTime() + 3600000); // 1 hour from now
  const endsAt = new Date(startsAt.getTime() + 1800000); // 30 min duration

  return {
    resource_id: TEST_RESOURCE_ID,
    requester_id: 200 + Math.floor(Math.random() * 100),
    service_id: TEST_SERVICE_ID,
    starts_at: startsAt.toISOString(),
    ends_at: endsAt.toISOString(),
    idempotency_key: `stress-${Date.now()}-${Math.random().toString(36)}`,
    ...overrides,
  };
}

// Get availability for a date range
export function getAvailability(resourceId = TEST_RESOURCE_ID, tenantId = DEFAULT_TENANT_ID) {
  const from = new Date();
  from.setDate(from.getDate() + 1); // Tomorrow
  const to = new Date(from);
  to.setDate(to.getDate() + 7); // Next 7 days

  const url = `${availabilityEndpoint(resourceId)}?from=${from.toISOString()}&to=${to.toISOString()}`;
  return httpGet(url, tenantId);
}

// Create a booking
export function createBooking(payload = null, tenantId = DEFAULT_TENANT_ID) {
  return httpPost(bookingsEndpoint(), payload || buildBookingPayload(), tenantId);
}

// Get a booking
export function getBooking(bookingId, tenantId = DEFAULT_TENANT_ID) {
  return httpGet(bookingEndpoint(bookingId), tenantId);
}

// List bookings for a resource
export function listBookings(resourceId = TEST_RESOURCE_ID, tenantId = DEFAULT_TENANT_ID) {
  const url = `${API_BASE_URL}/v1/bookings?resource_id=${resourceId}`;
  return httpGet(url, tenantId);
}
