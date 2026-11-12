# =============================================================================
# base: Go toolchain with dependencies cached in their own layer
# =============================================================================
FROM golang:1.26-alpine AS base

# Optional corporate CAs (certs/*.crt, gitignored) so `go mod download` works
# behind a TLS-inspecting proxy. Trusted by this build stage only: the prod stage
# is built FROM a clean alpine and never receives them.
COPY certs/ /tmp/extra-certs/
RUN find /tmp/extra-certs -name '*.crt' -exec cp {} /usr/local/share/ca-certificates/ \; && \
    apk add --no-cache git ca-certificates && update-ca-certificates && \
    rm -rf /tmp/extra-certs
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

# =============================================================================
# test: used by `make test-integration` via docker compose
# =============================================================================
FROM base AS test
COPY . .
# No -race here: it needs cgo and a C toolchain, which this image deliberately
# lacks. These tests check database behaviour, not Go data races; `make test`
# runs the unit tests with -race on the host.
CMD ["go", "test", "-count=1", "-tags", "integration", "./..."]

# =============================================================================
# builder: static binaries. Generated code (sqlc, oapi-codegen) is committed, so
# no generator is needed here.
# =============================================================================
FROM base AS builder
COPY . .

# VERSION is baked in as main.version so logs can be traced to a build.
ARG VERSION="dev"

# timetzdata embeds the IANA timezone database in the binary. Business hours are
# evaluated in each resource's own timezone (ADR-0003), so the service must not
# depend on the host image having tzdata.
RUN CGO_ENABLED=0 go build -trimpath -tags timetzdata \
      -ldflags="-s -w -X main.version=${VERSION}" -o /out/server ./cmd/server && \
    CGO_ENABLED=0 go build -trimpath -tags timetzdata \
      -ldflags="-s -w -X main.version=${VERSION}" -o /out/seed ./cmd/seed

# =============================================================================
# prod: minimal runtime image
# =============================================================================
FROM alpine:3.23 AS prod

RUN apk --no-cache add ca-certificates

WORKDIR /app
COPY --from=builder /out/server /out/seed ./
COPY data/appointments.seed.json ./data/appointments.seed.json
RUN chown -R nobody:nobody /app

USER nobody
EXPOSE 8080
CMD ["./server"]
