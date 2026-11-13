package ratelimit

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// mockConfigLoader returns fixed rate limits for testing.
type mockConfigLoader struct {
	tenantLimits map[string]int // key: "tenant_id:scope"
}

func (m *mockConfigLoader) LoadRateLimitConfig(ctx context.Context, tenantID int64, scope string) (int, error) {
	key := stringOf(tenantID) + ":" + scope
	if rps, ok := m.tenantLimits[key]; ok {
		return rps, nil
	}
	return 0, nil // use defaults
}

func TestLimiterSlidingWindow(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test, requires Redis")
	}

	ctx := context.Background()
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer client.Close()

	// Flush redis
	client.FlushDB(ctx)

	loader := &mockConfigLoader{
		tenantLimits: map[string]int{
			"1:global":       3, // 3 requests per second for tenant 1 global
			"1:get_bookings": 2, // 2 requests per second for get_bookings
		},
	}

	limiter := New(client, loader, slog.Default())

	t.Run("allows requests within limit", func(t *testing.T) {
		client.FlushDB(ctx)

		// First 3 requests should be allowed (global limit is 3)
		for i := 0; i < 3; i++ {
			allowed, _, err := limiter.AllowRequest(ctx, 1, "global")
			require.NoError(t, err)
			require.True(t, allowed, "request %d should be allowed", i+1)
		}

		// 4th request should be rejected
		allowed, _, err := limiter.AllowRequest(ctx, 1, "global")
		require.NoError(t, err)
		require.False(t, allowed, "request 4 should be rejected")
	})

	t.Run("respects per-api limits", func(t *testing.T) {
		client.FlushDB(ctx)

		// get_bookings has a limit of 2 per second
		for i := 0; i < 2; i++ {
			allowed, _, err := limiter.AllowRequest(ctx, 1, "get_bookings")
			require.NoError(t, err)
			require.True(t, allowed, "request %d should be allowed", i+1)
		}

		// 3rd request should be rejected
		allowed, _, err := limiter.AllowRequest(ctx, 1, "get_bookings")
		require.NoError(t, err)
		require.False(t, allowed, "request 3 should be rejected")
	})

	t.Run("rate limits are tenant-scoped", func(t *testing.T) {
		client.FlushDB(ctx)

		// Tenant 1 should be limited
		for i := 0; i < 3; i++ {
			allowed, _, err := limiter.AllowRequest(ctx, 1, "global")
			require.NoError(t, err)
			require.True(t, allowed)
		}

		allowed, _, err := limiter.AllowRequest(ctx, 1, "global")
		require.NoError(t, err)
		require.False(t, allowed)

		// Tenant 2 should have its own limit (using defaults)
		allowed, _, err = limiter.AllowRequest(ctx, 2, "global")
		require.NoError(t, err)
		require.True(t, allowed, "tenant 2 should be allowed despite tenant 1 being limited")
	})

	t.Run("resets after window", func(t *testing.T) {
		client.FlushDB(ctx)

		// Use up the limit
		for i := 0; i < 3; i++ {
			allowed, _, err := limiter.AllowRequest(ctx, 1, "global")
			require.NoError(t, err)
			require.True(t, allowed)
		}

		// Should be rejected
		allowed, _, err := limiter.AllowRequest(ctx, 1, "global")
		require.NoError(t, err)
		require.False(t, allowed)

		// Wait for window to expire (1.1 seconds)
		time.Sleep(1100 * time.Millisecond)

		// Should be allowed again
		allowed, _, err = limiter.AllowRequest(ctx, 1, "global")
		require.NoError(t, err)
		require.True(t, allowed)
	})

	t.Run("config cache works", func(t *testing.T) {
		// Update cache with new limit
		loader.tenantLimits["1:custom"] = 1

		client.FlushDB(ctx)

		// First request allowed
		allowed, _, err := limiter.AllowRequest(ctx, 1, "custom")
		require.NoError(t, err)
		require.True(t, allowed)

		// Second should be rejected
		allowed, _, err = limiter.AllowRequest(ctx, 1, "custom")
		require.NoError(t, err)
		require.False(t, allowed)
	})
}

func stringOf(id int64) string {
	return fmt.Sprintf("%d", id)
}
