package observability

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Config selects where telemetry goes. An empty Endpoint keeps everything
// local: the providers still run, so trace ids exist, but nothing is exported.
type Config struct {
	Endpoint       string // OTLP/HTTP base URL, e.g. http://otel-lgtm:4318
	ServiceName    string
	ServiceVersion string
	Env            string
}

// Setup installs the global tracer and meter providers, and returns the logger
// the process should use: JSON on out, plus OTLP log export when Endpoint is
// set. The returned shutdown flushes everything and must be called on exit.
func Setup(ctx context.Context, cfg Config, out io.Writer, level slog.Leveler) (*slog.Logger, func(context.Context) error, error) {
	res := resource.NewSchemaless(
		attribute.String("service.name", cfg.ServiceName),
		attribute.String("service.version", cfg.ServiceVersion),
		attribute.String("deployment.environment", cfg.Env),
	)
	tp := sdktrace.NewTracerProvider(sdktrace.WithResource(res))
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithResource(res))
	shutdowns := []func(context.Context) error{tp.Shutdown, mp.Shutdown}
	stdout := slog.NewJSONHandler(out, &slog.HandlerOptions{Level: level})
	handlers := []slog.Handler{stdout}

	if cfg.Endpoint != "" {
		host, insecure, err := parseEndpoint(cfg.Endpoint)
		if err != nil {
			return nil, nil, err
		}
		traceExp, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpoint(host), otlptracehttpInsecure(insecure))
		if err != nil {
			return nil, nil, err
		}
		tp = sdktrace.NewTracerProvider(sdktrace.WithResource(res), sdktrace.WithBatcher(traceExp))
		shutdowns[0] = tp.Shutdown

		metricExp, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpoint(host), otlpmetrichttpInsecure(insecure))
		if err != nil {
			return nil, nil, err
		}
		mp = sdkmetric.NewMeterProvider(sdkmetric.WithResource(res),
			sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp, sdkmetric.WithInterval(10*time.Second))))
		shutdowns[1] = mp.Shutdown

		logExp, err := otlploghttp.New(ctx, otlploghttp.WithEndpoint(host), otlploghttpInsecure(insecure))
		if err != nil {
			return nil, nil, err
		}
		lp := sdklog.NewLoggerProvider(sdklog.WithResource(res),
			sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp)))
		shutdowns = append(shutdowns, lp.Shutdown)
		handlers = append(handlers, otelslog.NewHandler(cfg.ServiceName, otelslog.WithLoggerProvider(lp)))
	}

	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	logger := slog.New(NewContextHandler(&fanout{level: level, handlers: handlers}))
	shutdown := func(ctx context.Context) error {
		var errs []error
		for _, s := range shutdowns {
			errs = append(errs, s(ctx))
		}
		return errors.Join(errs...)
	}
	return logger, shutdown, nil
}

// parseEndpoint splits a base URL into host:port and whether it is plain HTTP.
// The OTLP exporters take a host, not a URL, and they need TLS turned off
// explicitly for http://.
func parseEndpoint(raw string) (host string, insecure bool, err error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", false, err
	}
	if u.Host == "" {
		return "", false, errors.New("OTEL_EXPORTER_OTLP_ENDPOINT must be an absolute URL, e.g. http://otel-lgtm:4318")
	}
	return u.Host, u.Scheme == "http", nil
}
