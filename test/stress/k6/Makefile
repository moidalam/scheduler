.PHONY: help functional chaos contention mixed full clean

API_URL ?= http://localhost:8080
K6 ?= k6

help:
	@echo "Stress test scenarios (requires 'make dev' running in another terminal)"
	@echo ""
	@echo "Usage: make [scenario]"
	@echo ""
	@echo "Scenarios:"
	@echo "  functional   - Functionality tests (verify correctness under load)"
	@echo "  chaos        - Chaos/monkey tests (random payloads, invalid requests)"
	@echo "  contention   - Concurrent contention (25 VUs booking same slot)"
	@echo "  mixed        - All scenarios sequentially"
	@echo "  full         - Full suite with all scenarios"
	@echo "  clean        - Remove k6 artifacts"
	@echo ""
	@echo "Options:"
	@echo "  API_URL=http://api:8080  - Override API endpoint (default: http://localhost:8080)"
	@echo ""
	@echo "Examples:"
	@echo "  make functional"
	@echo "  make chaos"
	@echo "  make contention"

functional:
	@echo "Running functionality tests..."
	API_URL=$(API_URL) $(K6) run scenarios/functionality.js -v

chaos:
	@echo "Running chaos/monkey tests..."
	API_URL=$(API_URL) $(K6) run scenarios/chaos.js -v

contention:
	@echo "Running concurrent contention test (25 VUs)..."
	API_URL=$(API_URL) $(K6) run scenarios/contention.js -v

mixed: functional chaos contention
	@echo "All mixed scenarios completed"

full: mixed
	@echo "Full stress test suite completed"
	@echo ""
	@echo "Check Grafana: http://localhost:3000"
	@echo "Prometheus:    http://localhost:9090"
	@echo "Loki:          http://localhost:3100"

clean:
	@rm -f *.json *.html
	@echo "Cleaned up k6 artifacts"
