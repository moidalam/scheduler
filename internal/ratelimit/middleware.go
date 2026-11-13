package ratelimit

import (
	"fmt"
	"net/http"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/moidalam/scheduler/internal/handlers"
)

// Middleware returns an HTTP handler that enforces per-tenant global and
// per-API rate limits, recording all checks as metrics.
// Must be applied after the TenantMiddleware so the tenant_id is in the context.
func Middleware(limiter *Limiter) func(http.Handler) http.Handler {
	meter := otel.Meter("scheduler/ratelimit")
	checksCounter, _ := meter.Int64Counter(
		"rate_limit_checks_total",
		metric.WithDescription("Total rate limit checks"),
		metric.WithUnit("1"),
	)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			tenantID := handlers.TenantFrom(ctx)
			apiName := getAPIName(r)

			// Check rate limits
			allowed, _, err := limiter.AllowRequest(ctx, tenantID, apiName)

			// Record metric
			attrs := []attribute.KeyValue{
				attribute.String("scope", apiName),
				attribute.Bool("allowed", allowed),
			}
			if err != nil {
				attrs = append(attrs, attribute.String("error", err.Error()))
			}
			checksCounter.Add(ctx, 1, metric.WithAttributes(attrs...))

			if !allowed {
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// getAPIName returns a normalized API endpoint name from the request for rate limit scoping.
// Examples: "get_resources_availability", "post_bookings", "get_resources_bookings"
func getAPIName(r *http.Request) string {
	method := strings.ToLower(r.Method)
	path := r.URL.Path

	// Normalize path: /v1/resources/{resource_id}/availability -> get_resources_availability
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 2 {
		// Skip version and tenants/{id} prefixes
		for _, part := range parts {
			if part != "v1" && part != "tenants" && !strings.HasPrefix(part, "{") && !strings.HasSuffix(part, "}") {
				return fmt.Sprintf("%s_%s", method, part)
			}
		}
	}

	// Fallback
	return fmt.Sprintf("%s_%s", method, strings.ReplaceAll(path, "/", "_"))
}
