# Stress Testing Suite (k6)

Hybrid stress test suite combining functionality verification + chaos testing.

## Prerequisites

1. **k6 installed**: https://k6.io/docs/getting-started/installation/
2. **Stack running**:
   ```bash
   make dev
   ```
   This starts Postgres, Redis, API, and OTEL stack (Grafana, Prometheus, Loki, Tempo).

3. **API healthy**:
   ```bash
   curl http://localhost:8080/healthz
   ```

## Scenarios

### Functionality Tests
Verify correctness under load.

```bash
make functional
```

**What it tests:**
- Availability calculation (slot generation)
- Booking creation (happy path)
- Idempotency (same key → same result)
- Tenant isolation (different tenants see different bookings)
- List bookings

**Expected:** All requests succeed, p95 latency < 500ms

---

### Chaos/Monkey Tests
Random sequences, edge cases, invalid payloads.

```bash
make chaos
```

**What it tests:**
- Random future dates (DST boundaries, leap years)
- Random durations (5 min to 8 hours)
- Invalid payloads (missing fields, type mismatches)
- Concurrent operations on same resource
- Mixed GET/POST sequences
- Error handling (4xx, not 5xx)

**Expected:** No 500 errors, graceful handling of conflicts/invalid input

---

### Concurrent Contention
25 VUs simultaneously booking the same time slot.

```bash
make contention
```

**What it tests:**
- Exclusion constraint prevents overlaps
- Advisory lock prevents deadlocks
- Clean conflict responses (409, not 500)

**Expected:** Exactly 1 success (201), 24 conflicts (409), zero deadlocks

---

### Full Suite
Run all scenarios sequentially.

```bash
make full
```

## Monitoring Live

While tests run, open Grafana and watch:

```
http://localhost:3000
```

**Dashboards to watch:**
- **API Layer**: Request latency, error rate, throughput
- **Rate Limiter**: Requests rejected, per-tenant limits
- **Database**: Query latency, connection pool usage
- **Traces**: End-to-end request flow

## Expected Results

### Functionality Suite
```
✓ Status is success: 90%+
✓ Idempotency verified: 100%
✓ Tenant isolation verified: 100%
✓ Availability queries succeed: 100%
✓ p95 latency: <500ms
```

### Chaos Suite
```
✓ No 5xx errors: 100%
✓ Bad requests handled (4xx): 100%
✓ Conflicts handled (409): 100%
✓ Chaotic payloads processed: 100%
```

### Contention Suite (25 VUs)
```
✓ 1 booking succeeded (201)
✓ 24 bookings conflicted (409)
✓ 0 deadlocks (all requests finished)
✓ 0 500 errors
```

## Custom Options

Override API endpoint:
```bash
make functional API_URL=http://staging-api:8080
```

## Troubleshooting

**"Connection refused":**
- Ensure `make dev` is running
- Check `curl http://localhost:8080/healthz`

**"Rate limit errors (429)":**
- Expected in chaos tests
- Verify limits configured in `docker-compose.yml`: `REDIS_URL=redis://redis:6379`

**High latency:**
- Check Grafana CPU, DB connection pool
- Look at trace latency breakdown in Tempo (http://localhost:3200)

**Strange failures:**
- Check logs: `docker compose logs app | grep error`
- Verify Redis connectivity: `docker compose logs redis`

## Files

```
scenarios/
  ├── functionality.js    # Correctness tests
  ├── chaos.js            # Random/chaos tests
  └── contention.js       # 25-VU concurrent booking stress
utils/
  ├── helpers.js          # HTTP wrappers, API endpoints
  ├── assertions.js       # Verification checks
  └── randomizers.js      # Chaos payload generators
config.js                 # Shared configuration
Makefile                  # Test runners
```

## Integration with BVTs

These stress tests complement the BVT suite (`test/bvt/`). Use together:
1. **BVTs** (test/bvt/): correctness, observability integration
2. **Stress tests** (test/stress/k6): load, concurrency, chaos

Run BVTs:
```bash
make bvt
```

Run stress tests:
```bash
make -C test/stress/k6 full
```
