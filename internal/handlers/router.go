package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/moidalam/scheduler/internal/gen/api"
	"github.com/moidalam/scheduler/internal/observability"
)

// Pinger reports whether the database is reachable.
type Pinger interface {
	Ping(ctx context.Context) error
}

// RouterDeps are the collaborators NewRouter wires together.
type RouterDeps struct {
	Handlers      *Handlers
	Tenants       TenantLookup
	DB            Pinger
	DefaultTenant int64
	Log           *slog.Logger
	// Metrics records one operation per request at the edge.
	Metrics *observability.OpTracker
}

// NewRouter builds the HTTP handler: health at /healthz, the generated API
// under /v1 behind the tenant middleware.
func NewRouter(d RouterDeps) http.Handler {
	r := chi.NewRouter()
	r.Use(observeRequests(d.Log, d.Metrics))
	r.Use(middleware.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, req *http.Request) {
		ctx, cancel := context.WithTimeout(req.Context(), 2*time.Second)
		defer cancel()
		if err := d.DB.Ping(ctx); err != nil {
			d.Log.ErrorContext(req.Context(), "health check failed", slog.Any("error", err))
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})

	strict := api.NewStrictHandlerWithOptions(d.Handlers, nil, api.StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  requestErrorHandler,
		ResponseErrorHandlerFunc: responseErrorHandler(d.Log),
	})
	r.Group(func(r chi.Router) {
		r.Use(TenantMiddleware(d.Tenants, d.DefaultTenant, d.Log))
		api.HandlerWithOptions(strict, api.ChiServerOptions{
			BaseRouter:       r,
			ErrorHandlerFunc: requestErrorHandler,
		})
	})
	// The outermost layer opens the request's trace span, so the edge and every
	// layer below it log and measure inside that one trace. It also accepts an
	// incoming traceparent header from the caller.
	return otelhttp.NewHandler(r, "scheduler")
}
