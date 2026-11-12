package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/moidalam/scheduler/internal/gen/api"
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
}

// NewRouter builds the HTTP handler: health at /healthz, the generated API
// under /v1 behind the tenant middleware.
func NewRouter(d RouterDeps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(requestLogger(d.Log))
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
		r.Use(TenantMiddleware(d.Tenants, d.DefaultTenant))
		api.HandlerWithOptions(strict, api.ChiServerOptions{
			BaseRouter:       r,
			ErrorHandlerFunc: requestErrorHandler,
		})
	})
	return r
}

// requestLogger writes one structured line per request, carrying the request id
// so a log line can be tied to a response.
func requestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			log.InfoContext(r.Context(), "request",
				slog.String("request_id", middleware.GetReqID(r.Context())),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", ww.Status()),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()),
			)
		})
	}
}
