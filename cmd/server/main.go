// Command server runs the scheduling API.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/moidalam/scheduler/internal/config"
	"github.com/moidalam/scheduler/internal/controllers"
	"github.com/moidalam/scheduler/internal/db"
	"github.com/moidalam/scheduler/internal/handlers"
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
	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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

	ctl := controllers.New(store, func() time.Time { return time.Now().UTC() }, logger)

	srv := &http.Server{
		Addr: ":" + cfg.Port,
		Handler: handlers.NewRouter(handlers.RouterDeps{
			Handlers: handlers.New(ctl), Tenants: store, DB: store,
			DefaultTenant: cfg.DefaultTenantID, Log: logger,
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

func newLogger(level string) *slog.Logger {
	var l slog.Level
	_ = l.UnmarshalText([]byte(level)) // validated in config; falls back to info
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: l}))
}
