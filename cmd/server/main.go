// Command server runs the scheduling API.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/moidalam/scheduler/internal/config"
	"github.com/moidalam/scheduler/internal/controllers"
	"github.com/moidalam/scheduler/internal/db"
	"github.com/moidalam/scheduler/internal/handlers"
	"github.com/moidalam/scheduler/internal/observability"
	"github.com/moidalam/scheduler/internal/ratelimit"
)

// Stamped by the Dockerfile via -ldflags "-X main.version=<commit sha>".
var version = "dev"

// shutdownTimeout bounds how long in-flight requests get to finish.
const shutdownTimeout = 25 * time.Second

func main() {
	if err := run(); err != nil {
		slog.Error("server exited", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Telemetry is set up first, so every instrument and tracer below binds to
	// the real providers. With no OTLP endpoint, logs stay on stdout.
	logger, shutdownTelemetry, err := observability.Setup(ctx, observability.Config{
		Endpoint: cfg.OTelEndpoint, ServiceName: "scheduler", ServiceVersion: version, Env: cfg.Env,
	}, os.Stdout, levelOf(cfg.LogLevel))
	if err != nil {
		return err
	}
	defer flushTelemetry(shutdownTelemetry)
	slog.SetDefault(logger)

	// Migrations run at startup, before the pool opens (ADR-0002).
	if err := db.RunMigrations(ctx, cfg.DatabaseURL, logger); err != nil {
		return err
	}
	pool, err := db.OpenPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	store := db.NewStore(pool)
	defer store.Close()

	observed, err := db.NewObservedStore(store, logger)
	if err != nil {
		return err
	}
	ctl, err := controllers.New(observed, func() time.Time { return time.Now().UTC() }, logger)
	if err != nil {
		return err
	}
	edge, err := observability.NewOpTracker("handler", "http")
	if err != nil {
		return err
	}

	// Set up rate limiter with Redis
	u, err := url.Parse(cfg.RedisURL)
	if err != nil {
		return fmt.Errorf("parse redis url: %w", err)
	}
	redisAddr := u.Host
	if u.Port() == "" {
		redisAddr = u.Host + ":6379"
	}
	redisClient := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})
	defer redisClient.Close()

	rateLimiter := ratelimit.New(redisClient, store, logger)

	srv := &http.Server{
		Addr: ":" + cfg.Port,
		Handler: handlers.NewRouter(handlers.RouterDeps{
			Handlers: handlers.New(ctl), Tenants: store, DB: store,
			DefaultTenant: cfg.DefaultTenantID, Log: logger, Metrics: edge,
			RateLimiter: rateLimiter,
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		logger.Info("listening", slog.String("addr", srv.Addr), slog.String("version", version),
			slog.String("env", cfg.Env), slog.Int64("default_tenant_id", cfg.DefaultTenantID))
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		logger.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// flushTelemetry sends buffered spans, metrics and logs before exit. It is
// bounded, so a dead collector cannot hold up shutdown.
func flushTelemetry(shutdown func(context.Context) error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		slog.Error("telemetry flush failed", slog.Any("error", err))
	}
}

func levelOf(name string) slog.Level {
	var l slog.Level
	_ = l.UnmarshalText([]byte(name)) // validated in config; falls back to info
	return l
}
