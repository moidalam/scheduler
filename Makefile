.PHONY: help corp-ca setup generate sqlc-generate build test test-integration vet dev dev-down dev-logs seed psql clean dashboards alerts bvt deploy-marker

GO := go
PYTHON ?= python3

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  %-18s %s\n", $$1, $$2}'

# --- Corporate proxy -----------------------------------------------------------
# Behind a TLS-inspecting proxy the build container cannot verify module downloads
# until it trusts the proxy's CA. This reads that PUBLIC certificate from the macOS
# keychain into certs/ (gitignored); see certs/README.md. Not needed elsewhere.
CORP_CA_NAME ?= ca.ezcater.goskope.com

corp-ca: ## Export your proxy's CA from the macOS keychain for the Docker build
	@mkdir -p certs
	security find-certificate -a -c "$(CORP_CA_NAME)" -p > certs/corp-ca.crt
	@test -s certs/corp-ca.crt || { echo "No certificate named $(CORP_CA_NAME) found"; rm -f certs/corp-ca.crt; exit 1; }
	@echo "Exported to certs/corp-ca.crt (gitignored)."

# --- Code generation ---------------------------------------------------------
# Generated code is committed, so a plain `docker compose up --build` needs no
# generator. Re-run these after editing openapi/spec.yaml or internal/db/queries.sql.

setup: ## Install Go tool dependencies
	$(GO) mod tidy

generate: sqlc-generate ## Regenerate API stubs and query code
	$(GO) tool oapi-codegen --config openapi/oapi-codegen.yaml openapi/spec.yaml

sqlc-generate: ## Regenerate Go from internal/db/queries.sql (needs the sqlc CLI: brew install sqlc)
	cd internal/db && sqlc generate

# --- Build and test ----------------------------------------------------------

build: ## Compile the server and seed binaries to ./bin
	@mkdir -p bin
	CGO_ENABLED=0 $(GO) build -tags timetzdata -o bin/server ./cmd/server
	CGO_ENABLED=0 $(GO) build -tags timetzdata -o bin/seed ./cmd/seed

vet: ## go vet, including the integration-tagged tests
	$(GO) vet ./...
	$(GO) vet -tags integration ./...

test: ## Unit tests (no database needed)
	$(GO) test -race -count=1 ./...

test-integration: .env ## Integration tests against real Postgres, in Docker
	docker compose --profile test run --rm --build test
	docker compose --profile test down --remove-orphans

# --- Local stack, all in Docker ----------------------------------------------

.env:
	cp .env.example .env
	@echo "Created .env from .env.example (throwaway local values, gitignored)."

dev: .env ## Start Postgres, the API and the seed loader
	docker compose up --build

# Records a deployment as a vertical marker on every Grafana panel. Grafana's
# local login is admin/admin (see docker-compose.yml); change it if you change that.
GRAFANA_URL ?= http://localhost:3000
VERSION ?= $(shell git rev-parse --short HEAD)
deploy-marker: ## Mark a deployment on the dashboards (VERSION defaults to the current commit)
	curl -fsS -u admin:admin -H 'Content-Type: application/json' -X POST \
		-d '{"text":"Deployed $(VERSION)","tags":["deploy"]}' $(GRAFANA_URL)/api/annotations
	@echo

dashboards: ## Regenerate the Grafana dashboards from deploy/grafana/build_dashboard.py
	$(PYTHON) deploy/grafana/build_dashboard.py

bvt: .env ## Build-verification tests against the running stack (starts it detached first)
	docker compose up -d --build
	until curl -fsS http://localhost:8080/healthz >/dev/null 2>&1; do sleep 2; done
	$(GO) test -tags bvt -count=1 -v ./test/bvt/

dev-down: ## Stop everything (keeps data; add -v to docker compose down to wipe it)
	docker compose down --remove-orphans

dev-logs: ## Tail the API logs
	docker compose logs -f app

seed: .env ## Re-run the seed loader (idempotent)
	docker compose run --rm seed

psql: .env ## Open psql against the local database
	docker compose exec postgres psql -U scheduler -d scheduler

clean: ## Remove build output
	rm -rf bin
