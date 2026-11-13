// Package ratelimit provides Redis-based sliding window rate limiting per tenant and API.
package ratelimit

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Limiter checks rate limits using Redis sliding window algorithm.
type Limiter struct {
	redis *redis.Client
	log   *slog.Logger

	mu           sync.RWMutex
	configCache  map[string]int // key: "tenant_id:scope", value: requests_per_second
	cacheExpiry  time.Time
	cacheTTL     time.Duration
	configLoader ConfigLoader
}

// ConfigLoader loads rate limit configurations from storage.
type ConfigLoader interface {
	LoadRateLimitConfig(ctx context.Context, tenantID int64, scope string) (requestsPerSecond int, err error)
}

// New creates a rate limiter.
func New(redis *redis.Client, loader ConfigLoader, log *slog.Logger) *Limiter {
	return &Limiter{
		redis:        redis,
		log:          log,
		configCache:  make(map[string]int),
		cacheTTL:     30 * time.Second, // reload config every 30s
		configLoader: loader,
	}
}

// AllowRequest checks if a request is allowed under rate limits.
// Returns (allowed, requestsRemaining, error).
// On Redis error, returns (true, 0, nil) for fail-open behavior.
func (l *Limiter) AllowRequest(ctx context.Context, tenantID int64, scope string) (allowed bool, remaining int, err error) {
	// Check tenant-level global limit
	globalAllowed, globalRemaining, err := l.checkLimit(ctx, tenantID, "global")
	if err != nil {
		l.log.Warn("rate limit check failed for global scope", "tenant_id", tenantID, "err", err)
		return true, 0, nil // fail-open
	}
	if !globalAllowed {
		return false, 0, nil
	}

	// Check API-level limit if scope is not global
	if scope != "global" {
		apiAllowed, apiRemaining, err := l.checkLimit(ctx, tenantID, scope)
		if err != nil {
			l.log.Warn("rate limit check failed for api scope", "tenant_id", tenantID, "scope", scope, "err", err)
			return true, 0, nil // fail-open
		}
		if !apiAllowed {
			return false, 0, nil
		}
		remaining = apiRemaining
	} else {
		remaining = globalRemaining
	}

	return true, remaining, nil
}

// checkLimit checks a single rate limit scope (global or API endpoint).
func (l *Limiter) checkLimit(ctx context.Context, tenantID int64, scope string) (allowed bool, remaining int, err error) {
	rps, err := l.getConfig(ctx, tenantID, scope)
	if err != nil {
		return false, 0, fmt.Errorf("get rate limit config: %w", err)
	}

	key := fmt.Sprintf("scheduler:ratelimit:tenant:%d:%s", tenantID, scope)
	now := time.Now().UnixMilli()
	windowSize := 1000 // milliseconds (1 second)
	windowStart := now - int64(windowSize)

	// Use Lua script for atomic sliding window check
	allowed, newCount, err := l.evaluateLuaScript(ctx, key, now, windowStart, rps)
	if err != nil {
		return false, 0, fmt.Errorf("evaluate rate limit script: %w", err)
	}

	remaining = rps - newCount
	if remaining < 0 {
		remaining = 0
	}

	return allowed, remaining, nil
}

// evaluateLuaScript runs the sliding window Lua script atomically in Redis.
func (l *Limiter) evaluateLuaScript(ctx context.Context, key string, now, windowStart int64, maxRequests int) (allowed bool, count int, err error) {
	script := redis.NewScript(`
		local key = KEYS[1]
		local now = tonumber(ARGV[1])
		local window_start = tonumber(ARGV[2])
		local max_requests = tonumber(ARGV[3])

		redis.call('ZREMRANGEBYSCORE', key, '-inf', window_start)
		local count = redis.call('ZCARD', key)

		if count < max_requests then
			redis.call('ZADD', key, now, now .. '-' .. math.random())
			redis.call('EXPIRE', key, 2)
			return {1, count + 1}
		else
			return {0, count}
		end
	`)

	result, err := script.Run(ctx, l.redis, []string{key}, now, windowStart, maxRequests).Result()
	if err != nil {
		return false, 0, err
	}

	vals, ok := result.([]interface{})
	if !ok || len(vals) < 2 {
		return false, 0, fmt.Errorf("unexpected Lua script result: %v", result)
	}

	allowed = vals[0].(int64) == 1
	count = int(vals[1].(int64))
	return allowed, count, nil
}

// getConfig fetches rate limit config, using cache with TTL.
func (l *Limiter) getConfig(ctx context.Context, tenantID int64, scope string) (int, error) {
	l.mu.RLock()
	if time.Now().Before(l.cacheExpiry) {
		key := fmt.Sprintf("%d:%s", tenantID, scope)
		if rps, ok := l.configCache[key]; ok {
			l.mu.RUnlock()
			return rps, nil
		}
	}
	l.mu.RUnlock()

	// Load from storage
	rps, err := l.configLoader.LoadRateLimitConfig(ctx, tenantID, scope)
	if err != nil {
		return 0, fmt.Errorf("load rate limit config: %w", err)
	}

	// Apply defaults if not configured
	if rps == 0 {
		if scope == "global" {
			rps = 1000 // default: 1000 req/sec per tenant
		} else {
			rps = 100 // default: 100 req/sec per API per tenant
		}
	}

	// Update cache
	l.mu.Lock()
	l.configCache[fmt.Sprintf("%d:%s", tenantID, scope)] = rps
	l.cacheExpiry = time.Now().Add(l.cacheTTL)
	l.mu.Unlock()

	return rps, nil
}

// ClearCache clears the config cache (useful for testing or admin operations).
func (l *Limiter) ClearCache() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.configCache = make(map[string]int)
	l.cacheExpiry = time.Time{}
}
