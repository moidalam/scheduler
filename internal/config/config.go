// Package config resolves runtime configuration from the environment.
// Settings come from environment variables, with an optional .env file for
// local runs, and are validated once at startup so a bad value fails fast.
package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Config is the static service configuration.
type Config struct {
	// Env names the environment, e.g. "local". Env: APP_ENV. Default: "local".
	Env string
	// Port is the HTTP listen port. Env: PORT. Default: "8080".
	Port string
	// DatabaseURL is the Postgres connection string. Env: DATABASE_URL. Required.
	DatabaseURL string
	// DefaultTenantID is used when a request has no X-Tenant-ID header
	// (ADR-0009). Zero means unset: a missing header is then a 400.
	// Env: DEFAULT_TENANT_ID. Default: 0.
	DefaultTenantID int64
	// LogLevel is debug, info, warn or error. Env: LOG_LEVEL. Default: "info".
	LogLevel string
	// OTelEndpoint is the OTLP/HTTP base URL logs, metrics and traces are sent
	// to. Empty keeps telemetry local (stdout only). Env: OTEL_EXPORTER_OTLP_ENDPOINT.
	OTelEndpoint string
}

// Load reads configuration once at process startup.
func Load() (*Config, error) {
	v := viper.New()
	v.SetConfigFile(".env")
	v.SetConfigType("env")
	_ = v.ReadInConfig() // a missing .env is normal outside local development
	v.AutomaticEnv()

	v.SetDefault("app_env", "local")
	v.SetDefault("port", "8080")
	v.SetDefault("default_tenant_id", 0)
	v.SetDefault("log_level", "info")

	cfg := &Config{
		Env:             v.GetString("app_env"),
		Port:            v.GetString("port"),
		DatabaseURL:     v.GetString("database_url"),
		DefaultTenantID: v.GetInt64("default_tenant_id"),
		LogLevel:        strings.ToLower(v.GetString("log_level")),
		OTelEndpoint:    v.GetString("otel_exporter_otlp_endpoint"),
	}
	return cfg, cfg.Validate()
}

// Validate reports every problem at once, so one run fixes them all.
func (c *Config) Validate() error {
	var errs []error
	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}
	if c.DefaultTenantID < 0 {
		errs = append(errs, fmt.Errorf("DEFAULT_TENANT_ID must not be negative, got %d", c.DefaultTenantID))
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Errorf("LOG_LEVEL must be debug, info, warn or error, got %q", c.LogLevel))
	}
	return errors.Join(errs...)
}
