package observability

import (
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
)

func otlptracehttpInsecure(insecure bool) otlptracehttp.Option {
	if insecure {
		return otlptracehttp.WithInsecure()
	}
	return otlptracehttp.WithURLPath("/v1/traces")
}

func otlpmetrichttpInsecure(insecure bool) otlpmetrichttp.Option {
	if insecure {
		return otlpmetrichttp.WithInsecure()
	}
	return otlpmetrichttp.WithURLPath("/v1/metrics")
}

func otlploghttpInsecure(insecure bool) otlploghttp.Option {
	if insecure {
		return otlploghttp.WithInsecure()
	}
	return otlploghttp.WithURLPath("/v1/logs")
}
