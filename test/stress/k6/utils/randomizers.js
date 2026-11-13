// Random date/time generators for chaos testing

// Random date in the future (next 1-30 days)
export function randomFutureDate() {
  const now = new Date();
  const daysAhead = Math.floor(Math.random() * 30) + 1;
  const date = new Date(now);
  date.setDate(date.getDate() + daysAhead);
  date.setHours(Math.floor(Math.random() * 24), 0, 0, 0);
  return date;
}

// Random duration in minutes (5 min to 8 hours)
export function randomDurationMinutes() {
  return Math.floor(Math.random() * 475) + 5; // 5 to 480 minutes
}

// Random tenant ID (1-1000)
export function randomTenantId() {
  return Math.floor(Math.random() * 1000) + 1;
}

// Random resource ID (1-100)
export function randomResourceId() {
  return Math.floor(Math.random() * 100) + 1;
}

// Random requester ID (1-10000)
export function randomRequesterId() {
  return Math.floor(Math.random() * 10000) + 1;
}

// Random booking payload with potentially chaotic values
export function randomChaosPayload() {
  const startsAt = randomFutureDate();
  const durationMs = randomDurationMinutes() * 60 * 1000;
  const endsAt = new Date(startsAt.getTime() + durationMs);

  return {
    resource_id: randomResourceId(),
    requester_id: randomRequesterId(),
    service_id: 1,
    starts_at: startsAt.toISOString(),
    ends_at: endsAt.toISOString(),
    idempotency_key: `chaos-${Date.now()}-${Math.random().toString(36)}`,
  };
}

// Random invalid payload (missing required fields)
export function randomInvalidPayload() {
  const templates = [
    { resource_id: randomResourceId(), requester_id: randomRequesterId() }, // Missing service_id, times
    { starts_at: randomFutureDate().toISOString() }, // Missing most fields
    { resource_id: 'invalid', requester_id: 'invalid' }, // Type mismatch
  ];
  return templates[Math.floor(Math.random() * templates.length)];
}

// Random endpoint from the main APIs
export function randomEndpoint() {
  return ['availability', 'bookings', 'bookings/list'][Math.floor(Math.random() * 3)];
}

// Random HTTP method mix
export function randomMethod() {
  return ['GET', 'POST'][Math.floor(Math.random() * 2)];
}

// DST edge case dates (spring-forward and fall-back in US/Eastern)
export function dstEdgeCaseDate(type = 'spring') {
  // 2027 DST dates (US/Eastern)
  if (type === 'spring') {
    return new Date('2027-03-14T02:30:00-05:00'); // 2:30 AM doesn't exist (spring forward)
  } else {
    return new Date('2027-11-07T01:30:00-05:00'); // 1:30 AM occurs twice (fall back)
  }
}

// Random wait time (simulate user think time)
export function randomThinkTime() {
  return Math.floor(Math.random() * 1000); // 0-1000ms
}
