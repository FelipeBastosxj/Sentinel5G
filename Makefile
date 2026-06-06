.PHONY: up down logs test reset build lint infra-up infra-down infra-reset obs-up obs-down obs-logs frontend frontend-build env help

# =============================================================================
# EventStream Observability Engine — Makefile
# =============================================================================

# -----------------------------------------------------------------------------
# Infrastructure
# -----------------------------------------------------------------------------

## Start all infrastructure services (Kafka, Redis, ClickHouse)
infra-up:
	docker compose -f infra/docker-compose.yml up -d

## Stop all infrastructure services
infra-down:
	docker compose -f infra/docker-compose.yml down

## Stop infrastructure and remove volumes (full reset)
infra-reset:
	docker compose -f infra/docker-compose.yml down -v

# -----------------------------------------------------------------------------
# Observability stack (Prometheus, Loki, Tempo, Grafana, OTel Collector)
# -----------------------------------------------------------------------------

## Start the optional observability stack
obs-up:
	docker compose -f infra/docker-compose.yml --profile observability up -d

## Stop the observability stack only
obs-down:
	docker compose -f infra/docker-compose.yml --profile observability down

## Tail observability stack logs
obs-logs:
	docker compose -f infra/docker-compose.yml --profile observability logs -f

# -----------------------------------------------------------------------------
# Services
# -----------------------------------------------------------------------------

## Start all backend services
up:
	@echo "Starting all services..."
	@cd services/ingestion-service && npm run start:dev &
	@cd services/processing-service && npm run start:dev &
	@cd services/realtime-gateway && npm run start:dev &
	@cd services/webhook-service && npm run start:dev &
	@echo "All services started."

## Stop all backend services
down:
	@echo "Stopping all services..."
	@pkill -f "npm run start:dev" || true
	docker compose -f infra/docker-compose.yml down
	@echo "Done."

## View aggregated service logs (docker infrastructure)
logs:
	docker compose -f infra/docker-compose.yml logs -f

# -----------------------------------------------------------------------------
# Testing
# -----------------------------------------------------------------------------

## Run all tests across all services
test:
	@echo "Running tests..."
	@cd services/ingestion-service && npm test
	@cd services/processing-service && npm test
	@cd services/realtime-gateway && npm test
	@cd services/webhook-service && npm test
	@echo "All tests completed."

## Run tests for ingestion-service only
test-ingestion:
	cd services/ingestion-service && npm test

## Run tests for processing-service only
test-processing:
	cd services/processing-service && npm test

## Run tests for realtime-gateway only
test-gateway:
	cd services/realtime-gateway && npm test

## Run tests for webhook-service only
test-webhook:
	cd services/webhook-service && npm test

# -----------------------------------------------------------------------------
# Linting
# -----------------------------------------------------------------------------

## Run linter across all services
lint:
	@echo "Linting services..."
	@cd services/ingestion-service && npm run lint
	@cd services/processing-service && npm run lint
	@cd services/realtime-gateway && npm run lint
	@cd services/webhook-service && npm run lint
	@echo "Lint complete."

# -----------------------------------------------------------------------------
# Build
# -----------------------------------------------------------------------------

## Build all backend services
build:
	@echo "Building services..."
	@cd services/ingestion-service && npm run build
	@cd services/processing-service && npm run build
	@cd services/realtime-gateway && npm run build
	@cd services/webhook-service && npm run build
	@echo "Build complete."

# -----------------------------------------------------------------------------
# Frontend
# -----------------------------------------------------------------------------

## Start Angular dashboard
frontend:
	cd frontend/angular-dashboard && npm start

## Build Angular dashboard
frontend-build:
	cd frontend/angular-dashboard && npm run build

# -----------------------------------------------------------------------------
# Environment
# -----------------------------------------------------------------------------

## Copy .env.example to .env
env:
	cp .env.example .env
	@echo ".env created. Fill in the values before starting services."

## Full reset: stop everything and remove all volumes
reset: infra-reset
	@echo "Environment reset complete."

# -----------------------------------------------------------------------------
# Help
# -----------------------------------------------------------------------------

## Show available commands
help:
	@echo ""
	@echo "EventStream Observability Engine — Available Commands"
	@echo "======================================================"
	@echo ""
	@grep -E '^## ' Makefile | sed 's/## /  /'
	@echo ""
