-- +goose Up
CREATE TABLE scheduler.rate_limits (
  tenant_id bigint NOT NULL REFERENCES scheduler.tenants (id),
  scope text NOT NULL,
  requests_per_second int NOT NULL CHECK (requests_per_second > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, scope)
);

CREATE INDEX idx_rate_limits_tenant ON scheduler.rate_limits(tenant_id);

COMMENT ON TABLE scheduler.rate_limits IS 'Per-tenant rate limit configuration: scope is "global" or an API endpoint name (e.g., "create_booking")';
COMMENT ON COLUMN scheduler.rate_limits.requests_per_second IS 'Maximum requests allowed per second for this scope';

-- +goose Down
DROP TABLE scheduler.rate_limits;
